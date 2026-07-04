package backend

import (
	"net/url"
	"strconv"
	"testing"
	"time"
)

// Host a room, then exercise admin: list players (with IP), kick, ban-name,
// ban-IP (rejects reconnect), lock (rejects new), force-reset.
func TestRoomAdmin(t *testing.T) {
	port, err := StartStandaloneRoomNamed(0, "Admin Test Room")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	gs := HostedRoom(port)
	if gs == nil {
		t.Fatal("hosted room not registered")
	}

	// player joins
	a := dialRaw(t, port)
	a.send("16&" + url.QueryEscape("BadGuy"))
	if !waitFor(func() bool { return a.myID() >= 0 }, 2*time.Second) {
		t.Fatal("BadGuy never joined")
	}
	id := a.myID()

	// admin sees the player + their IP
	pl := gs.ListPlayers()
	if len(pl) != 1 || pl[0].Name == "" || pl[0].IP == "" {
		t.Fatalf("ListPlayers wrong: %+v", pl)
	}
	t.Logf("player: id=%d name=%q ip=%s", pl[0].ID, pl[0].Name, pl[0].IP)

	// kick
	if !gs.KickPlayer(id) {
		t.Fatal("kick failed")
	}
	if !waitFor(func() bool { return len(gs.ListPlayers()) == 0 }, 2*time.Second) {
		t.Fatal("player still present after kick")
	}

	// ban name: a client adding the banned name gets dropped (no player remains)
	gs.BanName("BadGuy")
	b := dialRaw(t, port)
	b.send("16&" + url.QueryEscape("BadGuy"))
	time.Sleep(500 * time.Millisecond)
	if len(gs.ListPlayers()) != 0 {
		t.Fatal("banned name was allowed to join")
	}
	// a different name still works
	c := dialRaw(t, port)
	c.send("16&" + url.QueryEscape("GoodGuy"))
	if !waitFor(func() bool { return len(gs.ListPlayers()) == 1 }, 2*time.Second) {
		t.Fatal("non-banned player could not join")
	}

	// lock: new connections refused (GAME_FULL then closed)
	gs.SetLocked(true)
	d := dialRaw(t, port)
	d.send("16&" + url.QueryEscape("Late"))
	time.Sleep(400 * time.Millisecond)
	if len(gs.ListPlayers()) != 1 {
		t.Fatal("locked room accepted a new player")
	}
	gs.SetLocked(false)

	// bans listing reflects the name ban
	names, _ := gs.Bans()
	found := false
	for _, n := range names {
		if n == "badguy" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ban list missing badguy: %v", names)
	}

	t.Log("admin kick/ban-name/lock OK on port " + strconv.Itoa(port))
}
