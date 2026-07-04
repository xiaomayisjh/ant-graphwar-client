// Package backend is an embedded Graphwar server (lobby + room relays) running
// as goroutines inside the Wails desktop app. Original Java clients connect
// over raw TCP and the WebView frontend connects over WebSocket — to the same
// ports — so the desktop app needs no external process to host online play.
//
// This is a faithful Go port of server/*.js (which itself ports the original
// Java GraphServer/GlobalServer). Trajectories are computed client-side, so
// this code only relays, generates maps, and drives turns.
package backend

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"math"
	"math/rand"
	"net"
	"strings"
	"sync"
	"time"
)

// Protocol message codes (NetworkProtocol.java).
const (
	NoInfo          = 10
	SetName         = 12
	ChatMsg         = 14
	AddPlayer       = 16
	AddSoldier      = 17
	RemoveSoldier   = 19
	SetTeam         = 20
	SetReady        = 21
	StartGame       = 22
	FireFunc        = 24
	NextTurn        = 25
	ReadyNextTurn   = 27
	SetAngle        = 28
	RemovePlayer    = 29
	NextMode        = 31
	SetMode         = 33
	TimeUp          = 37
	GameFull        = 38
	Disconnect      = 39
	GameFinished    = 40
	NewLeader       = 41
	StartCountdown  = 42
	Reorder         = 43
	FunctionPreview = 44

	Join        = 101
	SayChat     = 102
	ListPlayers = 103
	ListRooms   = 104
	RoomStatus  = 105
	Quit        = 106
	CloseRoom   = 107
	CreateRoom  = 108
	RoomInvalid = 109
)

// Game constants (GraphServer/Constants.java).
const (
	PlaneLength            = 770
	PlaneHeight            = 450
	SoldierRadius          = 7
	SoldierSelectionRadius = 15
	CircleMeanRadius       = 40
	CircleStdDev           = 25
	NumCirclesMean         = 15
	NumCirclesStdDev       = 7
	MaxPlayers             = 10
	MaxSoldiersPerPlayer   = 4
	MaxClients             = 10
	InitialNumSoldiers     = 2
	Team1                  = 1
	Team2                  = 2
	NormalFunc             = 0
	PreGame                = 1
	StateGame              = 2
	StartGameDelay         = 5000 * time.Millisecond
	TurnTime               = 60 * time.Second
	TimeoutKeepalive       = 5 * time.Second
	TimeoutDrop            = 30 * time.Second
	DummyName              = "23E(S_%24%40)!Xc"
	// MaxFuncLen bounds a fired/previewed function string we'll relay, so a
	// malicious current player can't broadcast a pathologically deep expression
	// that overflows other clients' parsers.
	MaxFuncLen = 4096

	MaxLineLen        = 8192
	MaxHandshakeBytes = 8192
	MaxWSFrameLen     = 8192
	MaxChatLen        = 600
	MaxNameLen        = 48
	MaxRoomNameLen    = 80
	MaxComputerLevel  = 9001
)

const wsMagic = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

func gaussian() float64 {
	u := rand.Float64()
	for u == 0 {
		u = rand.Float64()
	}
	v := rand.Float64()
	for v == 0 {
		v = rand.Float64()
	}
	return math.Sqrt(-2*math.Log(u)) * math.Cos(2*math.Pi*v)
}

// LineConn is a transport-agnostic newline-framed connection over either raw
// TCP or a WebSocket-upgraded socket.
type LineConn struct {
	conn         net.Conn
	isWS         bool
	mu           sync.Mutex
	closed       bool
	onLine       func(string)
	onClose      func()
	lastReceived time.Time
	lastSent     time.Time
	lastMsgAt    time.Time
	msgTokens    float64
}

func (c *LineConn) touchRecv() { c.mu.Lock(); c.lastReceived = time.Now(); c.mu.Unlock() }

func (c *LineConn) allowMessage() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if c.lastMsgAt.IsZero() {
		c.lastMsgAt = now
		c.msgTokens = 40
	}
	elapsed := now.Sub(c.lastMsgAt).Seconds()
	c.lastMsgAt = now
	c.msgTokens += elapsed * 12
	if c.msgTokens > 40 {
		c.msgTokens = 40
	}
	if c.msgTokens < 1 {
		return false
	}
	c.msgTokens--
	return true
}

// Send writes one protocol line.
func (c *LineConn) Send(line string) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.lastSent = time.Now()
	c.mu.Unlock()
	if c.isWS {
		_ = wsWriteText(c.conn, line)
	} else {
		_, _ = c.conn.Write([]byte(line + "\n"))
	}
}

func (c *LineConn) IP() string {
	host, _, err := net.SplitHostPort(c.conn.RemoteAddr().String())
	if err != nil {
		return "0.0.0.0"
	}
	return strings.TrimPrefix(host, "::ffff:")
}

func (c *LineConn) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.mu.Unlock()
	_ = c.conn.Close()
	if c.onClose != nil {
		c.onClose()
	}
}

// listenDual accepts BOTH raw TCP and WebSocket on one port; onConn is called
// (synchronously, then the connection's read loop runs in its own goroutine).
// Pass port 0 to let the OS assign a free port; the actual bound port is
// returned so callers can advertise it.
func listenDual(port int, onConn func(*LineConn), label string) (net.Listener, int, error) {
	ln, err := net.Listen("tcp", ":"+itoa(port))
	if err != nil {
		return nil, 0, err
	}
	actual := ln.Addr().(*net.TCPAddr).Port
	go func() {
		for {
			sock, err := ln.Accept()
			if err != nil {
				return
			}
			go handleSocket(sock, onConn)
		}
	}()
	return ln, actual, nil
}

func handleSocket(sock net.Conn, onConn func(*LineConn)) {
	defer func() {
		if recover() != nil {
			_ = sock.Close()
		}
	}()
	br := bufio.NewReader(sock)
	peek, _ := br.Peek(3)
	if string(peek) == "GET" {
		// WebSocket upgrade
		key := readHandshakeKey(br)
		if key == "" {
			sock.Close()
			return
		}
		accept := wsAccept(key)
		resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + accept + "\r\n\r\n"
		if _, err := sock.Write([]byte(resp)); err != nil {
			sock.Close()
			return
		}
		lc := &LineConn{conn: sock, isWS: true, lastReceived: time.Now(), lastSent: time.Now()}
		onConn(lc)
		wsReadLoop(br, lc)
	} else {
		lc := &LineConn{conn: sock, isWS: false, lastReceived: time.Now(), lastSent: time.Now()}
		onConn(lc)
		tcpReadLoop(br, lc)
	}
}

func tcpReadLoop(br *bufio.Reader, lc *LineConn) {
	for {
		line, err := readLimitedLine(br, MaxLineLen)
		if err != nil {
			lc.Close()
			return
		}
		lc.touchRecv()
		if lc.onLine != nil {
			lc.onLine(line)
		}
	}
}

func readHandshakeKey(br *bufio.Reader) string {
	key := ""
	total := 0
	for {
		line, err := readLimitedLine(br, MaxLineLen)
		if err != nil {
			return ""
		}
		total += len(line)
		if total > MaxHandshakeBytes {
			return ""
		}
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "sec-websocket-key:") {
			key = strings.TrimSpace(line[len("sec-websocket-key:"):])
		}
	}
	return key
}

func readLimitedLine(br *bufio.Reader, max int) (string, error) {
	var b strings.Builder
	b.Grow(min(max, 1024))
	for {
		part, prefix, err := br.ReadLine()
		if err != nil {
			return "", err
		}
		if b.Len()+len(part) > max {
			return "", errors.New("line too long")
		}
		b.Write(part)
		if !prefix {
			break
		}
	}
	return strings.TrimRight(b.String(), "\r"), nil
}

func wsAccept(key string) string {
	h := sha1.New()
	h.Write([]byte(key + wsMagic))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
