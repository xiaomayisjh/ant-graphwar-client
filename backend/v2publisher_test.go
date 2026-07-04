package backend

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"
)

func TestGraphwar2BrokerEventPayload(t *testing.T) {
	payload, err := graphwar2BrokerEventPayload("GameInfo", Graphwar2PublishOptions{
		RoomName:     "Pub Test",
		Address:      "127.0.0.1:6200",
		FunctionMode: "NormalFunction",
	})
	if err != nil {
		t.Fatalf("payload: %v", err)
	}
	var raw map[string]struct {
		GameInfo graphwar2GameInfo `json:"game_info"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	info := raw["GameInfo"].GameInfo
	if info.RoomName != "Pub Test" || info.Address != "127.0.0.1:6200" {
		t.Fatalf("bad info: %#v", info)
	}
	room, ok := parseGraphwar2LobbyEvent(payload)
	if !ok {
		t.Fatalf("payload did not parse as lobby GameInfo")
	}
	if room.Name != "Pub Test" || room.Port != 6200 {
		t.Fatalf("bad room: %#v", room)
	}

	heartbeat, err := graphwar2BrokerEventPayload("GameHeartbeat", Graphwar2PublishOptions{
		RoomName: "Pub Test",
		Address:  "127.0.0.1:6200",
	})
	if err != nil {
		t.Fatalf("heartbeat payload: %v", err)
	}
	if string(heartbeat) != `{"GameHeartbeat":{}}` {
		t.Fatalf("heartbeat should match official empty payload, got %s", string(heartbeat))
	}
}

func TestGraphwar2BrokerConnectionRequestPayload(t *testing.T) {
	payload, err := graphwar2BrokerConnectionRequestPayload()
	if err != nil {
		t.Fatalf("connection request payload: %v", err)
	}
	if graphwar2EventKind(payload) != "NewConnectionRequest" {
		t.Fatalf("bad event kind for %s", string(payload))
	}
	var raw map[string]struct {
		MajorVersion string `json:"major_version"`
		MinorVersion string `json:"minor_version"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	req := raw["NewConnectionRequest"]
	if req.MajorVersion != "2.050" || req.MinorVersion != "windows" {
		t.Fatalf("bad official broker request: %#v", req)
	}
}

func TestNormalizeGraphwar2PublishOptions(t *testing.T) {
	opts := normalizeGraphwar2PublishOptions(Graphwar2PublishOptions{
		RoomName: "  ",
		Host:     "127.0.0.1",
		Port:     61834,
	})
	if opts.RoomName == "" || opts.Address != "127.0.0.1:61834" {
		t.Fatalf("bad normalized options: %#v", opts)
	}
	if opts.TurnMode != "SimultaneousTurns" || opts.TimeMode != "Timer1m" {
		t.Fatalf("bad default modes: %#v", opts)
	}
}

func TestGraphwar2PublisherReconnectDelayBackoff(t *testing.T) {
	if got := nextGraphwar2PublisherReconnectDelay(0); got != defaultV2PublisherReconnect {
		t.Fatalf("zero delay got %s want %s", got, defaultV2PublisherReconnect)
	}
	if got := nextGraphwar2PublisherReconnectDelay(defaultV2PublisherReconnect); got != 4*time.Second {
		t.Fatalf("first backoff got %s", got)
	}
	if got := nextGraphwar2PublisherReconnectDelay(20 * time.Second); got != maxV2PublisherReconnect {
		t.Fatalf("capped backoff got %s want %s", got, maxV2PublisherReconnect)
	}
	if got := nextGraphwar2PublisherReconnectDelay(maxV2PublisherReconnect); got != maxV2PublisherReconnect {
		t.Fatalf("max backoff got %s want %s", got, maxV2PublisherReconnect)
	}
}

func TestGraphwar2PublisherUpdateSendsImmediateGameInfoAndLatestRemoved(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen fake broker: %v", err)
	}
	defer ln.Close()
	t.Setenv("GW2_BROKER_ADDR", ln.Addr().String())

	events := make(chan []byte, 4)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		for {
			payload, err := readV2Frame(c)
			if err != nil {
				return
			}
			events <- payload
		}
	}()

	p := &Graphwar2OfficialPublisher{}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := p.Start(ctx, Graphwar2PublishOptions{RoomName: "Initial", Address: "127.0.0.1:7001"}); err != nil {
		t.Fatalf("start publisher: %v", err)
	}
	defer p.Stop()

	first := mustReadBrokerEvent(t, events, "GameInfo", 2*time.Second)
	if first.RoomName != "Initial" || first.Address != "127.0.0.1:7001" {
		t.Fatalf("bad first GameInfo: %#v", first)
	}
	if err := p.Update(Graphwar2PublishOptions{RoomName: "Updated", Address: "127.0.0.1:7002"}); err != nil {
		t.Fatalf("update publisher: %v", err)
	}
	updated := mustReadBrokerEvent(t, events, "GameInfo", 2*time.Second)
	if updated.RoomName != "Updated" || updated.Address != "127.0.0.1:7002" {
		t.Fatalf("bad updated GameInfo: %#v", updated)
	}
	p.Stop()
	removedFields := mustReadRawBrokerEvent(t, events, "GameRemoved", 2*time.Second)
	if removedFields["game_address"] != "127.0.0.1:7002" {
		t.Fatalf("bad official GameRemoved fields: %#v", removedFields)
	}
}

func TestGraphwar2PublisherRelaysBrokerConnectionToLocalRoom(t *testing.T) {
	room, err := StartGraphwar2CompatRoom("Relay Room", 0)
	if err != nil {
		t.Fatalf("start local room: %v", err)
	}
	defer room.Close()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen fake broker: %v", err)
	}
	defer ln.Close()
	t.Setenv("GW2_BROKER_ADDR", ln.Addr().String())

	events := make(chan []byte, 32)
	brokerConn := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		if _, err := readV2Frame(c); err != nil {
			return
		}
		brokerConn <- c
		_ = writeV2Frame(c, []byte(`{"NewConnection":{"connection_id":77}}`))
		for {
			payload, err := readV2Frame(c)
			if err != nil {
				return
			}
			events <- payload
		}
	}()

	p := &Graphwar2OfficialPublisher{}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	st, err := p.Start(ctx, Graphwar2PublishOptions{
		RoomName: "Relay Room",
		Host:     "127.0.0.1",
		Port:     room.Port(),
	})
	if err != nil {
		t.Fatalf("start publisher: %v status=%#v", err, st)
	}
	defer p.Stop()

	mustReadRawBrokerEvent(t, events, "ConnectedToServer", 2*time.Second)
	connFields := mustReadRawBrokerEvent(t, events, "NewConnection", 2*time.Second)
	if got := int(connFields["connection_id"].(float64)); got != 77 {
		t.Fatalf("relay should rewrite local connection id to broker id, got %#v", connFields)
	}

	var bc net.Conn
	select {
	case bc = <-brokerConn:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for fake broker connection")
	}
	if err := writeV2Frame(bc, []byte(`{"NewPlayerRequest":{"connection_id":77}}`)); err != nil {
		t.Fatalf("write broker NewPlayerRequest: %v", err)
	}
	playerFields := mustReadRawBrokerEvent(t, events, "PlayerInfo", 2*time.Second)
	if got := int(playerFields["connection_id"].(float64)); got != 77 {
		t.Fatalf("PlayerInfo should keep broker connection id, got %#v", playerFields)
	}
	playerID := int(playerFields["player_id"].(float64))
	if playerID <= 0 {
		t.Fatalf("bad relayed player id: %#v", playerFields)
	}
	mustReadRawBrokerEvent(t, events, "NameInfo", 2*time.Second)
	mustReadRawBrokerEvent(t, events, "TeamInfo", 2*time.Second)
	initialSoldier := mustReadRawBrokerEvent(t, events, "SoldierInfo", 2*time.Second)
	soldierID := int(initialSoldier["soldier_id"].(float64))
	if soldierID <= 0 {
		t.Fatalf("bad initial soldier id: %#v", initialSoldier)
	}

	if err := writeV2Frame(bc, []byte(`{"AddSoldierRequest":{"player_id":`+itoa(playerID)+`}}`)); err != nil {
		t.Fatalf("write broker AddSoldierRequest: %v", err)
	}
	soldierFields := mustReadRawBrokerEvent(t, events, "SoldierInfo", 2*time.Second)
	if int(soldierFields["player_id"].(float64)) != playerID {
		t.Fatalf("bad relayed SoldierInfo: %#v", soldierFields)
	}

	if err := writeV2Frame(bc, []byte(`{"RemoveSoldierRequest":{"entity_id":`+itoa(playerID)+`}}`)); err != nil {
		t.Fatalf("write broker RemoveSoldierRequest alias: %v", err)
	}
	removedFields := mustReadRawBrokerEvent(t, events, "EntityRemoved", 2*time.Second)
	if int(removedFields["entity_id"].(float64)) == 0 {
		t.Fatalf("bad relayed EntityRemoved: %#v", removedFields)
	}

	if err := writeV2Frame(bc, []byte(`{"ChangeTeamRequest":{"entity_id":`+itoa(playerID)+`}}`)); err != nil {
		t.Fatalf("write broker ChangeTeamRequest alias: %v", err)
	}
	teamFields := mustReadRawBrokerEvent(t, events, "TeamInfo", 2*time.Second)
	if int(teamFields["entity_id"].(float64)) != playerID {
		t.Fatalf("bad relayed TeamInfo: %#v", teamFields)
	}

	if err := writeV2Frame(bc, []byte(`{"ChatMessageRequest":{"entity_id":77,"message":"hello via broker"}}`)); err != nil {
		t.Fatalf("write broker ChatMessageRequest by conn: %v", err)
	}
	chatFields := mustReadRawBrokerEvent(t, events, "ChatMessage", 2*time.Second)
	if chatFields["message"] != "hello via broker" || int(chatFields["entity_id"].(float64)) != 77 || int(chatFields["player_id"].(float64)) != playerID {
		t.Fatalf("bad relayed chat by conn: %#v", chatFields)
	}

	if err := writeV2Frame(bc, []byte(`{"ChatMessageRequest":{"entity_id":`+itoa(playerID)+`,"message":"hello by player"}}`)); err != nil {
		t.Fatalf("write broker ChatMessageRequest by player: %v", err)
	}
	chatFields = mustReadRawBrokerEvent(t, events, "ChatMessage", 2*time.Second)
	if chatFields["message"] != "hello by player" || int(chatFields["entity_id"].(float64)) != 77 || int(chatFields["player_id"].(float64)) != playerID {
		t.Fatalf("bad relayed chat by player: %#v", chatFields)
	}

	if err := writeV2Frame(bc, []byte(`{"SkinGraphicsRequest":{"soldier_id":`+itoa(soldierID)+`,"graphics_str":"skin_1"}}`)); err != nil {
		t.Fatalf("write broker SkinGraphicsRequest by soldier: %v", err)
	}
	skinFields := mustReadRawBrokerEvent(t, events, "SkinGraphicsInfo", 2*time.Second)
	if skinFields["graphics_str"] != "skin_1" || int(skinFields["entity_id"].(float64)) != soldierID {
		t.Fatalf("bad relayed SkinGraphicsInfo: %#v", skinFields)
	}

	if err := writeV2Frame(bc, []byte(`{"GameStartRequest":{}}`)); err != nil {
		t.Fatalf("write broker GameStartRequest: %v", err)
	}
	mustReadRawBrokerEvent(t, events, "GameStateInfo", 2*time.Second)
	if err := writeV2Frame(bc, []byte(`{"FunctionUpdateRequest":{"player_id":`+itoa(playerID)+`,"function":"0"}}`)); err != nil {
		t.Fatalf("write broker FunctionUpdateRequest by player: %v", err)
	}
	functionFields := mustReadRawBrokerEvent(t, events, "FunctionInfo", 2*time.Second)
	if functionFields["function"] != "0" || functionFields["corrected_function"] != "0" || int(functionFields["owner_id"].(float64)) != playerID {
		t.Fatalf("bad relayed FunctionInfo: %#v", functionFields)
	}
	if err := writeV2Frame(bc, []byte(`{"FunctionFireRequest":{"player_id":`+itoa(playerID)+`}}`)); err != nil {
		t.Fatalf("write broker FunctionFireRequest by player: %v", err)
	}
	fireFields := mustReadRawBrokerEvent(t, events, "FunctionFire", 2*time.Second)
	if int(fireFields["soldier_id"].(float64)) != soldierID {
		t.Fatalf("bad relayed FunctionFire: %#v", fireFields)
	}
}

func TestGraphwar2PublisherRoutesIdlessLeaderRequestsWithMultipleBrokerPeers(t *testing.T) {
	room, err := StartGraphwar2CompatRoom("Relay Leaders", 0)
	if err != nil {
		t.Fatalf("start local room: %v", err)
	}
	defer room.Close()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen fake broker: %v", err)
	}
	defer ln.Close()
	t.Setenv("GW2_BROKER_ADDR", ln.Addr().String())

	events := make(chan []byte, 64)
	brokerConn := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		if _, err := readV2Frame(c); err != nil {
			return
		}
		brokerConn <- c
		for _, id := range []int{77, 88} {
			_ = writeV2Frame(c, []byte(`{"NewConnection":{"connection_id":`+itoa(id)+`}}`))
		}
		for {
			payload, err := readV2Frame(c)
			if err != nil {
				return
			}
			events <- payload
		}
	}()

	p := &Graphwar2OfficialPublisher{}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := p.Start(ctx, Graphwar2PublishOptions{RoomName: "Relay Leaders", Host: "127.0.0.1", Port: room.Port()}); err != nil {
		t.Fatalf("start publisher: %v", err)
	}
	defer p.Stop()

	var bc net.Conn
	select {
	case bc = <-brokerConn:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for fake broker connection")
	}
	firstConn := mustReadRawBrokerEvent(t, events, "NewConnection", 5*time.Second)
	secondConn := mustReadRawBrokerEvent(t, events, "NewConnection", 5*time.Second)
	seenConnIDs := map[int]bool{
		int(firstConn["connection_id"].(float64)):  true,
		int(secondConn["connection_id"].(float64)): true,
	}
	if !seenConnIDs[77] || !seenConnIDs[88] || len(seenConnIDs) != 2 {
		t.Fatalf("bad relayed connections: first=%#v second=%#v", firstConn, secondConn)
	}

	if err := writeV2Frame(bc, []byte(`{"AxisModeChangeRequest":{}}`)); err != nil {
		t.Fatalf("write leader id-less AxisModeChangeRequest: %v", err)
	}
	axisFields := mustReadRawBrokerEventMatching(t, events, "AxisModeInfo", 2*time.Second, func(fields map[string]interface{}) bool {
		return fields["axis_mode"] != "EveryUnit"
	})
	if axisFields["axis_mode"] == "EveryUnit" {
		t.Fatalf("leader id-less mode request did not cycle axis mode: %#v", axisFields)
	}

	if err := writeV2Frame(bc, []byte(`{"NewPlayerRequest":{"connection_id":88}}`)); err != nil {
		t.Fatalf("write follower NewPlayerRequest: %v", err)
	}
	mustReadRawBrokerEvent(t, events, "PlayerInfo", 2*time.Second)
	if err := writeV2Frame(bc, []byte(`{"GameStartRequest":{}}`)); err != nil {
		t.Fatalf("write leader id-less GameStartRequest: %v", err)
	}
	mustReadRawBrokerEvent(t, events, "GameStateInfo", 2*time.Second)
}

func TestGraphwar2BrokerRelayRoutesIdlessRequestsToLeader(t *testing.T) {
	r := newGraphwar2BrokerRelay(context.Background(), Graphwar2PublishOptions{}, nil)
	c1, s1 := net.Pipe()
	c2, s2 := net.Pipe()
	defer c1.Close()
	defer s1.Close()
	defer c2.Close()
	defer s2.Close()
	peer1 := &graphwar2BrokerPeer{brokerID: 11, conn: c1}
	peer2 := &graphwar2BrokerPeer{brokerID: 22, conn: c2}
	r.byBrokerConn[11] = peer1
	r.byBrokerConn[22] = peer2
	r.leaderBrokerID = 11

	if peer := r.peerForBrokerPayload([]byte(`{"GameStartRequest":{}}`)); peer != peer1 {
		t.Fatalf("id-less start request should route to leader peer: %#v", peer)
	}
	if peer := r.peerForBrokerPayload([]byte(`{"AxisModeChangeRequest":{}}`)); peer != peer1 {
		t.Fatalf("id-less mode request should route to leader peer: %#v", peer)
	}

	r.removePeer(peer1, false, "")
	if peer := r.peerForBrokerPayload([]byte(`{"GameStartRequest":{}}`)); peer != peer2 {
		t.Fatalf("id-less request should route to promoted leader peer: %#v", peer)
	}
}

func TestGraphwar2BrokerRelayNotifiesBrokerWhenLocalPeerCloses(t *testing.T) {
	brokerLocal, brokerRemote := net.Pipe()
	defer brokerLocal.Close()
	defer brokerRemote.Close()
	localClient, localServer := net.Pipe()
	defer localClient.Close()
	defer localServer.Close()

	r := newGraphwar2BrokerRelay(context.Background(), Graphwar2PublishOptions{}, brokerLocal)
	peer := &graphwar2BrokerPeer{brokerID: 77, localID: 3, conn: localClient}
	r.byBrokerConn[77] = peer
	r.leaderBrokerID = 77
	r.playerToBroker[5] = 77
	r.soldierToPlayer[9] = 5

	_ = localServer.Close()
	go r.readLocalPeer(peer)
	fields := mustReadPipeEvent(t, brokerRemote, "ConnectionRemoved", 2*time.Second)
	if int(fields["connection_id"].(float64)) != 77 || fields["reason"] == "" {
		t.Fatalf("bad ConnectionRemoved notify: %#v", fields)
	}
	if r.byBrokerConn[77] != nil || r.playerToBroker[5] != 0 || r.soldierToPlayer[9] != 0 {
		t.Fatalf("peer mappings were not cleaned")
	}
}

func TestGraphwar2BrokerRelayRoutesOfficialAliases(t *testing.T) {
	r := newGraphwar2BrokerRelay(context.Background(), Graphwar2PublishOptions{}, nil)
	c, s := net.Pipe()
	defer c.Close()
	defer s.Close()
	peer := &graphwar2BrokerPeer{brokerID: 77, conn: c}
	r.byBrokerConn[77] = peer
	r.leaderBrokerID = 77
	r.playerToBroker[5] = 77
	r.soldierToPlayer[9] = 5

	for _, payload := range []string{
		`{"FunctionUpdateRequest":{"owner_id":5,"function":"0"}}`,
		`{"FunctionFireRequest":{"soldier_id":9}}`,
		`{"SkinGraphicsRequest":{"entity_id":9,"graphics_str":"skin_1"}}`,
		`{"GameStartRequest":{}}`,
	} {
		if got := r.peerForBrokerPayload([]byte(payload)); got != peer {
			t.Fatalf("payload %s routed to %#v, want peer", payload, got)
		}
	}
}

func TestGraphwar2BrokerRelayRewritesConnectionRefsForLocalRoom(t *testing.T) {
	r := newGraphwar2BrokerRelay(context.Background(), Graphwar2PublishOptions{}, nil)
	peer := &graphwar2BrokerPeer{brokerID: 77, localID: 3}
	for _, tt := range []struct {
		in        string
		variant   string
		field     string
		wantValue int
	}{
		{`{"RankRequest":{"entity_id":77}}`, "RankRequest", "entity_id", 3},
		{`{"AddSoldierRequest":{"entity_id":77}}`, "AddSoldierRequest", "entity_id", 3},
		{`{"FunctionUpdateRequest":{"owner_id":77,"corrected_function":"x"}}`, "FunctionUpdateRequest", "owner_id", 3},
		{`{"ConnectionRemoved":{"connection_id":77,"reason":"ClientDisconnect"}}`, "ConnectionRemoved", "connection_id", 3},
		{`{"SkinGraphicsRequest":{"soldier_id":77,"graphics_str":"skin_1"}}`, "SkinGraphicsRequest", "soldier_id", 77},
	} {
		got := r.rewriteBrokerToLocal(peer, []byte(tt.in))
		var ev map[string]map[string]interface{}
		if err := json.Unmarshal(got, &ev); err != nil {
			t.Fatalf("bad rewritten json %s: %v", string(got), err)
		}
		if int(ev[tt.variant][tt.field].(float64)) != tt.wantValue {
			t.Fatalf("rewrite %s got %s", tt.in, string(got))
		}
	}
}

func TestGraphwar2BrokerRelayRewritesLocalChatConnectionEntity(t *testing.T) {
	r := newGraphwar2BrokerRelay(context.Background(), Graphwar2PublishOptions{}, nil)
	peer := &graphwar2BrokerPeer{brokerID: 77, localID: 3}
	got := r.rewriteLocalToBroker(peer, []byte(`{"ChatMessage":{"entity_id":3,"connection_id":3,"player_id":5,"message":"hello"}}`))
	var ev map[string]map[string]interface{}
	if err := json.Unmarshal(got, &ev); err != nil {
		t.Fatalf("bad rewritten json %s: %v", string(got), err)
	}
	fields := ev["ChatMessage"]
	if int(fields["entity_id"].(float64)) != 77 || int(fields["connection_id"].(float64)) != 77 || int(fields["player_id"].(float64)) != 5 {
		t.Fatalf("bad rewritten chat ids: %s", string(got))
	}
}

func mustReadPipeEvent(t *testing.T, c net.Conn, want string, timeout time.Duration) map[string]interface{} {
	t.Helper()
	type result struct {
		fields map[string]interface{}
		err    error
	}
	ch := make(chan result, 1)
	go func() {
		payload, err := readV2Frame(c)
		if err != nil {
			ch <- result{err: err}
			return
		}
		var ev map[string]map[string]interface{}
		if err := json.Unmarshal(payload, &ev); err != nil {
			ch <- result{err: err}
			return
		}
		fields, ok := ev[want]
		if !ok {
			ch <- result{err: errors.New("unexpected event " + string(payload))}
			return
		}
		ch <- result{fields: fields}
	}()
	select {
	case got := <-ch:
		if got.err != nil {
			t.Fatalf("read %s: %v", want, got.err)
		}
		return got.fields
	case <-time.After(timeout):
		t.Fatalf("timeout waiting for pipe event %s", want)
		return nil
	}
}

func mustReadBrokerEvent(t *testing.T, events <-chan []byte, want string, timeout time.Duration) graphwar2GameInfo {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case payload := <-events:
			kind, room, ok := parseGraphwar2LobbyEventKind(payload)
			if ok && kind == want {
				return graphwar2GameInfo{
					RoomName:           room.Name,
					GameState:          room.GameState,
					CurrentPlayerCount: room.NumPlayers,
					Address:            room.Address,
					AxisMode:           room.AxisMode,
					FunctionMode:       room.Function,
					TurnMode:           room.TurnMode,
					TimeMode:           room.TimeMode,
					Locked:             room.Locked,
				}
			}
		case <-deadline:
			t.Fatalf("timeout waiting for %s", want)
		}
	}
}

func mustReadRawBrokerEvent(t *testing.T, events <-chan []byte, want string, timeout time.Duration) map[string]interface{} {
	return mustReadRawBrokerEventMatching(t, events, want, timeout, nil)
}

func mustReadRawBrokerEventMatching(t *testing.T, events <-chan []byte, want string, timeout time.Duration, match func(map[string]interface{}) bool) map[string]interface{} {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case payload := <-events:
			var ev map[string]map[string]interface{}
			if err := json.Unmarshal(payload, &ev); err != nil {
				t.Fatalf("bad broker json %s: %v", string(payload), err)
			}
			if fields, ok := ev[want]; ok {
				if match == nil || match(fields) {
					return fields
				}
			}
		case <-deadline:
			t.Fatalf("timeout waiting for broker event %s", want)
		}
	}
}

func readRawBrokerEventsUntil(t *testing.T, events <-chan []byte, timeout time.Duration, wants ...string) map[string]bool {
	t.Helper()
	wantSet := map[string]bool{}
	for _, want := range wants {
		wantSet[want] = true
	}
	seen := map[string]bool{}
	deadline := time.After(timeout)
	for {
		if len(seen) == len(wantSet) {
			return seen
		}
		select {
		case payload := <-events:
			var ev map[string]map[string]interface{}
			if err := json.Unmarshal(payload, &ev); err != nil {
				t.Fatalf("bad broker json %s: %v", string(payload), err)
			}
			for kind := range ev {
				if wantSet[kind] {
					seen[kind] = true
				}
			}
		case <-deadline:
			return seen
		}
	}
}
