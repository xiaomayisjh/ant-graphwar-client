package backend

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestProbeGraphwar2RoomHandshake(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen fake v2 room: %v", err)
	}
	defer ln.Close()
	serverErr := make(chan error, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer c.Close()
		if _, err := readV2Frame(c); err != nil {
			serverErr <- err
			return
		}
		if _, err := readV2Frame(c); err != nil {
			serverErr <- err
			return
		}
		_ = writeV2Frame(c, []byte(`{"ConnectedToServer":{"success":true}}`))
		_ = writeV2Frame(c, []byte(`{"NewConnection":{"connection_id":7}}`))
		time.Sleep(100 * time.Millisecond)
		serverErr <- nil
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res := ProbeGraphwar2Room(ctx, "127.0.0.1", ln.Addr().(*net.TCPAddr).Port)
	if !res.OK {
		select {
		case err := <-serverErr:
			if err != nil {
				t.Fatalf("fake room failed before probe response: %v; probe=%#v", err, res)
			}
		default:
		}
		t.Fatalf("probe failed: %#v", res)
	}
	if len(res.Events) == 0 || res.Events[0] != "ConnectedToServer" {
		t.Fatalf("bad probe events: %#v", res.Events)
	}
}

func TestProbeGraphwar2RoomRejectsBadAddress(t *testing.T) {
	res := ProbeGraphwar2Room(context.Background(), "", 0)
	if res.OK || res.Error == "" {
		t.Fatalf("expected bad address error, got %#v", res)
	}
}
