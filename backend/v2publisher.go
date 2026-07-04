package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultV2PublisherHeartbeat = 5 * time.Second
	defaultV2PublisherReconnect = 2 * time.Second
	maxV2PublisherReconnect     = 30 * time.Second
)

type Graphwar2PublishOptions struct {
	RoomName  string `json:"roomName"`
	Address   string `json:"address"`
	Host      string `json:"host"`
	LocalHost string `json:"localHost,omitempty"`
	Port      int    `json:"port"`

	AxisMode     string `json:"axisMode"`
	FunctionMode string `json:"functionMode"`
	TurnMode     string `json:"turnMode"`
	TimeMode     string `json:"timeMode"`
	Locked       bool   `json:"locked"`
	GameState    string `json:"gameState"`
	NumPlayers   int    `json:"numPlayers"`
}

type Graphwar2PublisherStatus struct {
	Running      bool   `json:"running"`
	Broker       string `json:"broker"`
	RoomName     string `json:"roomName"`
	Address      string `json:"address"`
	LastError    string `json:"lastError"`
	LastSent     string `json:"lastSent"`
	LastReceived string `json:"lastReceived"`
}

type Graphwar2OfficialPublisher struct {
	mu       sync.Mutex
	cancel   context.CancelFunc
	updateCh chan struct{}
	status   Graphwar2PublisherStatus
	options  Graphwar2PublishOptions
}

type graphwar2BrokerRelay struct {
	ctx             context.Context
	opts            Graphwar2PublishOptions
	broker          net.Conn
	brokerMu        sync.Mutex
	mu              sync.Mutex
	byBrokerConn    map[int]*graphwar2BrokerPeer
	playerToBroker  map[int]int
	soldierToPlayer map[int]int
	leaderBrokerID  int
}

type graphwar2BrokerPeer struct {
	brokerID int
	localID  int
	conn     net.Conn
	sendMu   sync.Mutex
}

var defaultGraphwar2Publisher = &Graphwar2OfficialPublisher{}

func DefaultGraphwar2Publisher() *Graphwar2OfficialPublisher {
	return defaultGraphwar2Publisher
}

func (p *Graphwar2OfficialPublisher) Start(ctx context.Context, opts Graphwar2PublishOptions) (Graphwar2PublisherStatus, error) {
	opts = normalizeGraphwar2PublishOptions(opts)
	if opts.Address == "" {
		return p.Status(), errors.New("empty Graphwar II publish address")
	}
	broker, err := ResolveGraphwar2Broker(ctx)
	if err != nil {
		return p.Status(), err
	}

	p.Stop()
	runCtx, cancel := context.WithCancel(context.Background())
	updateCh := make(chan struct{}, 1)
	p.mu.Lock()
	p.cancel = cancel
	p.updateCh = updateCh
	p.options = opts
	p.status = Graphwar2PublisherStatus{
		Running:  true,
		Broker:   broker,
		RoomName: opts.RoomName,
		Address:  opts.Address,
	}
	p.mu.Unlock()

	go p.loop(runCtx, broker, opts)
	return p.Status(), nil
}

func (p *Graphwar2OfficialPublisher) Stop() {
	p.mu.Lock()
	cancel := p.cancel
	p.cancel = nil
	p.updateCh = nil
	p.status.Running = false
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (p *Graphwar2OfficialPublisher) Status() Graphwar2PublisherStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
}

func (p *Graphwar2OfficialPublisher) Update(opts Graphwar2PublishOptions) error {
	opts = normalizeGraphwar2PublishOptions(opts)
	p.mu.Lock()
	if p.cancel == nil || !p.status.Running {
		p.mu.Unlock()
		return errors.New("Graphwar II publisher is not running")
	}
	p.options = opts
	p.status.RoomName = opts.RoomName
	p.status.Address = opts.Address
	updateCh := p.updateCh
	p.mu.Unlock()
	if updateCh != nil {
		select {
		case updateCh <- struct{}{}:
		default:
		}
	}
	return nil
}

func (p *Graphwar2OfficialPublisher) loop(ctx context.Context, broker string, opts Graphwar2PublishOptions) {
	delay := defaultV2PublisherReconnect
	for {
		if ctx.Err() != nil {
			return
		}
		opts = p.currentOptions(opts)
		err := p.publishSession(ctx, broker, opts)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			p.noteError(err)
			V2Debugf("[v2 publisher] broker session failed: %v", err)
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		delay = nextGraphwar2PublisherReconnectDelay(delay)
	}
}

func nextGraphwar2PublisherReconnectDelay(current time.Duration) time.Duration {
	if current <= 0 {
		return defaultV2PublisherReconnect
	}
	next := current * 2
	if next > maxV2PublisherReconnect {
		return maxV2PublisherReconnect
	}
	return next
}

func (p *Graphwar2OfficialPublisher) publishSession(ctx context.Context, broker string, opts Graphwar2PublishOptions) error {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", broker)
	if err != nil {
		return err
	}
	defer conn.Close()
	V2Debugf("[v2 publisher] connected broker=%s address=%s", broker, opts.Address)

	relay := newGraphwar2BrokerRelay(ctx, opts, conn)
	readErr := make(chan error, 1)
	go p.readBrokerEvents(conn, relay, readErr)

	infoPayload, err := graphwar2BrokerEventPayload("GameInfo", opts)
	if err != nil {
		return err
	}
	heartbeatPayload, err := graphwar2BrokerEventPayload("GameHeartbeat", opts)
	if err != nil {
		return err
	}
	removedPayload, err := graphwar2BrokerEventPayload("GameRemoved", opts)
	if err != nil {
		return err
	}

	if err := relay.writeBrokerFrame(infoPayload); err != nil {
		return err
	}
	p.noteSent("GameInfo")

	ticker := time.NewTicker(defaultV2PublisherHeartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			opts = p.currentOptions(opts)
			removedPayload, _ = graphwar2BrokerEventPayload("GameRemoved", opts)
			_ = relay.writeBrokerFrame(removedPayload)
			p.noteSent("GameRemoved")
			return nil
		case <-p.updateSignal():
			opts = p.currentOptions(opts)
			infoPayload, err = graphwar2BrokerEventPayload("GameInfo", opts)
			if err != nil {
				return err
			}
			removedPayload, _ = graphwar2BrokerEventPayload("GameRemoved", opts)
			if err := relay.writeBrokerFrame(infoPayload); err != nil {
				return err
			}
			p.noteSent("GameInfo")
		case <-ticker.C:
			opts = p.currentOptions(opts)
			heartbeatPayload, err = graphwar2BrokerEventPayload("GameHeartbeat", opts)
			if err != nil {
				return err
			}
			removedPayload, _ = graphwar2BrokerEventPayload("GameRemoved", opts)
			if err := relay.writeBrokerFrame(heartbeatPayload); err != nil {
				return err
			}
			p.noteSent("GameHeartbeat")
		case err := <-readErr:
			if err != nil {
				return err
			}
		}
	}
}

func (p *Graphwar2OfficialPublisher) readBrokerEvents(conn net.Conn, relay *graphwar2BrokerRelay, readErr chan<- error) {
	for {
		payload, err := readV2Frame(conn)
		if err != nil {
			readErr <- err
			return
		}
		kind := graphwar2EventKind(payload)
		if kind == "" {
			kind = "unknown"
		}
		p.noteReceived(kind)
		V2Debugf("[v2 publisher] broker -> %s %s", kind, clipV2Debug(string(payload)))
		if relay != nil && relay.handleBrokerEvent(payload) {
			continue
		}
	}
}

func (p *Graphwar2OfficialPublisher) updateSignal() <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.updateCh
}

func (p *Graphwar2OfficialPublisher) currentOptions(fallback Graphwar2PublishOptions) Graphwar2PublishOptions {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.options.Address == "" {
		return fallback
	}
	return p.options
}

func (p *Graphwar2OfficialPublisher) noteError(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status.LastError = err.Error()
}

func (p *Graphwar2OfficialPublisher) noteSent(event string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status.LastError = ""
	p.status.LastSent = event + " at " + time.Now().Format(time.RFC3339)
}

func (p *Graphwar2OfficialPublisher) noteReceived(event string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status.LastReceived = event + " at " + time.Now().Format(time.RFC3339)
}

func normalizeGraphwar2PublishOptions(opts Graphwar2PublishOptions) Graphwar2PublishOptions {
	opts.RoomName = strings.TrimSpace(opts.RoomName)
	if opts.RoomName == "" {
		opts.RoomName = "Graphwar II Room"
	}
	opts.Address = strings.TrimSpace(opts.Address)
	if opts.Address == "" && opts.Port > 0 {
		host := strings.TrimSpace(opts.Host)
		if host == "" {
			host = localOutboundHost()
		}
		opts.Address = net.JoinHostPort(host, strconv.Itoa(opts.Port))
	}
	if opts.AxisMode == "" {
		opts.AxisMode = "EveryUnit"
	}
	if opts.FunctionMode == "" {
		opts.FunctionMode = "NormalFunction"
	}
	if opts.TurnMode == "" {
		opts.TurnMode = "SimultaneousTurns"
	}
	if opts.TimeMode == "" {
		opts.TimeMode = "Timer1m"
	}
	if opts.GameState == "" {
		opts.GameState = "Setup"
	}
	return opts
}

func localOutboundHost() string {
	conn, err := net.DialTimeout("udp", "8.8.8.8:80", 500*time.Millisecond)
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok && addr.IP != nil && !addr.IP.IsUnspecified() {
		return addr.IP.String()
	}
	return "127.0.0.1"
}

func graphwar2BrokerEventPayload(event string, opts Graphwar2PublishOptions) ([]byte, error) {
	if event == "GameHeartbeat" {
		return json.Marshal(map[string]map[string]interface{}{event: {}})
	}
	if event == "GameRemoved" {
		address := strings.TrimSpace(opts.Address)
		if address == "" {
			return nil, errors.New("empty Graphwar II GameRemoved address")
		}
		return json.Marshal(map[string]map[string]string{
			event: {"game_address": address},
		})
	}
	info := graphwar2GameInfo{
		RoomName:           opts.RoomName,
		GameState:          opts.GameState,
		CurrentPlayerCount: opts.NumPlayers,
		Address:            opts.Address,
		AxisMode:           opts.AxisMode,
		FunctionMode:       opts.FunctionMode,
		TurnMode:           opts.TurnMode,
		TimeMode:           opts.TimeMode,
		Locked:             opts.Locked,
	}
	payload := map[string]map[string]graphwar2GameInfo{
		event: {"game_info": info},
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 || len(out) > MaxV2PayloadLen {
		return nil, fmt.Errorf("graphwar2 broker event too large: %d", len(out))
	}
	return out, nil
}

func graphwar2BrokerConnectionRequestPayload() ([]byte, error) {
	return json.Marshal(map[string]map[string]string{
		"NewConnectionRequest": {
			"major_version": "2.050",
			"minor_version": "windows",
		},
	})
}

func newGraphwar2BrokerRelay(ctx context.Context, opts Graphwar2PublishOptions, broker net.Conn) *graphwar2BrokerRelay {
	return &graphwar2BrokerRelay{
		ctx:             ctx,
		opts:            opts,
		broker:          broker,
		byBrokerConn:    map[int]*graphwar2BrokerPeer{},
		playerToBroker:  map[int]int{},
		soldierToPlayer: map[int]int{},
	}
}

func (r *graphwar2BrokerRelay) handleBrokerEvent(payload []byte) bool {
	kind := graphwar2EventKind(payload)
	switch kind {
	case "NewConnection":
		r.handleBrokerNewConnection(payload)
		return true
	case "ConnectedToServer":
		return true
	default:
		peer := r.peerForBrokerPayload(payload)
		if peer == nil {
			return false
		}
		rewritten := r.rewriteBrokerToLocal(peer, payload)
		peer.sendMu.Lock()
		err := writeV2Frame(peer.conn, rewritten)
		peer.sendMu.Unlock()
		if err != nil {
			V2Debugf("[v2 publisher] broker relay write local failed broker_conn=%d local_conn=%d err=%v", peer.brokerID, peer.localID, err)
			r.removePeer(peer, kind != "ConnectionRemoved", "LocalRelayWriteFailed")
		}
		if kind == "ConnectionRemoved" {
			r.removePeer(peer, false, "")
		}
		return true
	}
}

func (r *graphwar2BrokerRelay) handleBrokerNewConnection(payload []byte) {
	var ev map[string]struct {
		ConnectionID int `json:"connection_id"`
	}
	if err := json.Unmarshal(payload, &ev); err != nil {
		V2Debugf("[v2 publisher] bad NewConnection from broker: %s", clipV2Debug(string(payload)))
		return
	}
	brokerID := ev["NewConnection"].ConnectionID
	if brokerID <= 0 {
		V2Debugf("[v2 publisher] broker NewConnection missing id: %s", clipV2Debug(string(payload)))
		return
	}
	r.mu.Lock()
	if r.byBrokerConn[brokerID] != nil {
		r.mu.Unlock()
		return
	}
	r.mu.Unlock()

	host := strings.TrimSpace(r.opts.LocalHost)
	if host == "" {
		host = strings.TrimSpace(r.opts.Host)
	}
	port := r.opts.Port
	if host == "" || port <= 0 {
		if h, p, err := net.SplitHostPort(r.opts.Address); err == nil {
			host = strings.Trim(h, "[]")
			port, _ = strconv.Atoi(p)
		}
	}
	if host == "" {
		host = "127.0.0.1"
	}
	if port <= 0 || port > 65535 {
		V2Debugf("[v2 publisher] cannot relay broker connection %d: bad local target %s", brokerID, r.opts.Address)
		return
	}
	target := net.JoinHostPort(host, strconv.Itoa(port))
	local, err := net.DialTimeout("tcp", target, 10*time.Second)
	if err != nil {
		V2Debugf("[v2 publisher] broker relay dial local failed broker_conn=%d target=%s err=%v", brokerID, target, err)
		return
	}
	peer := &graphwar2BrokerPeer{brokerID: brokerID, conn: local}
	r.mu.Lock()
	r.byBrokerConn[brokerID] = peer
	if r.leaderBrokerID == 0 {
		r.leaderBrokerID = brokerID
	}
	r.mu.Unlock()
	V2Debugf("[v2 publisher] broker relay attached broker_conn=%d local=%s", brokerID, target)

	go r.readLocalPeer(peer)
	peer.sendMu.Lock()
	err = writeV2Frame(peer.conn, []byte(`{"NewConnectionRequest":{"major_version":"2.050","minor_version":"windows"}}`))
	peer.sendMu.Unlock()
	if err != nil {
		V2Debugf("[v2 publisher] broker relay local handshake failed broker_conn=%d err=%v", brokerID, err)
		r.removePeer(peer, true, "LocalHandshakeFailed")
	}
}

func (r *graphwar2BrokerRelay) readLocalPeer(peer *graphwar2BrokerPeer) {
	defer func() {
		notify := r.ctx.Err() == nil
		r.removePeer(peer, notify, "LocalConnectionClosed")
	}()
	for {
		payload, err := readV2Frame(peer.conn)
		if err != nil {
			if r.ctx.Err() == nil && !errors.Is(err, io.EOF) {
				V2Debugf("[v2 publisher] local relay read ended broker_conn=%d local_conn=%d err=%v", peer.brokerID, peer.localID, err)
			}
			return
		}
		rewritten := r.rewriteLocalToBroker(peer, payload)
		if err := r.writeBrokerFrame(rewritten); err != nil {
			V2Debugf("[v2 publisher] local relay write broker failed broker_conn=%d local_conn=%d err=%v", peer.brokerID, peer.localID, err)
			r.removePeer(peer, false, "")
			return
		}
	}
}

func (r *graphwar2BrokerRelay) writeBrokerFrame(payload []byte) error {
	r.brokerMu.Lock()
	defer r.brokerMu.Unlock()
	return writeV2Frame(r.broker, payload)
}

func (r *graphwar2BrokerRelay) removePeer(peer *graphwar2BrokerPeer, notifyBroker bool, reason string) {
	if peer == nil {
		return
	}
	removed := false
	r.mu.Lock()
	if r.byBrokerConn[peer.brokerID] == peer {
		delete(r.byBrokerConn, peer.brokerID)
		removed = true
	}
	if r.leaderBrokerID == peer.brokerID {
		r.leaderBrokerID = 0
		for brokerID := range r.byBrokerConn {
			if r.leaderBrokerID == 0 || brokerID < r.leaderBrokerID {
				r.leaderBrokerID = brokerID
			}
		}
	}
	removedPlayers := map[int]bool{}
	for playerID, brokerID := range r.playerToBroker {
		if brokerID == peer.brokerID {
			removedPlayers[playerID] = true
			delete(r.playerToBroker, playerID)
		}
	}
	for soldierID, playerID := range r.soldierToPlayer {
		if removedPlayers[playerID] {
			delete(r.soldierToPlayer, soldierID)
		}
	}
	r.mu.Unlock()
	_ = peer.conn.Close()
	if removed && notifyBroker && r.broker != nil && peer.brokerID > 0 {
		if strings.TrimSpace(reason) == "" {
			reason = "LocalConnectionClosed"
		}
		payload, err := json.Marshal(map[string]map[string]interface{}{
			"ConnectionRemoved": {
				"connection_id": peer.brokerID,
				"reason":        reason,
			},
		})
		if err == nil {
			if err := r.writeBrokerFrame(payload); err != nil {
				V2Debugf("[v2 publisher] broker notify ConnectionRemoved failed broker_conn=%d err=%v", peer.brokerID, err)
			}
		}
	}
}

func (r *graphwar2BrokerRelay) peerForBrokerPayload(payload []byte) *graphwar2BrokerPeer {
	var ev map[string]json.RawMessage
	if err := json.Unmarshal(payload, &ev); err != nil || len(ev) != 1 {
		return nil
	}
	var raw json.RawMessage
	for _, v := range ev {
		raw = v
	}
	var fields map[string]interface{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil
	}
	if id := intField(fields, "connection_id"); id > 0 {
		r.mu.Lock()
		peer := r.byBrokerConn[id]
		r.mu.Unlock()
		return peer
	}
	if id := intField(fields, "entity_id"); id > 0 {
		r.mu.Lock()
		peer := r.byBrokerConn[id]
		if peer == nil {
			peer = r.byBrokerConn[r.playerToBroker[id]]
		}
		if peer == nil {
			peer = r.byBrokerConn[r.playerToBroker[r.soldierToPlayer[id]]]
		}
		r.mu.Unlock()
		return peer
	}
	if id := firstIntField(fields, "player_id", "owner_id"); id > 0 {
		r.mu.Lock()
		peer := r.byBrokerConn[r.playerToBroker[id]]
		r.mu.Unlock()
		return peer
	}
	if id := intField(fields, "soldier_id"); id > 0 {
		r.mu.Lock()
		peer := r.byBrokerConn[r.playerToBroker[r.soldierToPlayer[id]]]
		r.mu.Unlock()
		return peer
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.leaderBrokerID > 0 {
		return r.byBrokerConn[r.leaderBrokerID]
	}
	return nil
}

func (r *graphwar2BrokerRelay) rewriteBrokerToLocal(peer *graphwar2BrokerPeer, payload []byte) []byte {
	return rewriteGraphwar2EventFields(payload, func(variant string, fields map[string]interface{}) {
		if v := intField(fields, "connection_id"); v == peer.brokerID && peer.localID > 0 {
			fields["connection_id"] = peer.localID
		}
		if peer.localID <= 0 {
			return
		}
		switch variant {
		case "RankRequest", "NameRequest", "ChatMessageRequest",
			"ChangeTeamRequest", "AddSoldierRequest", "RemoveSoldierRequest", "RemovePlayerRequest",
			"FunctionUpdateRequest", "FunctionFireRequest":
			rewriteIntFieldIf(fields, "entity_id", peer.brokerID, peer.localID)
			rewriteIntFieldIf(fields, "owner_id", peer.brokerID, peer.localID)
			rewriteIntFieldIf(fields, "player_id", peer.brokerID, peer.localID)
		}
	})
}

func (r *graphwar2BrokerRelay) rewriteLocalToBroker(peer *graphwar2BrokerPeer, payload []byte) []byte {
	return rewriteGraphwar2EventFields(payload, func(variant string, fields map[string]interface{}) {
		if variant == "NewConnection" {
			if localID := intField(fields, "connection_id"); localID > 0 {
				peer.localID = localID
				fields["connection_id"] = peer.brokerID
			}
		}
		if localID := intField(fields, "connection_id"); localID > 0 && localID == peer.localID {
			fields["connection_id"] = peer.brokerID
		}
		if variant == "ChatMessage" {
			rewriteIntFieldIf(fields, "entity_id", peer.localID, peer.brokerID)
		}
		if variant == "RankInfo" {
			entityID := intField(fields, "entity_id")
			rank := intField(fields, "rank")
			if rank == 1 && (entityID == peer.localID || entityID == peer.brokerID || entityID == 0) {
				r.mu.Lock()
				r.leaderBrokerID = peer.brokerID
				r.mu.Unlock()
				if entityID == peer.localID && peer.localID > 0 {
					fields["entity_id"] = peer.brokerID
				}
			}
		}
		if playerID := intField(fields, "player_id"); playerID > 0 {
			r.mu.Lock()
			if variant == "PlayerInfo" {
				r.playerToBroker[playerID] = peer.brokerID
			}
			if variant == "SoldierInfo" {
				if soldierID := intField(fields, "soldier_id"); soldierID > 0 {
					r.soldierToPlayer[soldierID] = playerID
				}
			}
			r.mu.Unlock()
		}
	})
}

func rewriteGraphwar2EventFields(payload []byte, mutate func(string, map[string]interface{})) []byte {
	var ev map[string]json.RawMessage
	if err := json.Unmarshal(payload, &ev); err != nil || len(ev) != 1 {
		return payload
	}
	for variant, raw := range ev {
		var fields map[string]interface{}
		if err := json.Unmarshal(raw, &fields); err != nil {
			return payload
		}
		mutate(variant, fields)
		out, err := json.Marshal(map[string]interface{}{variant: fields})
		if err != nil || len(out) == 0 || len(out) > MaxV2PayloadLen {
			return payload
		}
		return out
	}
	return payload
}

func intField(fields map[string]interface{}, key string) int {
	v, ok := fields[key]
	if !ok {
		return 0
	}
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case json.Number:
		n, _ := strconv.Atoi(x.String())
		return n
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(x))
		return n
	default:
		return 0
	}
}

func firstIntField(fields map[string]interface{}, keys ...string) int {
	for _, key := range keys {
		if n := intField(fields, key); n > 0 {
			return n
		}
	}
	return 0
}

func rewriteIntFieldIf(fields map[string]interface{}, key string, from, to int) {
	if from <= 0 || to <= 0 {
		return
	}
	if intField(fields, key) == from {
		fields[key] = to
	}
}

func graphwar2EventKind(payload []byte) string {
	var event map[string]json.RawMessage
	if err := json.Unmarshal(payload, &event); err != nil || len(event) != 1 {
		return ""
	}
	for kind := range event {
		return kind
	}
	return ""
}
