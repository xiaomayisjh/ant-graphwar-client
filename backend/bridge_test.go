package backend

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

// Connects the embedded WS<->TCP bridge to the OFFICIAL Graphwar global server
// and checks we receive the real player/room lists — i.e. the desktop app can
// play with worldwide players. Network test: skipped with -short.
func TestBridgeToOfficialServer(t *testing.T) {
	if testing.Short() {
		t.Skip("network test")
	}
	b := NewBridge()
	port, err := b.Start()
	if err != nil {
		t.Fatalf("bridge start: %v", err)
	}

	c := newWSClient(t, port)
	defer c.close()

	// 1) handshake JSON telling the bridge to dial the official lobby
	c.sendText(`{"host":"www.graphwar.com","port":23761}`)
	if !waitFor(func() bool { return c.hasCtrl("connected") }, 12*time.Second) {
		t.Skip("could not reach official server (network/proxy); bridge dial did not connect")
	}
	// 2) first protocol line to the lobby = URL-encoded player name
	c.sendText(url.QueryEscape("DesktopTester"))

	// 3) expect LIST_PLAYERS (103) and LIST_ROOMS (104) back from the official server
	gotPlayers := waitFor(func() bool { return c.hasLine("103&") }, 12*time.Second)
	gotRooms := waitFor(func() bool { return c.hasLine("104&") }, 12*time.Second)
	if !gotPlayers && !gotRooms {
		t.Fatalf("no LIST_PLAYERS/LIST_ROOMS from official server; got: %v", c.lines)
	}
	for _, l := range c.snapshot() {
		if strings.HasPrefix(l, "104&") {
			t.Logf("official LIST_ROOMS: %.120s", l)
		}
		if strings.HasPrefix(l, "103&") {
			t.Logf("official LIST_PLAYERS: %.120s", l)
		}
	}
	// be polite: quit
	c.sendText("106")
	t.Log("bridge reached official server and received live lobby data OK")
}
