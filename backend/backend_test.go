package backend

import (
	"bufio"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// A raw-TCP test client speaking the line protocol (like an original Java client).
type rawClient struct {
	conn  net.Conn
	mu    sync.Mutex
	lines []string
}

func dialRaw(t *testing.T, port int) *rawClient {
	c, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	rc := &rawClient{conn: c}
	go func() {
		br := bufio.NewReader(c)
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			rc.mu.Lock()
			rc.lines = append(rc.lines, strings.TrimRight(line, "\r\n"))
			rc.mu.Unlock()
		}
	}()
	return rc
}
func (rc *rawClient) send(s string) { rc.conn.Write([]byte(s + "\n")) }
func (rc *rawClient) has(prefix string) bool {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	for _, l := range rc.lines {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}
func (rc *rawClient) myID() int {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	for _, l := range rc.lines {
		p := strings.Split(l, "&")
		if len(p) >= 5 && p[0] == "16" && p[4] == "1" {
			id, _ := strconv.Atoi(p[1])
			return id
		}
	}
	return -1
}
func waitFor(cond func() bool, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

// Two raw clients join a room on the embedded Go GraphServer, ready on opposite
// teams, and the server starts the game — proving the Go relay works.
func TestRoomStartGame(t *testing.T) {
	gs := NewGraphServer(0, Hooks{})
	if err := gs.Listen(); err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer gs.Close()
	port := gs.Port()

	a := dialRaw(t, port)
	a.send("16&Alice") // ADD_PLAYER
	if !waitFor(func() bool { return a.myID() >= 0 }, 2*time.Second) {
		t.Fatal("Alice never got her own ADD_PLAYER echo")
	}
	b := dialRaw(t, port)
	b.send("16&Bob")
	if !waitFor(func() bool { return b.myID() >= 0 }, 2*time.Second) {
		t.Fatal("Bob never joined")
	}
	if !waitFor(func() bool { return a.has("16&" + strconv.Itoa(b.myID())) }, 2*time.Second) {
		t.Fatal("Alice never saw Bob join")
	}

	// opposite teams: Bob -> team2
	b.send("20&2&" + strconv.Itoa(b.myID()))
	time.Sleep(200 * time.Millisecond)
	// both ready
	a.send("21&" + strconv.Itoa(a.myID()) + "&1")
	b.send("21&" + strconv.Itoa(b.myID()) + "&1")

	if !waitFor(func() bool { return a.has("42") }, 3*time.Second) {
		t.Fatal("no START_COUNTDOWN broadcast")
	}
	if !waitFor(func() bool { return a.has("22&") && b.has("22&") }, 8*time.Second) {
		t.Fatal("START_GAME not received by both clients")
	}

	// current player fires; both should receive the relayed FIRE_FUNC
	a.send("24&" + strconv.Itoa(a.myID()) + "&sin(x)")
	if !waitFor(func() bool { return b.has("24&") }, 2*time.Second) {
		t.Fatal("Bob did not receive relayed FIRE_FUNC")
	}
	t.Log("room start + fire relay OK")
}

// Lobby lists a registered room and a client receives LIST_ROOMS.
func TestLobbyListsRoom(t *testing.T) {
	lobby := NewGlobalServer(0, "127.0.0.1")
	if err := lobby.Listen(); err != nil {
		t.Fatalf("lobby listen: %v", err)
	}
	defer lobby.Close()
	lobby.RegisterLocalRoom("TestRoom", "127.0.0.1", 6520)

	c := dialRaw(t, lobby.Port())
	c.send("WebPlayer") // first line = name
	if !waitFor(func() bool { return c.has("104&") }, 2*time.Second) {
		t.Fatal("client never received LIST_ROOMS")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	found := false
	for _, l := range c.lines {
		if strings.HasPrefix(l, "104&") && strings.Contains(l, "TestRoom") {
			found = true
		}
	}
	if !found {
		t.Fatalf("LIST_ROOMS did not contain the registered room: %v", c.lines)
	}
	t.Log("lobby lists room OK")
}

func TestRoomComputerPlayerAutoFires(t *testing.T) {
	gs := NewGraphServer(0, Hooks{})
	if err := gs.Listen(); err != nil {
		t.Fatalf("listen: %v", err)
	}

	a := dialRaw(t, gs.Port())
	a.send("16&Alice")
	if !waitFor(func() bool { return a.myID() >= 0 }, 2*time.Second) {
		t.Fatal("Alice never got her own ADD_PLAYER echo")
	}
	aliceID := a.myID()
	a.send("20&1&" + strconv.Itoa(aliceID))

	bot, ok := gs.AddComputerPlayer("Computer 2", 50)
	if !ok || bot == nil {
		t.Fatal("failed to add computer player")
	}
	gs.mu.Lock()
	bot.team = Team2
	gs.mu.Unlock()

	if !waitFor(func() bool { return a.has("16&" + strconv.Itoa(bot.id)) }, 2*time.Second) {
		t.Fatal("client never saw computer player join")
	}
	a.send("21&" + strconv.Itoa(aliceID) + "&1")
	if !waitFor(func() bool { return a.has("22&") }, 8*time.Second) {
		t.Fatal("START_GAME not received")
	}

	gs.mu.Lock()
	gs.turnIndex = -1
	for i, p := range gs.players {
		if p.id == bot.id {
			gs.turnIndex = i
			break
		}
	}
	gs.maybeScheduleComputerTurnLocked()
	gs.mu.Unlock()

	if !waitFor(func() bool { return a.has("24&" + strconv.Itoa(bot.id) + "&") }, 2*time.Second) {
		t.Fatal("computer player did not broadcast FIRE_FUNC")
	}
}
