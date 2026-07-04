package backend

import (
	"bufio"
	"encoding/json"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Bridge is an embedded WebSocket<->TCP relay (Go port of bridge/bridge.js).
// The WebView frontend cannot open raw TCP, but the official Graphwar servers
// (GlobalServer / room GraphServers) speak raw TCP. The bridge accepts a WS
// connection from the local frontend and relays newline-framed protocol lines
// to/from a target TCP host:port, so the desktop app can join the OFFICIAL
// global lobby and play with worldwide players.
//
// It listens on 127.0.0.1 only (serves this app's own WebView), so dialing
// arbitrary hosts on the user's behalf carries no extra exposure.
type Bridge struct {
	port int
	ln   net.Listener
}

func NewBridge() *Bridge { return &Bridge{} }

// Start binds an OS-assigned local port and returns it.
func (b *Bridge) Start() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	b.ln = ln
	b.port = ln.Addr().(*net.TCPAddr).Port
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go b.handle(c)
		}
	}()
	return b.port, nil
}

func (b *Bridge) Port() int { return b.port }

// safeConn serializes writes from the two relay goroutines.
type safeConn struct {
	c  net.Conn
	mu sync.Mutex
}

func (s *safeConn) writeText(str string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return wsWriteText(s.c, str)
}
func (s *safeConn) writePong() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.c.Write([]byte{0x8a, 0x00})
}

func (b *Bridge) handle(sock net.Conn) {
	defer func() {
		if recover() != nil {
			_ = sock.Close()
		}
	}()
	br := bufio.NewReader(sock)
	peek, _ := br.Peek(3)
	if string(peek) != "GET" {
		sock.Close()
		return
	}
	key := readHandshakeKey(br)
	if key == "" {
		sock.Close()
		return
	}
	resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + wsAccept(key) + "\r\n\r\n"
	if _, err := sock.Write([]byte(resp)); err != nil {
		sock.Close()
		return
	}
	ws := &safeConn{c: sock}

	// First WS text frame = JSON {host, port}.
	text, opcode, err := wsReadFrame(br)
	if err != nil || (opcode != 0x1 && opcode != 0x0) {
		sock.Close()
		return
	}
	var cfg struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	if len(text) > 512 || json.Unmarshal([]byte(text), &cfg) != nil || !safeBridgeHost(cfg.Host) || cfg.Port <= 0 || cfg.Port > 65535 {
		ws.writeText("\x00{\"type\":\"error\",\"error\":\"bad_handshake\"}")
		sock.Close()
		return
	}

	// Dial the target TCP server (e.g. www.graphwar.com:23761 or a room).
	tcp, err := net.DialTimeout("tcp", cfg.Host+":"+strconv.Itoa(cfg.Port), 10*time.Second)
	if err != nil {
		ws.writeText("\x00{\"type\":\"tcp_error\",\"error\":\"" + jsonEscape(err.Error()) + "\"}")
		sock.Close()
		return
	}
	ws.writeText("\x00{\"type\":\"connected\",\"host\":\"" + jsonEscape(cfg.Host) + "\",\"port\":" + strconv.Itoa(cfg.Port) + "}")

	// TCP -> WS (one WS text message per protocol line).
	go func() {
		tbr := bufio.NewReader(tcp)
		for {
			line, err := readLimitedLine(tbr, MaxLineLen)
			if err != nil {
				ws.writeText("\x00{\"type\":\"tcp_closed\"}")
				sock.Close()
				return
			}
			if ws.writeText(line) != nil {
				tcp.Close()
				return
			}
		}
	}()

	// WS -> TCP.
	for {
		t, op, err := wsReadFrame(br)
		if err != nil {
			tcp.Close()
			return
		}
		switch op {
		case 0x8:
			tcp.Close()
			sock.Close()
			return
		case 0x9:
			ws.writePong()
		case 0x1, 0x0:
			if len(t) > MaxLineLen {
				tcp.Close()
				sock.Close()
				return
			}
			line := strings.TrimRight(t, "\r\n")
			if _, err := tcp.Write([]byte(line + "\n")); err != nil {
				sock.Close()
				return
			}
		}
	}
}

func safeBridgeHost(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	for _, r := range host {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '.' || r == '-' || r == ':' || r == '[' || r == ']' {
			continue
		}
		return false
	}
	return !strings.ContainsAny(host, "/\\\r\n\t ")
}

func jsonEscape(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return s
}
