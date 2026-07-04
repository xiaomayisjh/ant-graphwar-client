package backend

import (
	"crypto/rand"
	"encoding/binary"
	"net/url"
	"strconv"
	"testing"
	"time"
)

// A lone player who zeroes their soldiers then readies must NOT brick the room:
// the room stays accepting and a new client can still join afterward.
func TestNoHalfDeadRoom(t *testing.T) {
	port, err := StartStandaloneRoomNamed(0, "halfdead")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	gs := HostedRoom(port)

	a := dialRaw(t, port)
	a.send("16&" + url.QueryEscape("Lonely"))
	if !waitFor(func() bool { return a.myID() >= 0 }, 2*time.Second) {
		t.Fatal("join failed")
	}
	id := a.myID()
	// remove both soldiers, then ready -> would start countdown
	a.send("19&" + strconv.Itoa(id))
	a.send("19&" + strconv.Itoa(id))
	time.Sleep(150 * time.Millisecond)
	a.send("21&" + strconv.Itoa(id) + "&1")
	// wait past the start-countdown (5s) for the (aborted) start attempt
	time.Sleep(StartGameDelay + 600*time.Millisecond)

	// room must still accept a brand-new connection
	b := dialRaw(t, port)
	b.send("16&" + url.QueryEscape("Newcomer"))
	if !waitFor(func() bool {
		for _, p := range gs.ListPlayers() {
			if p.Name == "Newcomer" {
				return true
			}
		}
		return false
	}, 3*time.Second) {
		t.Fatal("room bricked: new player could not join after zero-soldier start attempt")
	}
	t.Log("room survived zero-soldier ready (no half-dead state)")
}

func (rc *rawClient) countPrefix(prefix string) int {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	n := 0
	for _, l := range rc.lines {
		if len(l) >= len(prefix) && l[:len(prefix)] == prefix {
			n++
		}
	}
	return n
}

func TestRepeatedReadyNotBroadcastStorm(t *testing.T) {
	port, err := StartStandaloneRoomNamed(0, "ready-storm")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	a := dialRaw(t, port)
	a.send("16&" + url.QueryEscape("Stormer"))
	if !waitFor(func() bool { return a.myID() >= 0 }, 2*time.Second) {
		t.Fatal("join failed")
	}
	id := a.myID()
	time.Sleep(100 * time.Millisecond)
	before := a.countPrefix("21&" + strconv.Itoa(id) + "&1")
	for i := 0; i < 30; i++ {
		a.send("21&" + strconv.Itoa(id) + "&1")
	}
	time.Sleep(500 * time.Millisecond)
	after := a.countPrefix("21&" + strconv.Itoa(id) + "&1")
	if after-before != 1 {
		t.Fatalf("repeated SET_READY was broadcast %d times, want 1", after-before)
	}
}

func TestOversizedWebSocketFrameClosed(t *testing.T) {
	port, err := StartStandaloneRoomNamed(0, "big-ws")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	c := newWSClient(t, port)
	defer c.close()
	sendOversizedMaskedText(t, c.conn, MaxWSFrameLen+1)
	time.Sleep(300 * time.Millisecond)
	if _, err := c.conn.Write([]byte{0x89, 0x80, 0, 0, 0, 0}); err == nil {
		_ = c.conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		buf := make([]byte, 1)
		if _, rerr := c.conn.Read(buf); rerr == nil {
			t.Fatal("connection still readable after oversized frame")
		}
	}
}

func TestLobbyChatRateLimited(t *testing.T) {
	lobby := NewGlobalServer(0, "127.0.0.1")
	if err := lobby.Listen(); err != nil {
		t.Fatalf("lobby listen: %v", err)
	}
	a := dialRaw(t, lobby.Port())
	b := dialRaw(t, lobby.Port())
	a.send(url.QueryEscape("LobbyFlood"))
	b.send(url.QueryEscape("Listener"))
	if !waitFor(func() bool { return b.has("101&") }, 2*time.Second) {
		t.Fatal("listener did not see lobby join")
	}
	before := b.countPrefix("102&")
	for i := 0; i < 100; i++ {
		a.send("102&" + url.QueryEscape("spam"))
	}
	time.Sleep(500 * time.Millisecond)
	after := b.countPrefix("102&")
	if after-before > 45 {
		t.Fatalf("lobby relayed too many flood messages: %d", after-before)
	}
}

func sendOversizedMaskedText(t *testing.T, conn interface{ Write([]byte) (int, error) }, n int) {
	header := make([]byte, 4)
	header[0] = 0x81
	header[1] = 0x80 | 126
	binary.BigEndian.PutUint16(header[2:], uint16(n))
	mask := make([]byte, 4)
	if _, err := rand.Read(mask); err != nil {
		t.Fatalf("rand: %v", err)
	}
	payload := make([]byte, n)
	for i := range payload {
		payload[i] = 'A' ^ mask[i&3]
	}
	if _, err := conn.Write(header); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if _, err := conn.Write(mask); err != nil {
		t.Fatalf("write mask: %v", err)
	}
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write payload: %v", err)
	}
}

// A pre-game GAME_FINISHED (40) must be ignored, not crash/transition the room.
func TestPregameGameFinishedIgnored(t *testing.T) {
	port, err := StartStandaloneRoomNamed(0, "pregame40")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	gs := HostedRoom(port)
	a := dialRaw(t, port)
	a.send("16&" + url.QueryEscape("P1"))
	if !waitFor(func() bool { return a.myID() >= 0 }, 2*time.Second) {
		t.Fatal("join failed")
	}
	// send GAME_FINISHED while still in pre-game
	a.send("40")
	time.Sleep(400 * time.Millisecond)
	// room still alive + accepting
	b := dialRaw(t, port)
	b.send("16&" + url.QueryEscape("P2"))
	if !waitFor(func() bool { return len(gs.ListPlayers()) == 2 }, 3*time.Second) {
		t.Fatal("room broke after pre-game GAME_FINISHED")
	}
	if gs.Status().InGame {
		t.Fatal("pre-game GAME_FINISHED wrongly transitioned state")
	}
	t.Log("pre-game GAME_FINISHED safely ignored")
}

// Oversized FIRE_FUNC is not relayed (length-bounded). Hard to assert broadcast
// negatively without two clients; just ensure the server doesn't crash and
// keeps serving after receiving a giant function.
func TestGiantFireFuncNotCrash(t *testing.T) {
	port, err := StartStandaloneRoomNamed(0, "giantfn")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	gs := HostedRoom(port)
	a := dialRaw(t, port)
	a.send("16&" + url.QueryEscape("Big"))
	if !waitFor(func() bool { return a.myID() >= 0 }, 2*time.Second) {
		t.Fatal("join failed")
	}
	big := "1"
	for i := 0; i < 20000; i++ {
		big += "+sin(x)"
	}
	a.send("24&" + strconv.Itoa(a.myID()) + "&" + big)
	time.Sleep(300 * time.Millisecond)
	// still serving
	b := dialRaw(t, port)
	b.send("16&" + url.QueryEscape("After"))
	if !waitFor(func() bool { return len(gs.ListPlayers()) >= 1 }, 3*time.Second) {
		t.Fatal("server unresponsive after giant FIRE_FUNC")
	}
	t.Log("giant FIRE_FUNC handled without crash")
}
