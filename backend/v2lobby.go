package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultGraphwar2BrokerURL = "https://graphwar.com/graphwar_II_ip.txt"
	defaultV2LobbyReadWindow  = 1200 * time.Millisecond
)

var graphwar2BrokerCache struct {
	sync.Mutex
	broker string
}

// Graphwar2Room is shaped for the existing lobby UI while keeping the native
// Graphwar II fields needed for diagnostics and future filtering.
type Graphwar2Room struct {
	Name       string `json:"name"`
	ID         int    `json:"id"`
	IP         string `json:"ip"`
	Port       int    `json:"port"`
	Mode       int    `json:"mode"`
	NumPlayers int    `json:"numPlayers"`
	Address    string `json:"address"`
	GameState  string `json:"gameState"`
	AxisMode   string `json:"axisMode"`
	Function   string `json:"functionMode"`
	TurnMode   string `json:"turnMode"`
	TimeMode   string `json:"timeMode"`
	Locked     bool   `json:"locked"`
}

type graphwar2GameInfo struct {
	RoomName           string `json:"room_name"`
	GameState          string `json:"game_state"`
	CurrentPlayerCount int    `json:"current_player_count"`
	Address            string `json:"address"`
	AxisMode           string `json:"axis_mode"`
	FunctionMode       string `json:"function_mode"`
	TurnMode           string `json:"turn_mode"`
	TimeMode           string `json:"time_mode"`
	Locked             bool   `json:"locked"`
}

func ResolveGraphwar2Broker(ctx context.Context) (string, error) {
	if env := strings.TrimSpace(os.Getenv("GW2_BROKER_ADDR")); env != "" {
		return parseGraphwar2BrokerEndpoint(env)
	}
	url := strings.TrimSpace(os.Getenv("GW2_BROKER_URL"))
	if url == "" {
		url = defaultGraphwar2BrokerURL
	}
	body, err := httpGetSmallWithRetry(ctx, url, 4)
	if err != nil {
		if cached := cachedGraphwar2Broker(); cached != "" {
			return cached, nil
		}
		return "", err
	}
	broker, err := parseGraphwar2BrokerEndpoint(string(body))
	if err != nil {
		return "", err
	}
	cacheGraphwar2Broker(broker)
	return broker, nil
}

func FetchGraphwar2Rooms(ctx context.Context) ([]Graphwar2Room, error) {
	broker, err := ResolveGraphwar2Broker(ctx)
	if err != nil {
		return nil, err
	}
	rooms, fetchErr := FetchGraphwar2RoomsFromBroker(ctx, broker, defaultV2LobbyReadWindow)
	if fetchErr == nil {
		cacheGraphwar2Broker(broker)
		return rooms, nil
	}
	if cached := cachedGraphwar2Broker(); cached != "" && cached != broker {
		if rooms, err := FetchGraphwar2RoomsFromBroker(ctx, cached, defaultV2LobbyReadWindow); err == nil {
			return rooms, nil
		}
	}
	return nil, fetchErr
}

func DetectGraphwar2HostedOfficialRoom(ctx context.Context) (Graphwar2HostedRoom, error) {
	localRooms, localErr := DetectGraphwar2LocalRooms()
	if localErr != nil {
		return Graphwar2HostedRoom{}, localErr
	}
	if len(localRooms) == 0 {
		return Graphwar2HostedRoom{}, errors.New("no local Graphwar II room server detected")
	}
	lobbyRooms, lobbyErr := FetchGraphwar2Rooms(ctx)
	if lobbyErr != nil {
		return Graphwar2HostedRoom{LocalRoom: localRooms[0]}, lobbyErr
	}
	status := DefaultGraphwar2Publisher().Status()
	if hosted, ok := matchGraphwar2HostedRoom(localRooms, lobbyRooms, status); ok {
		return hosted, nil
	}
	return Graphwar2HostedRoom{LocalRoom: localRooms[0]}, errors.New("local Graphwar II room detected but not listed by official broker yet")
}

func VerifyGraphwar2OfficialPublication(ctx context.Context, localRooms []Graphwar2LocalRoom, status Graphwar2PublisherStatus, attempts int) (Graphwar2HostedRoom, error) {
	if attempts <= 0 {
		attempts = 1
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		rooms, err := FetchGraphwar2Rooms(ctx)
		if err == nil {
			if hosted, ok := matchGraphwar2HostedRoom(localRooms, rooms, status); ok {
				return hosted, nil
			}
			lastErr = errors.New("published Graphwar II room not found in official broker room list")
		} else {
			lastErr = err
		}
		if i == attempts-1 || !sleepBackoff(ctx, i) {
			break
		}
	}
	if lastErr == nil {
		lastErr = errors.New("published Graphwar II room verification failed")
	}
	return Graphwar2HostedRoom{Publisher: status}, lastErr
}

func matchGraphwar2HostedRoom(localRooms []Graphwar2LocalRoom, lobbyRooms []Graphwar2Room, status Graphwar2PublisherStatus) (Graphwar2HostedRoom, bool) {
	publishedAddress := normalizeGraphwar2Address(status.Address)
	if publishedAddress != "" {
		for _, lobby := range lobbyRooms {
			if normalizeGraphwar2Address(lobby.Address) != publishedAddress {
				continue
			}
			return Graphwar2HostedRoom{
				LocalRoom: localRoomForLobby(localRooms, lobby),
				LobbyRoom: lobby,
				Publisher: status,
				Reason:    "matched official lobby room by published address",
			}, true
		}
	}
	if status.RoomName != "" {
		for _, local := range localRooms {
			for _, lobby := range lobbyRooms {
				if lobby.Port != local.Port || lobby.Name != status.RoomName {
					continue
				}
				return Graphwar2HostedRoom{
					LocalRoom: local,
					LobbyRoom: lobby,
					Publisher: status,
					Reason:    "matched official lobby room by port and room name",
				}, true
			}
		}
	}
	for _, local := range localRooms {
		for _, lobby := range lobbyRooms {
			if lobby.Port == local.Port {
				return Graphwar2HostedRoom{
					LocalRoom: local,
					LobbyRoom: lobby,
					Publisher: status,
					Reason:    "matched official lobby room by room server port",
				}, true
			}
		}
	}
	return Graphwar2HostedRoom{}, false
}

func localRoomForLobby(localRooms []Graphwar2LocalRoom, lobby Graphwar2Room) Graphwar2LocalRoom {
	for _, local := range localRooms {
		if local.Port == lobby.Port {
			return local
		}
	}
	return Graphwar2LocalRoom{
		Host:    lobby.IP,
		Port:    lobby.Port,
		Address: lobby.Address,
	}
}

func normalizeGraphwar2Address(address string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return ""
	}
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		room, mapErr := graphwar2GameInfoToRoom(graphwar2GameInfo{Address: address})
		if mapErr != nil {
			return strings.ToLower(address)
		}
		host, portText = room.IP, strconv.Itoa(room.Port)
	}
	port, err := strconv.Atoi(strings.TrimSpace(portText))
	if err != nil || port <= 0 || port > 65535 {
		return strings.ToLower(address)
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if parsed := net.ParseIP(host); parsed != nil {
		host = parsed.String()
	} else {
		host = strings.ToLower(host)
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

func FetchGraphwar2RoomsFromBroker(ctx context.Context, broker string, readWindow time.Duration) ([]Graphwar2Room, error) {
	broker, err := parseGraphwar2BrokerEndpoint(broker)
	if err != nil {
		return nil, err
	}
	conn, err := dialGraphwar2LobbyWithRetry(ctx, broker, 4)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if readWindow <= 0 {
		readWindow = defaultV2LobbyReadWindow
	}
	deadline := time.Now().Add(readWindow)
	_ = conn.SetDeadline(deadline)

	roomsByAddress := map[string]Graphwar2Room{}
	for {
		payload, err := readV2Frame(conn)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				break
			}
			if errors.Is(err, io.EOF) {
				break
			}
			if len(roomsByAddress) > 0 {
				break
			}
			return nil, err
		}
		if kind, room, ok := parseGraphwar2LobbyEventKind(payload); ok {
			if room.Address == "" {
				continue
			}
			if kind == "GameRemoved" {
				delete(roomsByAddress, room.Address)
				continue
			}
			roomsByAddress[room.Address] = room
		}
		if time.Now().After(deadline) {
			break
		}
	}

	rooms := make([]Graphwar2Room, 0, len(roomsByAddress))
	for _, room := range roomsByAddress {
		rooms = append(rooms, room)
	}
	sort.Slice(rooms, func(i, j int) bool {
		if rooms[i].Name != rooms[j].Name {
			return rooms[i].Name < rooms[j].Name
		}
		return rooms[i].Address < rooms[j].Address
	})
	for i := range rooms {
		rooms[i].ID = i + 1
	}
	return rooms, nil
}

func parseGraphwar2LobbyEvent(payload []byte) (Graphwar2Room, bool) {
	_, room, ok := parseGraphwar2LobbyEventKind(payload)
	return room, ok
}

func parseGraphwar2LobbyEventKind(payload []byte) (string, Graphwar2Room, bool) {
	var event map[string]json.RawMessage
	if err := json.Unmarshal(payload, &event); err != nil {
		return "", Graphwar2Room{}, false
	}
	kind := ""
	raw, ok := event["GameInfo"]
	if ok {
		kind = "GameInfo"
	} else if raw, ok = event["GameHeartbeat"]; ok {
		kind = "GameHeartbeat"
	} else if raw, ok = event["GameRemoved"]; ok {
		kind = "GameRemoved"
	} else {
		return "", Graphwar2Room{}, false
	}
	var wrapped struct {
		GameInfo    graphwar2GameInfo `json:"game_info"`
		GameAddress string            `json:"game_address"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return "", Graphwar2Room{}, false
	}
	if kind == "GameRemoved" && strings.TrimSpace(wrapped.GameAddress) != "" {
		room, err := graphwar2GameInfoToRoom(graphwar2GameInfo{Address: wrapped.GameAddress})
		if err != nil {
			return "", Graphwar2Room{}, false
		}
		return kind, room, true
	}
	if wrapped.GameInfo.Address == "" {
		if kind == "GameHeartbeat" {
			return kind, Graphwar2Room{}, true
		}
		return "", Graphwar2Room{}, false
	}
	room, err := graphwar2GameInfoToRoom(wrapped.GameInfo)
	if err != nil {
		return "", Graphwar2Room{}, false
	}
	return kind, room, true
}

func graphwar2GameInfoToRoom(info graphwar2GameInfo) (Graphwar2Room, error) {
	address := strings.TrimSpace(info.Address)
	if address == "" {
		return Graphwar2Room{}, errors.New("empty graphwar2 room address")
	}
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		if strings.Count(address, ":") == 1 {
			parts := strings.Split(address, ":")
			host, portText = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		} else if last := strings.LastIndex(address, ":"); last > 0 && net.ParseIP(address[:last]) != nil {
			host, portText = address[:last], address[last+1:]
		} else {
			return Graphwar2Room{}, err
		}
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return Graphwar2Room{}, fmt.Errorf("bad graphwar2 room port %q", portText)
	}
	name := strings.TrimSpace(info.RoomName)
	if name == "" {
		name = address
	}
	return Graphwar2Room{
		Name:       name,
		IP:         strings.Trim(host, "[]"),
		Port:       port,
		Mode:       graphwar2FunctionModeIndex(info.FunctionMode),
		NumPlayers: info.CurrentPlayerCount,
		Address:    address,
		GameState:  info.GameState,
		AxisMode:   info.AxisMode,
		Function:   info.FunctionMode,
		TurnMode:   info.TurnMode,
		TimeMode:   info.TimeMode,
		Locked:     info.Locked,
	}, nil
}

func parseGraphwar2BrokerEndpoint(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.Trim(raw, "\x00\r\n\t ")
	if raw == "" {
		return "", errors.New("empty graphwar2 broker endpoint")
	}
	if strings.HasPrefix(raw, "tcp://") {
		raw = strings.TrimPrefix(raw, "tcp://")
	}
	if strings.Contains(raw, "://") {
		return "", fmt.Errorf("unsupported graphwar2 broker endpoint %q", raw)
	}
	host, portText, err := net.SplitHostPort(raw)
	if err != nil {
		if strings.Count(raw, ":") == 1 {
			parts := strings.Split(raw, ":")
			host, portText = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		} else if last := strings.LastIndex(raw, ":"); last > 0 && net.ParseIP(raw[:last]) != nil {
			host, portText = raw[:last], raw[last+1:]
		} else {
			return "", err
		}
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return "", fmt.Errorf("bad graphwar2 broker port %q", portText)
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" {
		return "", errors.New("empty graphwar2 broker host")
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}

func httpGetSmallWithRetry(ctx context.Context, rawURL string, attempts int) ([]byte, error) {
	if attempts <= 0 {
		attempts = 1
	}
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
		},
	}
	var last error
	for i := 0; i < attempts; i++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "GraphwarDesktop/1.0")
		resp, err := client.Do(req)
		if err == nil {
			if resp.StatusCode < 200 || resp.StatusCode > 299 {
				err = fmt.Errorf("graphwar2 broker endpoint returned HTTP %d", resp.StatusCode)
			} else {
				var body []byte
				body, err = io.ReadAll(io.LimitReader(resp.Body, 512))
				if closeErr := resp.Body.Close(); err == nil && closeErr != nil {
					err = closeErr
				}
				if err == nil {
					return body, nil
				}
			}
			if resp.Body != nil {
				_ = resp.Body.Close()
			}
		}
		last = annotateProxyError(err)
		if i == attempts-1 || !isRetryableNetError(err) {
			break
		}
		if !sleepBackoff(ctx, i) {
			return nil, ctx.Err()
		}
	}
	if last == nil {
		last = errors.New("graphwar2 broker fetch failed")
	}
	return nil, last
}

func dialGraphwar2LobbyWithRetry(ctx context.Context, broker string, attempts int) (net.Conn, error) {
	if attempts <= 0 {
		attempts = 1
	}
	var last error
	for i := 0; i < attempts; i++ {
		dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
		conn, err := dialer.DialContext(ctx, "tcp", broker)
		if err == nil {
			return conn, nil
		}
		last = err
		if i == attempts-1 || !isRetryableNetError(err) {
			break
		}
		if !sleepBackoff(ctx, i) {
			return nil, ctx.Err()
		}
	}
	return nil, last
}

func isRetryableNetError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) && (ne.Timeout() || ne.Temporary()) {
		return true
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		return isRetryableNetError(ue.Err)
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "eof") || strings.Contains(msg, "connection reset") || strings.Contains(msg, "timeout")
}

func sleepBackoff(ctx context.Context, attempt int) bool {
	delay := time.Duration(300*(1<<attempt)) * time.Millisecond
	if delay > 2400*time.Millisecond {
		delay = 2400 * time.Millisecond
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func annotateProxyError(err error) error {
	if err == nil {
		return nil
	}
	proxy := activeProxySummary()
	if proxy == "" {
		return fmt.Errorf("%w (system proxy: none from environment)", err)
	}
	return fmt.Errorf("%w (system proxy: %s)", err, proxy)
}

func activeProxySummary() string {
	keys := []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"}
	for _, key := range keys {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return key + "=" + v
		}
	}
	return ""
}

func cacheGraphwar2Broker(broker string) {
	broker = strings.TrimSpace(broker)
	if broker == "" {
		return
	}
	graphwar2BrokerCache.Lock()
	graphwar2BrokerCache.broker = broker
	graphwar2BrokerCache.Unlock()
}

func cachedGraphwar2Broker() string {
	graphwar2BrokerCache.Lock()
	defer graphwar2BrokerCache.Unlock()
	return graphwar2BrokerCache.broker
}

func graphwar2FunctionModeIndex(mode string) int {
	switch mode {
	case "FirstOrderODE", "DiffEqFunction":
		return 1
	case "SecondOrderODE", "SecondDiffEqFunction":
		return 2
	default:
		return 0
	}
}
