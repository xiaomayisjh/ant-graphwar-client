package backend

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Minimal WS client for tests: handshake, send masked text, read text frames.
// Splits the bridge's control messages (prefixed with \x00) from data lines.
type wsClient struct {
	conn  net.Conn
	mu    sync.Mutex
	lines []string // data lines (protocol)
	ctrl  []string // control messages (without the \x00 prefix)
}

func newWSClient(t *testing.T, port int) *wsClient {
	conn, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	kb := make([]byte, 16)
	rand.Read(kb)
	key := base64.StdEncoding.EncodeToString(kb)
	req := "GET / HTTP/1.1\r\nHost: 127.0.0.1\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\nSec-WebSocket-Version: 13\r\n\r\n"
	conn.Write([]byte(req))
	br := bufio.NewReader(conn)
	// read handshake response headers
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("ws handshake read: %v", err)
		}
		if strings.TrimRight(line, "\r\n") == "" {
			break
		}
	}
	c := &wsClient{conn: conn}
	go c.readLoop(br)
	return c
}

func (c *wsClient) readLoop(br *bufio.Reader) {
	for {
		text, opcode, err := readClientFrame(br)
		if err != nil {
			return
		}
		if opcode == 0x8 {
			return
		}
		if opcode == 0x1 || opcode == 0x0 {
			c.mu.Lock()
			if len(text) > 0 && text[0] == 0 {
				c.ctrl = append(c.ctrl, text[1:])
			} else {
				c.lines = append(c.lines, text)
			}
			c.mu.Unlock()
		}
	}
}

// server->client frames are unmasked
func readClientFrame(br *bufio.Reader) (string, byte, error) {
	h := make([]byte, 2)
	if _, err := io.ReadFull(br, h); err != nil {
		return "", 0, err
	}
	opcode := h[0] & 0x0f
	length := int(h[1] & 0x7f)
	if length == 126 {
		ext := make([]byte, 2)
		if _, err := io.ReadFull(br, ext); err != nil {
			return "", 0, err
		}
		length = int(binary.BigEndian.Uint16(ext))
	} else if length == 127 {
		ext := make([]byte, 8)
		if _, err := io.ReadFull(br, ext); err != nil {
			return "", 0, err
		}
		length = int(binary.BigEndian.Uint64(ext))
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(br, payload); err != nil {
		return "", 0, err
	}
	return string(payload), opcode, nil
}

func (c *wsClient) sendText(s string) {
	payload := []byte(s)
	n := len(payload)
	var header []byte
	mask := make([]byte, 4)
	rand.Read(mask)
	switch {
	case n < 126:
		header = []byte{0x81, byte(0x80 | n)}
	case n < 65536:
		header = []byte{0x81, byte(0x80 | 126), byte(n >> 8), byte(n)}
	default:
		header = make([]byte, 4)
		header[0] = 0x81
		header[1] = 0x80 | 127
	}
	masked := make([]byte, n)
	for i := 0; i < n; i++ {
		masked[i] = payload[i] ^ mask[i&3]
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.conn.Write(header)
	c.conn.Write(mask)
	c.conn.Write(masked)
}

func (c *wsClient) hasLine(prefix string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, l := range c.lines {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}
func (c *wsClient) hasCtrl(typ string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, m := range c.ctrl {
		if strings.Contains(m, `"type":"`+typ+`"`) {
			return true
		}
	}
	return false
}
func (c *wsClient) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string{}, c.lines...)
}
func (c *wsClient) close() { c.conn.Close() }

var _ = time.Second
