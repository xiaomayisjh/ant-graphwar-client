package backend

import (
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestV2BridgeFramesJSONEvents(t *testing.T) {
	room, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fake room listen: %v", err)
	}
	defer room.Close()

	got := make(chan string, 1)
	go func() {
		c, err := room.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		payload, err := readV2Frame(c)
		if err != nil {
			got <- "read_error:" + err.Error()
			return
		}
		got <- string(payload)
		_ = writeV2Frame(c, []byte(`{"TickReply":{"current_tick":123}}`))
	}()

	b := NewV2Bridge()
	port, err := b.Start()
	if err != nil {
		t.Fatalf("v2 bridge start: %v", err)
	}

	c := newWSClient(t, port)
	defer c.close()
	targetPort := room.Addr().(*net.TCPAddr).Port
	c.sendText(`{"host":"127.0.0.1","port":` + strconv.Itoa(targetPort) + `}`)
	if !waitFor(func() bool { return c.hasCtrl("connected") }, 2*time.Second) {
		t.Fatalf("bridge never connected to fake v2 room")
	}

	c.sendText(`{"TickRequest":{}}`)
	select {
	case event := <-got:
		if event != `{"TickRequest":{}}` {
			t.Fatalf("fake room got %q", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("fake room did not receive framed event")
	}
	if !waitFor(func() bool { return c.hasLine(`{"TickReply"`) }, 2*time.Second) {
		t.Fatalf("bridge did not relay v2 reply, got %#v", c.snapshot())
	}
}

func TestV2BridgeRejectsInvalidEventJSON(t *testing.T) {
	b := NewV2Bridge()
	port, err := b.Start()
	if err != nil {
		t.Fatalf("v2 bridge start: %v", err)
	}

	room, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fake room listen: %v", err)
	}
	defer room.Close()
	go func() {
		c, err := room.Accept()
		if err == nil {
			defer c.Close()
			_, _ = readV2Frame(c)
		}
	}()

	c := newWSClient(t, port)
	defer c.close()
	c.sendText(`{"host":"127.0.0.1","port":` + strconv.Itoa(room.Addr().(*net.TCPAddr).Port) + `}`)
	if !waitFor(func() bool { return c.hasCtrl("connected") }, 2*time.Second) {
		t.Fatalf("bridge never connected to fake v2 room")
	}
	c.sendText(`not json`)
	if !waitFor(func() bool { return c.hasCtrl("error") }, 2*time.Second) {
		t.Fatalf("bridge did not close after invalid v2 event")
	}
}

func TestV2BridgeToLiveGraphwar2Room(t *testing.T) {
	portRaw := os.Getenv("GW2_LIVE_PORT")
	if portRaw == "" {
		t.Skip("set GW2_LIVE_PORT to run against a live Graphwar II room")
	}
	host := os.Getenv("GW2_LIVE_HOST")
	if host == "" {
		host = "::1"
	}
	targetPort, err := strconv.Atoi(portRaw)
	if err != nil {
		t.Fatalf("bad GW2_LIVE_PORT: %v", err)
	}

	b := NewV2Bridge()
	port, err := b.Start()
	if err != nil {
		t.Fatalf("v2 bridge start: %v", err)
	}
	c := newWSClient(t, port)
	defer c.close()
	c.sendText(`{"host":"` + host + `","port":` + strconv.Itoa(targetPort) + `}`)
	if !waitFor(func() bool { return c.hasCtrl("connected") }, 3*time.Second) {
		t.Fatalf("bridge did not connect to live room %s:%d", host, targetPort)
	}
	c.sendText(`{"NewConnectionRequest":{"major_version":"2.0","minor_version":"2.0"}}`)
	c.sendText(`{"TickRequest":{}}`)

	has := func(needle string) bool {
		for _, line := range c.snapshot() {
			if strings.Contains(line, needle) {
				return true
			}
		}
		return false
	}
	if !waitFor(func() bool { return has(`"NewConnection"`) && has(`"TickReply"`) }, 5*time.Second) {
		t.Fatalf("live room did not return NewConnection and TickReply through bridge; got %#v", c.snapshot())
	}
}
