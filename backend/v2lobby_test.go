package backend

import (
	"context"
	"net"
	"os"
	"testing"
	"time"
)

func TestParseGraphwar2BrokerEndpoint(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"69.164.213.144:42970\n", "69.164.213.144:42970"},
		{"tcp://69.164.213.144:42970", "69.164.213.144:42970"},
		{"[::1]:42970", "[::1]:42970"},
		{"::1:42970", "[::1]:42970"},
	}
	for _, tt := range tests {
		got, err := parseGraphwar2BrokerEndpoint(tt.in)
		if err != nil {
			t.Fatalf("parse %q: %v", tt.in, err)
		}
		if got != tt.want {
			t.Fatalf("parse %q got %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestGraphwar2GameInfoToRoom(t *testing.T) {
	room, err := graphwar2GameInfoToRoom(graphwar2GameInfo{
		RoomName:           "US Room 4",
		GameState:          "WaitingForFunctions",
		CurrentPlayerCount: 2,
		Address:            "69.164.213.144:46785",
		AxisMode:           "EveryFive",
		FunctionMode:       "NormalFunction",
		TurnMode:           "SimultaneousTurns",
		TimeMode:           "Timer2m",
		Locked:             true,
	})
	if err != nil {
		t.Fatalf("map room: %v", err)
	}
	if room.Name != "US Room 4" || room.IP != "69.164.213.144" || room.Port != 46785 {
		t.Fatalf("bad room identity: %#v", room)
	}
	if room.NumPlayers != 2 || room.Mode != 0 || !room.Locked || room.TurnMode != "SimultaneousTurns" {
		t.Fatalf("bad room fields: %#v", room)
	}

	v6, err := graphwar2GameInfoToRoom(graphwar2GameInfo{RoomName: "Local", Address: "::1:61834"})
	if err != nil {
		t.Fatalf("map ipv6 room: %v", err)
	}
	if v6.IP != "::1" || v6.Port != 61834 {
		t.Fatalf("bad ipv6 room: %#v", v6)
	}
}

func TestParseGraphwar2LobbyEventGameInfo(t *testing.T) {
	payload := []byte(`{"GameInfo":{"game_info":{"room_name":"US Room 1","game_state":"Setup","current_player_count":0,"address":"127.0.0.1:6112","axis_mode":"EveryUnit","function_mode":"FirstOrderODE","turn_mode":"SequentialTurns","time_mode":"Timer1m","locked":false}}}`)
	room, ok := parseGraphwar2LobbyEvent(payload)
	if !ok {
		t.Fatalf("event was not parsed")
	}
	if room.Name != "US Room 1" || room.Mode != 1 || room.Port != 6112 {
		t.Fatalf("bad parsed room: %#v", room)
	}
}

func TestParseGraphwar2LobbyEventHeartbeatAndRemoved(t *testing.T) {
	for _, tt := range []struct {
		payload string
		kind    string
		name    string
	}{
		{`{"GameHeartbeat":{"game_info":{"room_name":"Room H","game_state":"Setup","current_player_count":1,"address":"127.0.0.1:6113","axis_mode":"EveryUnit","function_mode":"NormalFunction","turn_mode":"SimultaneousTurns","time_mode":"Timer1m","locked":false}}}`, "GameHeartbeat", "Room H"},
		{`{"GameRemoved":{"game_info":{"room_name":"Room H","game_state":"Setup","current_player_count":1,"address":"127.0.0.1:6113","axis_mode":"EveryUnit","function_mode":"NormalFunction","turn_mode":"SimultaneousTurns","time_mode":"Timer1m","locked":false}}}`, "GameRemoved", "Room H"},
		{`{"GameRemoved":{"game_address":"127.0.0.1:6113"}}`, "GameRemoved", "127.0.0.1:6113"},
	} {
		kind, room, ok := parseGraphwar2LobbyEventKind([]byte(tt.payload))
		if !ok || kind != tt.kind {
			t.Fatalf("kind parse got kind=%q ok=%v", kind, ok)
		}
		if room.Name != tt.name || room.Port != 6113 {
			t.Fatalf("bad parsed room: %#v", room)
		}
	}
}

func TestParseGraphwar2LobbyEventOfficialEmptyHeartbeat(t *testing.T) {
	kind, room, ok := parseGraphwar2LobbyEventKind([]byte(`{"GameHeartbeat":{}}`))
	if !ok || kind != "GameHeartbeat" {
		t.Fatalf("empty heartbeat parse got kind=%q ok=%v", kind, ok)
	}
	if room.Address != "" {
		t.Fatalf("empty heartbeat should not carry a room: %#v", room)
	}
}

func TestGraphwar2FunctionModeIndex(t *testing.T) {
	if graphwar2FunctionModeIndex("NormalFunction") != 0 {
		t.Fatalf("NormalFunction should map to 0")
	}
	if graphwar2FunctionModeIndex("DiffEqFunction") != 1 {
		t.Fatalf("DiffEqFunction should map to 1")
	}
	if graphwar2FunctionModeIndex("SecondDiffEqFunction") != 2 {
		t.Fatalf("SecondDiffEqFunction should map to 2")
	}
}

func TestMatchGraphwar2HostedRoomPrefersPublishedAddress(t *testing.T) {
	localRooms := []Graphwar2LocalRoom{{Host: "127.0.0.1", Port: 6201, Address: "127.0.0.1:6201"}}
	lobbyRooms := []Graphwar2Room{
		{Name: "Other", Port: 6201, Address: "203.0.113.10:6201", IP: "203.0.113.10"},
		{Name: "Mine", Port: 6201, Address: "198.51.100.7:6201", IP: "198.51.100.7"},
	}
	hosted, ok := matchGraphwar2HostedRoom(localRooms, lobbyRooms, Graphwar2PublisherStatus{
		Running:  true,
		RoomName: "Mine",
		Address:  "198.51.100.7:6201",
	})
	if !ok {
		t.Fatal("expected hosted room match")
	}
	if hosted.LobbyRoom.Name != "Mine" || hosted.Reason != "matched official lobby room by published address" {
		t.Fatalf("bad hosted match: %#v", hosted)
	}
	if hosted.LocalRoom.Address != "127.0.0.1:6201" {
		t.Fatalf("should preserve local join room: %#v", hosted.LocalRoom)
	}
}

func TestMatchGraphwar2HostedRoomUsesNameToAvoidSamePortCollision(t *testing.T) {
	localRooms := []Graphwar2LocalRoom{{Host: "127.0.0.1", Port: 6201, Address: "127.0.0.1:6201"}}
	lobbyRooms := []Graphwar2Room{
		{Name: "Other", Port: 6201, Address: "203.0.113.10:6201", IP: "203.0.113.10"},
		{Name: "Mine", Port: 6201, Address: "198.51.100.7:6201", IP: "198.51.100.7"},
	}
	hosted, ok := matchGraphwar2HostedRoom(localRooms, lobbyRooms, Graphwar2PublisherStatus{RoomName: "Mine"})
	if !ok {
		t.Fatal("expected hosted room match")
	}
	if hosted.LobbyRoom.Name != "Mine" || hosted.Reason != "matched official lobby room by port and room name" {
		t.Fatalf("bad hosted match: %#v", hosted)
	}
}

func TestVerifyGraphwar2OfficialPublicationMatchesPublishedAddress(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen fake broker: %v", err)
	}
	defer ln.Close()
	t.Setenv("GW2_BROKER_ADDR", ln.Addr().String())
	go serveGraphwar2BrokerFrames(t, ln, [][]byte{
		[]byte(`{"GameInfo":{"game_info":{"room_name":"Mine","game_state":"Setup","current_player_count":0,"address":"198.51.100.7:6201","axis_mode":"EveryUnit","function_mode":"NormalFunction","turn_mode":"SimultaneousTurns","time_mode":"Timer1m","locked":false}}}`),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hosted, err := VerifyGraphwar2OfficialPublication(ctx,
		[]Graphwar2LocalRoom{{Host: "127.0.0.1", Port: 6201, Address: "127.0.0.1:6201"}},
		Graphwar2PublisherStatus{Running: true, RoomName: "Mine", Address: "198.51.100.7:6201"},
		1,
	)
	if err != nil {
		t.Fatalf("verify publication: %v", err)
	}
	if hosted.LobbyRoom.Address != "198.51.100.7:6201" || hosted.LocalRoom.Address != "127.0.0.1:6201" {
		t.Fatalf("bad hosted match: %#v", hosted)
	}
}

func TestVerifyGraphwar2OfficialPublicationRetriesUntilListed(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen fake broker: %v", err)
	}
	defer ln.Close()
	t.Setenv("GW2_BROKER_ADDR", ln.Addr().String())
	calls := make(chan struct{}, 4)
	go func() {
		defer close(calls)
		for i := 0; i < 2; i++ {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			calls <- struct{}{}
			if i == 1 {
				_ = writeV2Frame(c, []byte(`{"GameInfo":{"game_info":{"room_name":"Mine","game_state":"Setup","current_player_count":0,"address":"198.51.100.7:6201","axis_mode":"EveryUnit","function_mode":"NormalFunction","turn_mode":"SimultaneousTurns","time_mode":"Timer1m","locked":false}}}`))
			}
			_ = c.Close()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	hosted, err := VerifyGraphwar2OfficialPublication(ctx,
		[]Graphwar2LocalRoom{{Host: "127.0.0.1", Port: 6201, Address: "127.0.0.1:6201"}},
		Graphwar2PublisherStatus{Running: true, RoomName: "Mine", Address: "198.51.100.7:6201"},
		2,
	)
	if err != nil {
		t.Fatalf("verify publication retry: %v", err)
	}
	if hosted.LobbyRoom.Name != "Mine" {
		t.Fatalf("bad hosted match: %#v", hosted)
	}
	if got := len(calls); got != 2 {
		t.Fatalf("expected 2 broker fetches, got %d", got)
	}
}

func TestNormalizeGraphwar2Address(t *testing.T) {
	if got := normalizeGraphwar2Address("[::1]:6201"); got != "[::1]:6201" {
		t.Fatalf("bad bracketed ipv6 normalize: %q", got)
	}
	if got := normalizeGraphwar2Address("::1:6201"); got != "[::1]:6201" {
		t.Fatalf("bad unbracketed ipv6 normalize: %q", got)
	}
	if got := normalizeGraphwar2Address("LOCALHOST:6201"); got != "localhost:6201" {
		t.Fatalf("bad hostname normalize: %q", got)
	}
}

func serveGraphwar2BrokerFrames(t *testing.T, ln net.Listener, frames [][]byte) {
	t.Helper()
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer c.Close()
			for _, frame := range frames {
				_ = writeV2Frame(c, frame)
			}
			time.Sleep(150 * time.Millisecond)
		}()
	}
}

func TestFetchGraphwar2RoomsFromBroker(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen fake broker: %v", err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_ = writeV2Frame(c, []byte(`{"GameInfo":{"game_info":{"room_name":"Room B","game_state":"Setup","current_player_count":0,"address":"127.0.0.1:6202","axis_mode":"EveryUnit","function_mode":"NormalFunction","turn_mode":"SequentialTurns","time_mode":"Timer1m","locked":false}}}`))
		_ = writeV2Frame(c, []byte(`{"GameInfo":{"game_info":{"room_name":"Room A","game_state":"WaitingForFunctions","current_player_count":2,"address":"127.0.0.1:6201","axis_mode":"EveryFive","function_mode":"SecondOrderODE","turn_mode":"SimultaneousTurns","time_mode":"Timer2m","locked":true}}}`))
		time.Sleep(250 * time.Millisecond)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rooms, err := FetchGraphwar2RoomsFromBroker(ctx, ln.Addr().String(), 120*time.Millisecond)
	if err != nil {
		t.Fatalf("fetch rooms: %v", err)
	}
	if len(rooms) != 2 {
		t.Fatalf("got %d rooms: %#v", len(rooms), rooms)
	}
	if rooms[0].Name != "Room A" || rooms[0].ID != 1 || rooms[0].Mode != 2 || !rooms[0].Locked {
		t.Fatalf("bad first room: %#v", rooms[0])
	}
	if rooms[1].Name != "Room B" || rooms[1].ID != 2 {
		t.Fatalf("bad second room: %#v", rooms[1])
	}
}

func TestFetchGraphwar2RoomsFromBrokerAppliesHeartbeatAndRemoved(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen fake broker: %v", err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_ = writeV2Frame(c, []byte(`{"GameInfo":{"game_info":{"room_name":"Room C","game_state":"Setup","current_player_count":0,"address":"127.0.0.1:6203","axis_mode":"EveryUnit","function_mode":"NormalFunction","turn_mode":"SequentialTurns","time_mode":"Timer1m","locked":false}}}`))
		_ = writeV2Frame(c, []byte(`{"GameHeartbeat":{"game_info":{"room_name":"Room C2","game_state":"WaitingForFunctions","current_player_count":2,"address":"127.0.0.1:6203","axis_mode":"EveryUnit","function_mode":"DiffEqFunction","turn_mode":"SimultaneousTurns","time_mode":"Timer2m","locked":true}}}`))
		_ = writeV2Frame(c, []byte(`{"GameRemoved":{"game_info":{"room_name":"Room C2","game_state":"WaitingForFunctions","current_player_count":2,"address":"127.0.0.1:6203","axis_mode":"EveryUnit","function_mode":"DiffEqFunction","turn_mode":"SimultaneousTurns","time_mode":"Timer2m","locked":true}}}`))
		time.Sleep(250 * time.Millisecond)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rooms, err := FetchGraphwar2RoomsFromBroker(ctx, ln.Addr().String(), 120*time.Millisecond)
	if err != nil {
		t.Fatalf("fetch rooms: %v", err)
	}
	if len(rooms) != 0 {
		t.Fatalf("removed room should not remain listed: %#v", rooms)
	}
}

func TestFetchGraphwar2RoomsLiveBroker(t *testing.T) {
	if testing.Short() {
		t.Skip("live broker test skipped in short mode")
	}
	if env := os.Getenv("GW2_LIVE_BROKER"); env != "1" {
		t.Skip("set GW2_LIVE_BROKER=1 to query the live Graphwar II broker")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	rooms, err := FetchGraphwar2Rooms(ctx)
	if err != nil {
		t.Fatalf("live fetch rooms: %v", err)
	}
	if len(rooms) == 0 {
		t.Fatalf("live broker returned no rooms")
	}
	t.Logf("live rooms: %#v", rooms)
}
