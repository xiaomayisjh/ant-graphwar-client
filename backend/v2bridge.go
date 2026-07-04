package backend

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

const (
	MaxV2PayloadLen = 65535
	MaxV2WSMsgLen   = 65535
)

var (
	v2DebugMu      sync.Mutex
	v2DebugSession uint64
	v2DebugEnabled atomic.Bool
)

// V2Bridge is a WebSocket<->TCP relay for Graphwar II. The frontend exchanges
// plain JSON event strings; this bridge adds/removes the game's u16be frame.
type V2Bridge struct {
	port int
	ln   net.Listener
}

func NewV2Bridge() *V2Bridge { return &V2Bridge{} }

func V2DebugLogPath() string {
	if p := os.Getenv("GW2_DEBUG_LOG"); p != "" {
		return p
	}
	if p := findProjectV2DebugLogPath(); p != "" {
		return p
	}
	return filepath.Join(os.TempDir(), "graphwar-desktop", "graphwar2-debug.log")
}

func findProjectV2DebugLogPath() string {
	roots := []string{}
	if cwd, err := os.Getwd(); err == nil {
		roots = append(roots, cwd)
	}
	if exe, err := os.Executable(); err == nil {
		roots = append(roots, filepath.Dir(exe))
	}
	for _, start := range roots {
		dir, err := filepath.Abs(start)
		if err != nil {
			continue
		}
		for i := 0; i < 8 && dir != "" && dir != filepath.Dir(dir); i++ {
			for _, candidate := range []string{
				filepath.Join(dir, "graphwar2"),
				filepath.Join(filepath.Dir(dir), "graphwar2"),
			} {
				if st, err := os.Stat(candidate); err == nil && st.IsDir() {
					return filepath.Join(candidate, "logs", "graphwar2-debug.log")
				}
			}
			dir = filepath.Dir(dir)
		}
	}
	return ""
}

func init() {
	v2DebugEnabled.Store(false)
}

func V2DebugEnabled() bool {
	return v2DebugEnabled.Load()
}

func SetV2DebugEnabled(enabled bool) {
	v2DebugEnabled.Store(enabled)
}

func ClearV2DebugLog() error {
	if err := os.MkdirAll(filepath.Dir(V2DebugLogPath()), 0755); err != nil {
		return err
	}
	return os.WriteFile(V2DebugLogPath(), nil, 0644)
}

func V2Debugf(format string, args ...interface{}) {
	if !V2DebugEnabled() {
		return
	}
	v2DebugMu.Lock()
	defer v2DebugMu.Unlock()
	path := V2DebugLogPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339Nano), fmt.Sprintf(format, args...))
}

func clipV2Debug(s string) string {
	const max = 8192
	if len(s) <= max {
		return s
	}
	return s[:max] + "...<truncated>"
}

func (b *V2Bridge) Start() (int, error) {
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

func (b *V2Bridge) Port() int { return b.port }

func (b *V2Bridge) handle(sock net.Conn) {
	session := atomic.AddUint64(&v2DebugSession, 1)
	V2Debugf("[bridge #%d] ws accepted from %s", session, sock.RemoteAddr())
	defer func() {
		if r := recover(); r != nil {
			V2Debugf("[bridge #%d] panic recovered: %v", session, r)
			_ = sock.Close()
		}
	}()
	br := bufio.NewReader(sock)
	peek, _ := br.Peek(3)
	if string(peek) != "GET" {
		V2Debugf("[bridge #%d] rejected non-websocket preface=%q", session, string(peek))
		_ = sock.Close()
		return
	}
	key := readHandshakeKey(br)
	if key == "" {
		V2Debugf("[bridge #%d] websocket handshake missing key", session)
		_ = sock.Close()
		return
	}
	resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + wsAccept(key) + "\r\n\r\n"
	if _, err := sock.Write([]byte(resp)); err != nil {
		V2Debugf("[bridge #%d] websocket handshake write failed: %v", session, err)
		_ = sock.Close()
		return
	}
	ws := &safeConn{c: sock}

	text, opcode, err := wsReadFrame(br)
	if err != nil || (opcode != 0x1 && opcode != 0x0) {
		V2Debugf("[bridge #%d] websocket config read failed op=%d err=%v", session, opcode, err)
		_ = sock.Close()
		return
	}
	var cfg struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	if len(text) > 512 || json.Unmarshal([]byte(text), &cfg) != nil || !safeBridgeHost(cfg.Host) || cfg.Port <= 0 || cfg.Port > 65535 {
		V2Debugf("[bridge #%d] bad config: %s", session, clipV2Debug(text))
		_ = ws.writeText("\x00{\"type\":\"error\",\"error\":\"bad_handshake\"}")
		_ = sock.Close()
		return
	}
	V2Debugf("[bridge #%d] dial tcp %s", session, net.JoinHostPort(normalizeDialHost(cfg.Host), strconv.Itoa(cfg.Port)))

	tcp, err := net.DialTimeout("tcp", net.JoinHostPort(normalizeDialHost(cfg.Host), strconv.Itoa(cfg.Port)), 10*time.Second)
	if err != nil {
		V2Debugf("[bridge #%d] tcp dial failed: %v", session, err)
		_ = ws.writeText("\x00{\"type\":\"tcp_error\",\"error\":\"" + jsonEscape(err.Error()) + "\"}")
		_ = sock.Close()
		return
	}
	V2Debugf("[bridge #%d] tcp connected", session)
	_ = ws.writeText("\x00{\"type\":\"connected\",\"host\":\"" + jsonEscape(cfg.Host) + "\",\"port\":" + strconv.Itoa(cfg.Port) + "}")

	var once sync.Once
	closeBoth := func() {
		once.Do(func() {
			_ = tcp.Close()
			_ = sock.Close()
		})
	}

	go func() {
		for {
			payload, err := readV2Frame(tcp)
			if err != nil {
				V2Debugf("[bridge #%d] S->C tcp read closed/error: %v", session, err)
				_ = ws.writeText("\x00{\"type\":\"tcp_closed\"}")
				closeBoth()
				return
			}
			V2Debugf("[bridge #%d] S->C %s", session, clipV2Debug(string(payload)))
			if ws.writeText(string(payload)) != nil {
				V2Debugf("[bridge #%d] websocket write to frontend failed", session)
				closeBoth()
				return
			}
		}
	}()

	for {
		t, op, err := wsReadFrame(br)
		if err != nil {
			V2Debugf("[bridge #%d] websocket read closed/error: %v", session, err)
			closeBoth()
			return
		}
		switch op {
		case 0x8:
			V2Debugf("[bridge #%d] websocket close frame", session)
			closeBoth()
			return
		case 0x9:
			ws.writePong()
		case 0x1, 0x0:
			if len(t) > MaxV2WSMsgLen || !utf8.ValidString(t) || !looksLikeJSONObject(t) {
				V2Debugf("[bridge #%d] bad frontend event: %s", session, clipV2Debug(t))
				_ = ws.writeText("\x00{\"type\":\"error\",\"error\":\"bad_v2_event\"}")
				closeBoth()
				return
			}
			V2Debugf("[bridge #%d] C->S %s", session, clipV2Debug(t))
			if err := writeV2Frame(tcp, []byte(t)); err != nil {
				V2Debugf("[bridge #%d] C->S tcp write failed: %v", session, err)
				closeBoth()
				return
			}
		}
	}
}

func normalizeDialHost(host string) string {
	host = strings.TrimSpace(host)
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		return strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	}
	return host
}

func looksLikeJSONObject(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}")
}

func readV2Frame(r io.Reader) ([]byte, error) {
	var header [2]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	n := int(binary.BigEndian.Uint16(header[:]))
	if n <= 0 || n > MaxV2PayloadLen {
		return nil, errors.New("bad graphwar2 frame length")
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	if !utf8.Valid(payload) {
		return nil, errors.New("graphwar2 payload is not utf8")
	}
	return payload, nil
}

func writeV2Frame(w io.Writer, payload []byte) error {
	if len(payload) == 0 || len(payload) > MaxV2PayloadLen {
		return errors.New("bad graphwar2 payload length")
	}
	var header [2]byte
	binary.BigEndian.PutUint16(header[:], uint16(len(payload)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}
