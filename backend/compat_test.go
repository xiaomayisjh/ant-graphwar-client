package backend

import (
	"net/url"
	"strconv"
	"testing"
	"time"
)

func TestGraphwarIRoomMatchesOfficialLeaderEditRules(t *testing.T) {
	port, err := StartStandaloneRoomNamed(0, "compat")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	a := dialRaw(t, port)
	a.send("16&" + url.QueryEscape("Leader"))
	if !waitFor(func() bool { return a.myID() >= 0 }, 2*time.Second) {
		t.Fatal("leader join failed")
	}
	b := dialRaw(t, port)
	b.send("16&" + url.QueryEscape("Guest"))
	if !waitFor(func() bool { return b.myID() >= 0 }, 2*time.Second) {
		t.Fatal("guest join failed")
	}
	leaderID := a.myID()
	guestID := b.myID()

	// Official GraphServer lets only the owner or the leader edit a player.
	b.send("17&" + strconv.Itoa(leaderID))
	time.Sleep(200 * time.Millisecond)
	if a.countPrefix("17&"+strconv.Itoa(leaderID)) != 0 {
		t.Fatal("non-leader changed another player's soldiers")
	}
	a.send("17&" + strconv.Itoa(guestID))
	if !waitFor(func() bool { return b.has("17&" + strconv.Itoa(guestID)) }, 2*time.Second) {
		t.Fatal("leader could not add soldier to another player")
	}
	a.send("19&" + strconv.Itoa(guestID))
	if !waitFor(func() bool { return b.has("19&" + strconv.Itoa(guestID)) }, 2*time.Second) {
		t.Fatal("leader could not remove soldier from another player")
	}

	// Max soldiers per player is 4 in Graphwar I; extra adds are ignored.
	before := b.countPrefix("17&" + strconv.Itoa(guestID))
	for i := 0; i < 6; i++ {
		a.send("17&" + strconv.Itoa(guestID))
	}
	time.Sleep(400 * time.Millisecond)
	after := b.countPrefix("17&" + strconv.Itoa(guestID))
	if after-before != 2 {
		t.Fatalf("soldier max boundary mismatch: got %d accepted adds, want 2", after-before)
	}
}
