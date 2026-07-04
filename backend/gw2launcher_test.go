//go:build windows

package backend

import "testing"

func TestParseGraphwar2Netstat(t *testing.T) {
	pids := map[int]string{1234: "Graphwar.exe"}
	raw := `
  Proto  Local Address          Foreign Address        State           PID
  TCP    0.0.0.0:6112           0.0.0.0:0              LISTENING       9999
  TCP    [::]:61834             [::]:0                 LISTENING       1234
  TCP    127.0.0.1:61835        0.0.0.0:0              LISTENING       1234
  TCP    [::1]:61836            [::]:0                 ESTABLISHED     1234
`
	rooms := parseGraphwar2Netstat(raw, pids)
	if len(rooms) != 2 {
		t.Fatalf("expected 2 rooms, got %d: %#v", len(rooms), rooms)
	}
	if rooms[0].Host != "::1" || rooms[0].Port != 61834 || rooms[0].PID != 1234 {
		t.Fatalf("bad first room: %#v", rooms[0])
	}
	if rooms[1].Host != "127.0.0.1" || rooms[1].Port != 61835 {
		t.Fatalf("bad second room: %#v", rooms[1])
	}
}

func TestParseNetstatAddr(t *testing.T) {
	tests := []struct {
		in   string
		host string
		port int
	}{
		{"[::]:61834", "::", 61834},
		{"[::1]:61835", "::1", 61835},
		{"0.0.0.0:61836", "0.0.0.0", 61836},
		{"127.0.0.1:61837", "127.0.0.1", 61837},
	}
	for _, tt := range tests {
		host, port, ok := parseNetstatAddr(tt.in)
		if !ok || host != tt.host || port != tt.port {
			t.Fatalf("parseNetstatAddr(%q) = %q,%d,%v", tt.in, host, port, ok)
		}
	}
}
