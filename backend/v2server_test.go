package backend

import (
	"encoding/json"
	"math"
	"net"
	"testing"
	"time"
)

func TestGraphwar2CompatRoomSetupFlow(t *testing.T) {
	s, err := StartGraphwar2CompatRoom("V2 Test", 0)
	if err != nil {
		t.Fatalf("start v2 room: %v", err)
	}
	defer s.Close()

	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", itoa(s.Port())), 2*time.Second)
	if err != nil {
		t.Fatalf("dial v2 room: %v", err)
	}
	defer c.Close()

	mustWriteV2(t, c, `{"NewConnectionRequest":{"major_version":"2.0","minor_version":"2.0"}}`)
	if _, fields := readVariant(t, c, "ConnectedToServer", 2*time.Second); fields == nil {
		t.Fatal("missing ConnectedToServer")
	}
	_, connFields := readVariant(t, c, "NewConnection", 2*time.Second)
	connID := int(connFields["connection_id"].(float64))
	if connID == 0 {
		t.Fatalf("bad connection id: %#v", connFields)
	}

	mustWriteV2(t, c, `{"NewPlayerRequest":{"connection_id":`+itoa(connID)+`}}`)
	_, playerFields := readVariant(t, c, "PlayerInfo", 2*time.Second)
	playerID := int(playerFields["player_id"].(float64))
	if playerID == 0 {
		t.Fatalf("bad player id: %#v", playerFields)
	}
	readVariant(t, c, "NameInfo", 2*time.Second)
	readVariant(t, c, "TeamInfo", 2*time.Second)
	readVariant(t, c, "SoldierInfo", 2*time.Second)

	mustWriteV2(t, c, `{"NameRequest":{"entity_id":`+itoa(playerID)+`,"name":"Tester"}}`)
	nameFields := readVariantMatching(t, c, "NameInfo", 2*time.Second, func(fields map[string]interface{}) bool {
		return fields["name"] == "Tester"
	})
	if nameFields["name"] != "Tester" {
		t.Fatalf("bad name echo: %#v", nameFields)
	}

	mustWriteV2(t, c, `{"AddSoldierRequest":{"player_id":`+itoa(playerID)+`}}`)
	_, soldierFields := readVariant(t, c, "SoldierInfo", 2*time.Second)
	if int(soldierFields["player_id"].(float64)) != playerID {
		t.Fatalf("bad soldier echo: %#v", soldierFields)
	}

	mustWriteV2(t, c, `{"ChatMessageRequest":{"entity_id":`+itoa(connID)+`,"message":"hello"}}`)
	_, chatFields := readVariant(t, c, "ChatMessage", 2*time.Second)
	if chatFields["message"] != "hello" {
		t.Fatalf("bad chat echo: %#v", chatFields)
	}
	if int(chatFields["entity_id"].(float64)) != connID || int(chatFields["player_id"].(float64)) != playerID {
		t.Fatalf("bad official chat ids: %#v", chatFields)
	}

	mustWriteV2(t, c, `{"SkinGraphicsRequest":{"entity_id":1,"graphics_str":"skin_0"}}`)
	skinFields := readVariantMatching(t, c, "SkinGraphicsInfo", 2*time.Second, func(fields map[string]interface{}) bool {
		return fields["graphics_str"] == "skin_0"
	})
	if int(skinFields["entity_id"].(float64)) != 1 {
		t.Fatalf("bad skin echo: %#v", skinFields)
	}

	mustWriteV2(t, c, `{"GameStartRequest":{}}`)
	seen := readVariantsUntil(t, c, 2*time.Second, "ClearObstacles", "AddObstacle", "GameStateInfo", "PosInfo", "LifeInfo", "TurnLimitInfo", "TurnInfo")
	for _, want := range []string{"ClearObstacles", "AddObstacle", "GameStateInfo", "PosInfo", "LifeInfo", "TurnLimitInfo", "TurnInfo"} {
		if !seen[want] {
			t.Fatalf("missing start event %s in %#v", want, seen)
		}
	}
}

func TestGraphwar2CompatRoomOfficialFieldAliases(t *testing.T) {
	s, err := StartGraphwar2CompatRoom("V2 Alias", 0)
	if err != nil {
		t.Fatalf("start v2 room: %v", err)
	}
	defer s.Close()

	c, connID, playerID := connectV2TestPlayer(t, s.Port())
	defer c.Close()
	readVariant(t, c, "NameInfo", 2*time.Second)
	readVariant(t, c, "TeamInfo", 2*time.Second)
	soldierFields := readVariantMatching(t, c, "SoldierInfo", 2*time.Second, func(fields map[string]interface{}) bool {
		return int(fields["player_id"].(float64)) == playerID
	})
	soldierID := int(soldierFields["soldier_id"].(float64))
	readVariantMatching(t, c, "SoldierInfo", 2*time.Second, func(fields map[string]interface{}) bool {
		return int(fields["player_id"].(float64)) == playerID && int(fields["soldier_id"].(float64)) != soldierID
	})

	mustWriteV2(t, c, `{"AddSoldierRequest":{"entity_id":`+itoa(playerID)+`}}`)
	added := readVariantMatching(t, c, "SoldierInfo", 2*time.Second, func(fields map[string]interface{}) bool {
		return int(fields["player_id"].(float64)) == playerID && int(fields["soldier_id"].(float64)) != soldierID
	})
	if int(added["player_id"].(float64)) != playerID {
		t.Fatalf("bad alias AddSoldierRequest echo: %#v", added)
	}

	mustWriteV2(t, c, `{"RemoveSoldierRequest":{"entity_id":`+itoa(connID)+`}}`)
	removedByConn := readVariantMatching(t, c, "EntityRemoved", 2*time.Second, func(fields map[string]interface{}) bool {
		return int(fields["entity_id"].(float64)) == int(added["soldier_id"].(float64))
	})
	if int(removedByConn["entity_id"].(float64)) == 0 {
		t.Fatalf("bad connection-id RemoveSoldierRequest echo: %#v", removedByConn)
	}

	mustWriteV2(t, c, `{"AddSoldierRequest":{"entity_id":`+itoa(connID)+`}}`)
	addedByConn := readVariantMatching(t, c, "SoldierInfo", 2*time.Second, func(fields map[string]interface{}) bool {
		return int(fields["player_id"].(float64)) == playerID && int(fields["soldier_id"].(float64)) != soldierID
	})
	if int(addedByConn["player_id"].(float64)) != playerID {
		t.Fatalf("bad connection-id AddSoldierRequest echo: %#v", addedByConn)
	}

	mustWriteV2(t, c, `{"AddSoldierRequest":{"soldier_id":`+itoa(soldierID)+`}}`)
	addedBySoldier := readVariantMatching(t, c, "SoldierInfo", 2*time.Second, func(fields map[string]interface{}) bool {
		return int(fields["player_id"].(float64)) == playerID &&
			int(fields["soldier_id"].(float64)) != soldierID &&
			int(fields["soldier_id"].(float64)) != int(addedByConn["soldier_id"].(float64))
	})
	if int(addedBySoldier["player_id"].(float64)) != playerID {
		t.Fatalf("bad official soldier-id AddSoldierRequest echo: %#v", addedBySoldier)
	}

	mustWriteV2(t, c, `{"RemoveSoldierRequest":{"soldier_id":`+itoa(soldierID)+`}}`)
	removedBySoldier := readVariantMatching(t, c, "EntityRemoved", 2*time.Second, func(fields map[string]interface{}) bool {
		return int(fields["entity_id"].(float64)) == int(addedBySoldier["soldier_id"].(float64))
	})
	if int(removedBySoldier["entity_id"].(float64)) == 0 {
		t.Fatalf("bad official soldier-id RemoveSoldierRequest echo: %#v", removedBySoldier)
	}

	mustWriteV2(t, c, `{"ChangeTeamRequest":{"entity_id":`+itoa(connID)+`}}`)
	teamFields := readVariantMatching(t, c, "TeamInfo", 2*time.Second, func(fields map[string]interface{}) bool {
		return int(fields["entity_id"].(float64)) == playerID
	})
	if teamFields["team"] == "" {
		t.Fatalf("bad alias ChangeTeamRequest echo: %#v", teamFields)
	}

	mustWriteV2(t, c, `{"FaceGraphicsRequest":{"soldier_id":`+itoa(soldierID)+`,"graphics":"regular_eyes"}}`)
	faceFields := readVariantMatching(t, c, "FaceGraphicsInfo", 2*time.Second, func(fields map[string]interface{}) bool {
		return int(fields["entity_id"].(float64)) == soldierID
	})
	if faceFields["graphics_str"] != "regular_eyes" {
		t.Fatalf("bad alias FaceGraphicsRequest echo: %#v", faceFields)
	}

	mustWriteV2(t, c, `{"ChatMessageRequest":{"player_id":`+itoa(playerID)+`,"text":"alias chat"}}`)
	chatFields := readVariantMatching(t, c, "ChatMessage", 2*time.Second, func(fields map[string]interface{}) bool {
		return fields["message"] == "alias chat"
	})
	if int(chatFields["entity_id"].(float64)) != connID || int(chatFields["player_id"].(float64)) != playerID {
		t.Fatalf("bad alias ChatMessage echo: %#v", chatFields)
	}

	mustWriteV2(t, c, `{"GameStartRequest":{}}`)
	readVariantsUntil(t, c, 2*time.Second, "GameStateInfo", "TurnInfo", "LifeInfo")
	mustWriteV2(t, c, `{"FunctionUpdateRequest":{"entity_id":`+itoa(connID)+`,"function_str":"x"}}`)
	waitForV2Functions(t, s, map[int]string{playerID: "x"})
	aliasFunctionFields := readVariantMatching(t, c, "FunctionInfo", 2*time.Second, func(fields map[string]interface{}) bool {
		return int(fields["owner_id"].(float64)) == playerID
	})
	if aliasFunctionFields["function"] != "x" || aliasFunctionFields["corrected_function"] != "x" {
		t.Fatalf("bad alias FunctionInfo echo: %#v", aliasFunctionFields)
	}
	mustWriteV2(t, c, `{"FunctionFireRequest":{"entity_id":`+itoa(connID)+`}}`)
	if seen := readVariantsUntil(t, c, 2*time.Second, "FunctionFire"); !seen["FunctionFire"] {
		t.Fatalf("missing alias FunctionFire echo: %#v", seen)
	}
}

func TestGraphwar2CompatRoomPublishOptionsIncludeAddress(t *testing.T) {
	s, err := StartGraphwar2CompatRoom("Publish Addr", 0)
	if err != nil {
		t.Fatalf("start v2 room: %v", err)
	}
	defer s.Close()

	s.mu.Lock()
	s.publishHost = "203.0.113.7"
	s.publishLocalHost = "127.0.0.1"
	opts := s.publishOptionsSnapshotLocked()
	s.mu.Unlock()

	want := net.JoinHostPort("203.0.113.7", itoa(s.Port()))
	if opts.Address != want {
		t.Fatalf("publish options missing address: got %#v want %q", opts, want)
	}
	if opts.Host != "203.0.113.7" || opts.Port != s.Port() {
		t.Fatalf("bad publish target: %#v", opts)
	}
	if opts.LocalHost != "127.0.0.1" {
		t.Fatalf("bad relay target host: %#v", opts)
	}

	fields := s.gameInfoFields()
	info := fields["game_info"].(map[string]interface{})
	if info["address"] != want {
		t.Fatalf("GameInfo address does not match publisher address: got %#v want %q", info["address"], want)
	}
}

func TestGraphwar2CompatLocalRoomsPreferIPv6Loopback(t *testing.T) {
	s, err := StartGraphwar2CompatRoom("Local Detect", 0)
	if err != nil {
		t.Fatalf("start v2 room: %v", err)
	}
	defer s.Close()

	var got *Graphwar2LocalRoom
	rooms := graphwar2CompatLocalRooms()
	for i := range rooms {
		if rooms[i].Port == s.Port() {
			got = &rooms[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("compat room not listed: %#v", rooms)
	}
	wantAddr := net.JoinHostPort("::1", itoa(s.Port()))
	if got.Host != "::1" || got.Address != wantAddr || got.ListenHost != "::" {
		t.Fatalf("bad compat local room: got %#v want host ::1 address %q listen ::", *got, wantAddr)
	}
}

func TestGraphwar2CompatRoomPublisherUpdateDoesNotDeadlock(t *testing.T) {
	s, err := StartGraphwar2CompatRoom("Publisher No Deadlock", 0)
	if err != nil {
		t.Fatalf("start v2 room: %v", err)
	}
	defer s.Close()
	s.publisher = &Graphwar2OfficialPublisher{}

	c, _, playerID := connectV2TestPlayer(t, s.Port())
	defer c.Close()
	readVariantMatching(t, c, "SoldierInfo", 2*time.Second, func(fields map[string]interface{}) bool {
		return int(fields["player_id"].(float64)) == playerID
	})

	mustWriteV2(t, c, `{"AddSoldierRequest":{"player_id":`+itoa(playerID)+`}}`)
	added := readVariantMatching(t, c, "SoldierInfo", 2*time.Second, func(fields map[string]interface{}) bool {
		return int(fields["player_id"].(float64)) == playerID
	})
	if int(added["player_id"].(float64)) != playerID {
		t.Fatalf("bad soldier add after publisher update: %#v", added)
	}
}

func TestGraphwar2CompatRoomRemovePlayerBroadcastsSoldiers(t *testing.T) {
	s, err := StartGraphwar2CompatRoom("V2 Remove Player", 0)
	if err != nil {
		t.Fatalf("start v2 room: %v", err)
	}
	defer s.Close()

	observer, _, leaderID := connectV2TestPlayer(t, s.Port())
	defer observer.Close()
	target, _, targetID := connectV2TestPlayer(t, s.Port())
	defer target.Close()
	waitForV2Players(t, s, leaderID, targetID)

	targetSoldiers := snapshotV2PlayerSoldiers(t, s, targetID)
	if len(targetSoldiers) == 0 {
		t.Fatalf("target player has no soldiers")
	}
	mustWriteV2(t, observer, `{"RemovePlayerRequest":{"player_id":`+itoa(targetID)+`}}`)
	assertV2RemovedEntities(t, observer, append(append([]int{}, targetSoldiers...), targetID)...)
}

func TestGraphwar2CompatRoomDisconnectBroadcastsSoldiers(t *testing.T) {
	s, err := StartGraphwar2CompatRoom("V2 Disconnect Player", 0)
	if err != nil {
		t.Fatalf("start v2 room: %v", err)
	}
	defer s.Close()

	observer, _, observerID := connectV2TestPlayer(t, s.Port())
	defer observer.Close()
	target, _, targetID := connectV2TestPlayer(t, s.Port())
	waitForV2Players(t, s, observerID, targetID)

	targetSoldiers := snapshotV2PlayerSoldiers(t, s, targetID)
	if len(targetSoldiers) == 0 {
		t.Fatalf("target player has no soldiers")
	}
	_ = target.Close()
	assertV2RemovedEntities(t, observer, append(append([]int{}, targetSoldiers...), targetID)...)
}

func TestGraphwar2CompatRoomOfficialHandshakeEvents(t *testing.T) {
	s, err := StartGraphwar2CompatRoom("V2 Handshake", 0)
	if err != nil {
		t.Fatalf("start v2 room: %v", err)
	}
	defer s.Close()

	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", itoa(s.Port())), 2*time.Second)
	if err != nil {
		t.Fatalf("dial v2 room: %v", err)
	}
	defer c.Close()

	mustWriteV2(t, c, `{"NewConnectionRequest":{"major_version":"2.0","minor_version":"2.0"}}`)
	seen := readVariantsUntil(t, c, 2*time.Second, "ConnectedToServer", "NewConnection", "VersionInfo", "RankInfo", "GameInfo")
	for _, want := range []string{"ConnectedToServer", "NewConnection", "VersionInfo", "RankInfo", "GameInfo"} {
		if !seen[want] {
			t.Fatalf("missing handshake event %s in %#v", want, seen)
		}
	}
}

func TestGraphwar2CompatRoomOfficialUIStateEvents(t *testing.T) {
	s, err := StartGraphwar2CompatRoom("V2 UI State", 0)
	if err != nil {
		t.Fatalf("start v2 room: %v", err)
	}
	defer s.Close()

	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", itoa(s.Port())), 2*time.Second)
	if err != nil {
		t.Fatalf("dial v2 room: %v", err)
	}
	defer c.Close()

	mustWriteV2(t, c, `{"NewConnectionRequest":{"major_version":"2.0","minor_version":"2.0"}}`)
	readVariant(t, c, "ConnectedToServer", 2*time.Second)
	connFields := readVariantMatching(t, c, "NewConnection", 2*time.Second, nil)
	connID := int(connFields["connection_id"].(float64))
	gameInfo := readVariantMatching(t, c, "GameInfo", 2*time.Second, func(fields map[string]interface{}) bool {
		info, _ := fields["game_info"].(map[string]interface{})
		return info != nil && info["room_name"] == "V2 UI State"
	})
	info := gameInfo["game_info"].(map[string]interface{})
	if info["axis_mode"] != "EveryUnit" || info["turn_mode"] != "SimultaneousTurns" || info["time_mode"] != "Timer1m" {
		t.Fatalf("bad GameInfo snapshot: %#v", info)
	}

	mustWriteV2(t, c, `{"NewPlayerRequest":{"connection_id":`+itoa(connID)+`}}`)
	playerFields := readVariantMatching(t, c, "PlayerInfo", 2*time.Second, func(fields map[string]interface{}) bool {
		return int(fields["connection_id"].(float64)) == connID
	})
	playerID := int(playerFields["player_id"].(float64))
	colorFields := readVariantMatching(t, c, "ColorInfo", 2*time.Second, func(fields map[string]interface{}) bool {
		return int(fields["entity_id"].(float64)) == playerID
	})
	color, _ := colorFields["color"].(map[string]interface{})
	if color == nil || color["r"] == nil || color["g"] == nil || color["b"] == nil {
		t.Fatalf("bad ColorInfo: %#v", colorFields)
	}

	mustWriteV2(t, c, `{"AxisModeChangeRequest":{}}`)
	updated := readVariantMatching(t, c, "GameInfo", 2*time.Second, func(fields map[string]interface{}) bool {
		info, _ := fields["game_info"].(map[string]interface{})
		return info != nil && info["axis_mode"] != "EveryUnit"
	})
	updatedInfo := updated["game_info"].(map[string]interface{})
	if updatedInfo["current_player_count"].(float64) < 1 {
		t.Fatalf("bad updated GameInfo player count: %#v", updatedInfo)
	}
}

func TestGraphwar2CompatRoomRankRequest(t *testing.T) {
	s, err := StartGraphwar2CompatRoom("V2 Rank", 0)
	if err != nil {
		t.Fatalf("start v2 room: %v", err)
	}
	defer s.Close()

	c, connID, playerID := connectV2TestPlayer(t, s.Port())
	defer c.Close()

	mustWriteV2(t, c, `{"RankRequest":{"entity_id":`+itoa(connID)+`}}`)
	connRank := readVariantMatching(t, c, "RankInfo", 2*time.Second, func(fields map[string]interface{}) bool {
		return int(fields["entity_id"].(float64)) == connID
	})
	if int(connRank["rank"].(float64)) != 1 {
		t.Fatalf("bad connection rank: %#v", connRank)
	}

	mustWriteV2(t, c, `{"RankRequest":{"player_id":`+itoa(playerID)+`}}`)
	playerRank := readVariantMatching(t, c, "RankInfo", 2*time.Second, func(fields map[string]interface{}) bool {
		return int(fields["entity_id"].(float64)) == playerID
	})
	if int(playerRank["rank"].(float64)) != 1 {
		t.Fatalf("bad player rank: %#v", playerRank)
	}
}

func TestGraphwar2CompatRoomAcceptsFunctionsCalculatedEcho(t *testing.T) {
	s, err := StartGraphwar2CompatRoom("V2 FunctionsCalculated", 0)
	if err != nil {
		t.Fatalf("start v2 room: %v", err)
	}
	defer s.Close()

	c, _, playerID := connectV2TestPlayer(t, s.Port())
	defer c.Close()
	mustWriteV2(t, c, `{"GameStartRequest":{}}`)
	readVariantsUntil(t, c, 2*time.Second, "GameStateInfo", "TurnInfo")

	mustWriteV2(t, c, `{"FunctionsCalculated":{}}`)
	if seen := readVariantsUntil(t, c, 300*time.Millisecond, "EndOfTurnInfo", "TurnCountInfo"); seen["EndOfTurnInfo"] || seen["TurnCountInfo"] {
		t.Fatalf("official FunctionsCalculated echo advanced the turn: %#v", seen)
	}

	mustWriteV2(t, c, `{"FunctionUpdateRequest":{"player_id":`+itoa(playerID)+`,"function":"0"}}`)
	waitForV2Functions(t, s, map[int]string{playerID: "0"})
	functionFields := readVariantMatching(t, c, "FunctionInfo", 2*time.Second, func(fields map[string]interface{}) bool {
		return int(fields["owner_id"].(float64)) == playerID
	})
	if functionFields["function"] != "0" || functionFields["corrected_function"] != "0" {
		t.Fatalf("bad official FunctionInfo fields: %#v", functionFields)
	}
	mustWriteV2(t, c, `{"TickRequest":{}}`)
	if fields := readVariantMatching(t, c, "TickReply", 2*time.Second, nil); fields == nil {
		t.Fatal("connection did not stay usable after FunctionsCalculated echo")
	}
}

func TestGraphwar2CompatRoomSimultaneousTurnResolution(t *testing.T) {
	s, err := StartGraphwar2CompatRoom("V2 Battle", 0)
	if err != nil {
		t.Fatalf("start v2 room: %v", err)
	}
	defer s.Close()

	c1, conn1, player1 := connectV2TestPlayer(t, s.Port())
	defer c1.Close()
	c2, _, player2 := connectV2TestPlayer(t, s.Port())
	defer c2.Close()
	if conn1 == 0 || player1 == 0 || player2 == 0 || player1 == player2 {
		t.Fatalf("bad ids conn=%d p1=%d p2=%d", conn1, player1, player2)
	}
	waitForV2Players(t, s, player1, player2)
	readVariantMatching(t, c1, "PlayerInfo", 2*time.Second, func(fields map[string]interface{}) bool {
		return int(fields["player_id"].(float64)) == player2
	})

	mustWriteV2(t, c1, `{"GameStartRequest":{}}`)
	seenStart := readVariantsUntil(t, c1, 2*time.Second, "GameStateInfo", "TurnInfo", "LifeInfo")
	for _, want := range []string{"GameStateInfo", "TurnInfo", "LifeInfo"} {
		if !seenStart[want] {
			t.Fatalf("missing start event %s in %#v", want, seenStart)
		}
	}

	mustWriteV2(t, c1, `{"FunctionUpdateRequest":{"player_id":`+itoa(player1)+`,"function":"x"}}`)
	mustWriteV2(t, c2, `{"FunctionUpdateRequest":{"player_id":`+itoa(player2)+`,"function":"-x"}}`)
	waitForV2Functions(t, s, map[int]string{player1: "x", player2: "-x"})

	mustWriteV2(t, c1, `{"FunctionFireRequest":{"player_id":`+itoa(player1)+`}}`)
	if seen := readVariantsUntil(t, c1, 400*time.Millisecond, "FunctionsCalculated"); seen["FunctionsCalculated"] {
		t.Fatal("simultaneous turn resolved before every active player confirmed")
	}
	mustWriteV2(t, c2, `{"FunctionFireRequest":{"player_id":`+itoa(player2)+`}}`)

	seen := readVariantsUntil(t, c1, 3*time.Second, "FunctionFire", "FunctionActive", "FunctionPoints", "EffectInfo", "FunctionsCalculated", "EndOfTurnInfo", "TurnCountInfo", "TurnInfo")
	for _, want := range []string{"FunctionFire", "FunctionActive", "FunctionPoints", "EffectInfo", "FunctionsCalculated", "EndOfTurnInfo", "TurnCountInfo", "TurnInfo"} {
		if !seen[want] {
			t.Fatalf("missing resolution event %s in %#v", want, seen)
		}
	}
}

func TestGraphwar2CompatRoomBotAutoSubmitsSimultaneousTurn(t *testing.T) {
	s, err := StartGraphwar2CompatRoom("V2 Bot Battle", 0)
	if err != nil {
		t.Fatalf("start v2 room: %v", err)
	}
	defer s.Close()

	c, connID, playerID := connectV2TestPlayer(t, s.Port())
	defer c.Close()
	if connID == 0 || playerID == 0 {
		t.Fatalf("bad ids conn=%d player=%d", connID, playerID)
	}
	mustWriteV2(t, c, `{"NewBotRequest":{"connection_id":`+itoa(connID)+`,"level":3}}`)
	botFields := readVariantMatching(t, c, "BotAdded", 2*time.Second, func(fields map[string]interface{}) bool {
		return int(fields["level"].(float64)) == 3
	})
	botID := int(botFields["player_id"].(float64))
	if botID == 0 || botID == playerID {
		t.Fatalf("bad bot fields: %#v", botFields)
	}
	waitForV2Players(t, s, playerID, botID)

	mustWriteV2(t, c, `{"GameStartRequest":{}}`)
	botFunction := readVariantMatching(t, c, "FunctionInfo", 2*time.Second, func(fields map[string]interface{}) bool {
		return int(fields["owner_id"].(float64)) == botID && fields["function"] != ""
	})
	if botFunction["corrected_function"] == "" {
		t.Fatalf("bad bot FunctionInfo: %#v", botFunction)
	}
	botFire := readVariantMatching(t, c, "FunctionFire", 2*time.Second, func(fields map[string]interface{}) bool {
		return int(fields["soldier_id"].(float64)) > 0
	})
	if int(botFire["soldier_id"].(float64)) == 0 {
		t.Fatalf("bad bot FunctionFire: %#v", botFire)
	}

	mustWriteV2(t, c, `{"FunctionUpdateRequest":{"player_id":`+itoa(playerID)+`,"function":"0"}}`)
	waitForV2Functions(t, s, map[int]string{playerID: "0"})
	readVariantMatching(t, c, "FunctionInfo", 2*time.Second, func(fields map[string]interface{}) bool {
		return int(fields["owner_id"].(float64)) == playerID
	})
	mustWriteV2(t, c, `{"FunctionFireRequest":{"player_id":`+itoa(playerID)+`}}`)
	if seen := readVariantsUntil(t, c, 3*time.Second, "FunctionsCalculated", "EndOfTurnInfo", "TurnCountInfo"); !seen["FunctionsCalculated"] || !seen["EndOfTurnInfo"] || !seen["TurnCountInfo"] {
		t.Fatalf("bot simultaneous turn did not resolve after local player confirmed: %#v", seen)
	}
}

func TestGraphwar2CompatRoomBotOnlyAutoResolvesTurn(t *testing.T) {
	s, err := StartGraphwar2CompatRoom("V2 Bot Only", 0)
	if err != nil {
		t.Fatalf("start v2 room: %v", err)
	}
	defer s.Close()

	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", itoa(s.Port())), 2*time.Second)
	if err != nil {
		t.Fatalf("dial v2 room: %v", err)
	}
	defer c.Close()

	mustWriteV2(t, c, `{"NewConnectionRequest":{"major_version":"2.0","minor_version":"2.0"}}`)
	readVariant(t, c, "ConnectedToServer", 2*time.Second)
	_, connFields := readVariant(t, c, "NewConnection", 2*time.Second)
	connID := int(connFields["connection_id"].(float64))

	mustWriteV2(t, c, `{"NewBotRequest":{"connection_id":`+itoa(connID)+`,"level":2}}`)
	botA := readVariantMatching(t, c, "BotAdded", 2*time.Second, func(fields map[string]interface{}) bool {
		return int(fields["level"].(float64)) == 2
	})
	mustWriteV2(t, c, `{"NewBotRequest":{"connection_id":`+itoa(connID)+`,"level":4}}`)
	botB := readVariantMatching(t, c, "BotAdded", 2*time.Second, func(fields map[string]interface{}) bool {
		return int(fields["level"].(float64)) == 4
	})
	botAID := int(botA["player_id"].(float64))
	botBID := int(botB["player_id"].(float64))
	if botAID == 0 || botBID == 0 || botAID == botBID {
		t.Fatalf("bad bot ids: a=%#v b=%#v", botA, botB)
	}
	waitForV2Players(t, s, botAID, botBID)

	mustWriteV2(t, c, `{"GameStartRequest":{}}`)
	seen := readVariantsUntil(t, c, 4*time.Second, "FunctionInfo", "FunctionFire", "FunctionsCalculated", "EndOfTurnInfo", "TurnCountInfo")
	for _, want := range []string{"FunctionInfo", "FunctionFire", "FunctionsCalculated", "EndOfTurnInfo", "TurnCountInfo"} {
		if !seen[want] {
			t.Fatalf("bot-only room missing %s in %#v", want, seen)
		}
	}
}

func TestGraphwar2NormalFunctionSamplerHitsSoldier(t *testing.T) {
	s := &Graphwar2Server{
		function:  "NormalFunction",
		players:   map[int]*v2ServerPlayer{},
		order:     []int{1, 2},
		obstacles: nil,
	}
	s.players[1] = &v2ServerPlayer{
		id:       1,
		team:     "Team1",
		soldiers: []int{1},
		pos:      map[int]v2Point{1: {x: 120, y: 225}},
		alive:    map[int]bool{1: true},
		lastFunc: "0",
	}
	s.players[2] = &v2ServerPlayer{
		id:       2,
		team:     "Team2",
		soldiers: []int{2},
		pos:      map[int]v2Point{2: {x: 220, y: 225}},
		alive:    map[int]bool{2: true},
	}
	result, ok := s.sampleFunctionShotLocked(v2Shot{playerID: 1, soldierID: 1, start: s.players[1].pos[1], function: "0"})
	if !ok {
		t.Fatal("sampler did not evaluate normal function")
	}
	if result.targetPlayer != 2 || result.targetSoldier != 2 || result.obstacleHit {
		t.Fatalf("expected hit on player 2 soldier 2, got %#v", result)
	}
}

func TestGraphwar2NormalFunctionSamplerHitsObstacle(t *testing.T) {
	s := &Graphwar2Server{
		function:  "NormalFunction",
		players:   map[int]*v2ServerPlayer{},
		order:     []int{1, 2},
		obstacles: []v2Obstacle{{x: 180, y: 225, r: 12}},
	}
	s.players[1] = &v2ServerPlayer{
		id:       1,
		team:     "Team1",
		soldiers: []int{1},
		pos:      map[int]v2Point{1: {x: 120, y: 225}},
		alive:    map[int]bool{1: true},
		lastFunc: "0",
	}
	s.players[2] = &v2ServerPlayer{
		id:       2,
		team:     "Team2",
		soldiers: []int{2},
		pos:      map[int]v2Point{2: {x: 260, y: 225}},
		alive:    map[int]bool{2: true},
	}
	result, ok := s.sampleFunctionShotLocked(v2Shot{playerID: 1, soldierID: 1, start: s.players[1].pos[1], function: "0"})
	if !ok {
		t.Fatal("sampler did not evaluate normal function")
	}
	if !result.obstacleHit || result.targetSoldier != 0 {
		t.Fatalf("expected obstacle hit before soldier, got %#v", result)
	}
}

func TestGraphwar2ODESamplersHitSoldier(t *testing.T) {
	for _, mode := range []string{"DiffEqFunction", "SecondDiffEqFunction"} {
		t.Run(mode, func(t *testing.T) {
			s := &Graphwar2Server{
				function:  mode,
				players:   map[int]*v2ServerPlayer{},
				order:     []int{1, 2},
				obstacles: nil,
			}
			s.players[1] = &v2ServerPlayer{
				id:       1,
				team:     "Team1",
				soldiers: []int{1},
				pos:      map[int]v2Point{1: {x: 120, y: 225}},
				alive:    map[int]bool{1: true},
				lastFunc: "0",
			}
			s.players[2] = &v2ServerPlayer{
				id:       2,
				team:     "Team2",
				soldiers: []int{2},
				pos:      map[int]v2Point{2: {x: 220, y: 225}},
				alive:    map[int]bool{2: true},
			}
			result, ok := s.sampleFunctionShotLocked(v2Shot{playerID: 1, soldierID: 1, start: s.players[1].pos[1], function: "0"})
			if !ok {
				t.Fatalf("%s sampler did not evaluate function", mode)
			}
			if result.targetPlayer != 2 || result.targetSoldier != 2 || result.obstacleHit {
				t.Fatalf("%s expected hit on player 2 soldier 2, got %#v", mode, result)
			}
		})
	}
}

func TestGraphwar2ObstacleGeneration(t *testing.T) {
	obstacles := generateV2Obstacles(12345)
	if len(obstacles) != v2ObstacleCount {
		t.Fatalf("expected %d obstacles, got %d", v2ObstacleCount, len(obstacles))
	}
	for i, ob := range obstacles {
		if ob.r < gameRadiusToPixel(0.8) || ob.r > gameRadiusToPixel(7.6) {
			t.Fatalf("obstacle %d radius out of range: %#v", i, ob)
		}
		if ob.x-ob.r < 0 || ob.x+ob.r > v2PlaneLength || ob.y-ob.r < 0 || ob.y+ob.r > v2PlaneHeight {
			t.Fatalf("obstacle %d out of bounds: %#v", i, ob)
		}
		for _, spawn := range v2SpawnSafePoints {
			if distSq(ob.x, ob.y, spawn.x, spawn.y) < sq(ob.r+v2SpawnSafeRadius) {
				t.Fatalf("obstacle %d overlaps spawn area: %#v spawn=%#v", i, ob, spawn)
			}
		}
	}
}

func TestGraphwar2CoordinateRoundTrip(t *testing.T) {
	for _, px := range []float64{0, 120, 385, 650, 770} {
		gx := pxToGameX(px)
		back := gameToV2Pixel(gx, 0, false).x
		if math.Abs(back-px) > 0.000001 {
			t.Fatalf("x round trip failed px=%v gx=%v back=%v", px, gx, back)
		}
	}
	for _, py := range []float64{0, 90, 225, 360, 450} {
		gy := pyToGameY(py)
		back := gameToV2Pixel(0, gy, false).y
		if math.Abs(back-py) > 0.000001 {
			t.Fatalf("y round trip failed py=%v gy=%v back=%v", py, gy, back)
		}
	}
	for _, r := range []float64{7, 12, 38, 117} {
		if math.Abs(gameRadiusToPixel(radiusToGame(r))-r) > 0.000001 {
			t.Fatalf("radius round trip failed r=%v", r)
		}
	}
}

func connectV2TestPlayer(t *testing.T, port int) (net.Conn, int, int) {
	t.Helper()
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", itoa(port)), 2*time.Second)
	if err != nil {
		t.Fatalf("dial v2 room: %v", err)
	}
	mustWriteV2(t, c, `{"NewConnectionRequest":{"major_version":"2.0","minor_version":"2.0"}}`)
	readVariant(t, c, "ConnectedToServer", 2*time.Second)
	_, connFields := readVariant(t, c, "NewConnection", 2*time.Second)
	connID := int(connFields["connection_id"].(float64))
	mustWriteV2(t, c, `{"NewPlayerRequest":{"connection_id":`+itoa(connID)+`}}`)
	playerFields := readVariantMatching(t, c, "PlayerInfo", 2*time.Second, func(fields map[string]interface{}) bool {
		return int(fields["connection_id"].(float64)) == connID
	})
	playerID := int(playerFields["player_id"].(float64))
	return c, connID, playerID
}

func waitForV2Players(t *testing.T, s *Graphwar2Server, ids ...int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		ok := true
		for _, id := range ids {
			if s.players[id] == nil {
				ok = false
				break
			}
		}
		s.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for v2 players %v", ids)
}

func waitForV2Functions(t *testing.T, s *Graphwar2Server, want map[int]string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		ok := true
		snapshot := map[int]string{}
		for id, fn := range want {
			p := s.players[id]
			if p == nil {
				ok = false
				continue
			}
			snapshot[id] = p.lastFunc
			if p.lastFunc != fn {
				ok = false
			}
		}
		s.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.mu.Lock()
	snapshot := map[int]string{}
	for id := range want {
		if p := s.players[id]; p != nil {
			snapshot[id] = p.lastFunc
		}
	}
	s.mu.Unlock()
	t.Fatalf("timeout waiting for v2 functions want=%v got=%v", want, snapshot)
}

func snapshotV2PlayerSoldiers(t *testing.T, s *Graphwar2Server, playerID int) []int {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.players[playerID]
	if p == nil {
		t.Fatalf("missing v2 player %d", playerID)
	}
	return append([]int{}, p.soldiers...)
}

func mustWriteV2(t *testing.T, c net.Conn, payload string) {
	t.Helper()
	if err := writeV2Frame(c, []byte(payload)); err != nil {
		t.Fatalf("write %s: %v", payload, err)
	}
}

func readVariant(t *testing.T, c net.Conn, want string, timeout time.Duration) (string, map[string]interface{}) {
	t.Helper()
	fields := readVariantMatching(t, c, want, timeout, nil)
	return want, fields
}

func readVariantMatching(t *testing.T, c net.Conn, want string, timeout time.Duration, match func(map[string]interface{}) bool) map[string]interface{} {
	t.Helper()
	deadline := time.Now().Add(timeout)
	seen := []string{}
	for time.Now().Before(deadline) {
		_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		payload, err := readV2Frame(c)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			t.Fatalf("read %s: %v", want, err)
		}
		var ev map[string]map[string]interface{}
		if err := json.Unmarshal(payload, &ev); err != nil {
			t.Fatalf("bad json %s: %v", string(payload), err)
		}
		for variant, fields := range ev {
			seen = append(seen, variant)
			if variant == want && (match == nil || match(fields)) {
				return fields
			}
		}
	}
	t.Fatalf("timeout waiting for %s after seeing %v", want, seen)
	return nil
}

func assertV2RemovedEntities(t *testing.T, c net.Conn, ids ...int) {
	t.Helper()
	want := map[int]bool{}
	for _, id := range ids {
		want[id] = true
	}
	deadline := time.Now().Add(2 * time.Second)
	seen := map[int]bool{}
	for time.Now().Before(deadline) {
		if len(seen) == len(want) {
			return
		}
		_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		payload, err := readV2Frame(c)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			t.Fatalf("read EntityRemoved: %v", err)
		}
		var ev map[string]map[string]interface{}
		if err := json.Unmarshal(payload, &ev); err != nil {
			t.Fatalf("bad json %s: %v", string(payload), err)
		}
		fields, ok := ev["EntityRemoved"]
		if !ok {
			continue
		}
		id := int(fields["entity_id"].(float64))
		if want[id] {
			seen[id] = true
		}
	}
	t.Fatalf("timeout waiting for removed entities want=%v seen=%v", want, seen)
}

func readVariantsUntil(t *testing.T, c net.Conn, timeout time.Duration, wants ...string) map[string]bool {
	t.Helper()
	wantSet := map[string]bool{}
	for _, w := range wants {
		wantSet[w] = true
	}
	seen := map[string]bool{}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if len(seen) == len(wants) {
			return seen
		}
		_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		payload, err := readV2Frame(c)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			t.Fatalf("read variants: %v", err)
		}
		var ev map[string]map[string]interface{}
		if err := json.Unmarshal(payload, &ev); err != nil {
			t.Fatalf("bad json %s: %v", string(payload), err)
		}
		for variant := range ev {
			if wantSet[variant] {
				seen[variant] = true
			}
		}
	}
	return seen
}
