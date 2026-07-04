package backend

import (
	"math"
	"math/rand"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

var lastPlayerID int

type splayer struct {
	name        string
	id          int
	team        int
	numSoldiers int
	ready       bool
	computer    bool
	level       int
}

func newSPlayer(name string) *splayer {
	name = sanitizeEncodedField(name, MaxNameLen)
	team := Team1
	if rand.Intn(2) == 0 {
		team = Team2
	}
	lastPlayerID++
	return &splayer{name: name, id: lastPlayerID - 1, team: team, numSoldiers: InitialNumSoldiers}
}

func newComputerPlayer(name string, level int) *splayer {
	p := newSPlayer(name)
	p.computer = true
	if level < 0 {
		level = 0
	}
	if level > MaxComputerLevel {
		level = MaxComputerLevel
	}
	p.level = level
	p.ready = true
	return p
}

type clientConn struct {
	server        *GraphServer
	conn          *LineConn
	players       []*splayer
	leader        bool
	readyNextTurn bool
	finished      bool
	skipLevel     bool
}

func (c *clientConn) send(s string) { c.conn.Send(s) }

func (c *clientConn) allowMessage() bool {
	return c.conn.allowMessage()
}

func sanitizeEncodedField(s string, maxDecoded int) string {
	if len(s) > MaxLineLen {
		s = s[:MaxLineLen]
	}
	decoded, err := url.QueryUnescape(strings.ReplaceAll(s, "+", " "))
	if err != nil {
		decoded = s
	}
	decoded = sanitizePlain(decoded, maxDecoded)
	return url.QueryEscape(decoded)
}

func sanitizePlain(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	rs := []rune(s)
	if len(rs) > max {
		rs = rs[:max]
	}
	return string(rs)
}

func safeEncodedLen(s string, maxDecoded int) bool {
	if len(s) > MaxLineLen {
		return false
	}
	decoded, err := url.QueryUnescape(strings.ReplaceAll(s, "+", " "))
	if err != nil {
		return false
	}
	return len([]rune(decoded)) <= maxDecoded
}

// GraphServer is one room relay.
type GraphServer struct {
	mu           sync.Mutex
	clients      []*clientConn
	players      []*splayer
	gameMode     int
	gameState    int
	countingDown bool
	startTimer   *time.Timer
	timeTurn     time.Time
	turnIndex    int
	circles      []int
	soldierPos   map[int][]pt
	accepting    bool
	port         int
	ln           interface{ Close() error }
	closed       chan struct{}
	hooks        Hooks
	// ---- management ----
	name        string          // room display name
	locked      bool            // when true, refuse new connections
	maxClients  int             // 0 => MaxClients default
	bannedIPs   map[string]bool // exact IP match
	bannedNames map[string]bool // case-insensitive name match
}

type Hooks struct {
	OnStatus func(mode, num int)
	OnStart  func()
	OnFinish func()
	OnEmpty  func()
}

func NewGraphServer(port int, hooks Hooks) *GraphServer {
	return &GraphServer{gameMode: NormalFunc, gameState: PreGame, turnIndex: -1, accepting: true, port: port, hooks: hooks,
		closed: make(chan struct{}), bannedIPs: map[string]bool{}, bannedNames: map[string]bool{}}
}

func (s *GraphServer) Listen() error {
	ln, actual, err := listenDual(s.port, func(lc *LineConn) { s.addClient(lc) }, "GraphServer")
	if err != nil {
		return err
	}
	s.ln = ln
	s.port = actual
	go s.sweep()
	return nil
}

// Port returns the actual bound port (meaningful after Listen).
func (s *GraphServer) Port() int { return s.port }

func (s *GraphServer) Close() {
	s.mu.Lock()
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}
	s.accepting = false
	if s.startTimer != nil {
		s.startTimer.Stop()
		s.startTimer = nil
	}
	ln := s.ln
	clients := append([]*clientConn{}, s.clients...)
	s.mu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
	for _, c := range clients {
		if c != nil && c.conn != nil {
			c.conn.Close()
		}
	}
}

func (s *GraphServer) sweep() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.closed:
			return
		case <-t.C:
			s.mu.Lock()
			now := time.Now()
			for _, c := range append([]*clientConn{}, s.clients...) {
				c.conn.mu.Lock()
				lr, ls := c.conn.lastReceived, c.conn.lastSent
				c.conn.mu.Unlock()
				if now.Sub(lr) > TimeoutDrop {
					c.conn.Close()
				} else if now.Sub(ls) > TimeoutKeepalive {
					c.send(itoa(NoInfo))
				}
			}
			if s.gameState == StateGame && !s.timeTurn.IsZero() && now.Sub(s.timeTurn) > TurnTime {
				s.nextTurnLocked()
			}
			s.mu.Unlock()
		}
	}
}

func (s *GraphServer) addClient(lc *LineConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	max := s.maxClients
	if max <= 0 {
		max = MaxClients
	}
	if len(s.clients) >= max || !s.accepting || s.locked || s.bannedIPs[lc.IP()] {
		lc.Send(itoa(GameFull))
		lc.Close()
		return
	}
	c := &clientConn{server: s, conn: lc}
	if len(s.clients) == 0 {
		c.leader = true
	}
	lc.onLine = func(line string) { s.handleMessage(line, c) }
	lc.onClose = func() { s.removeClient(c) }
	s.clients = append(s.clients, c)
	s.sendAllInfo(c)
	if c.leader {
		c.send(itoa(NewLeader))
	}
}

func (s *GraphServer) sendAll(msg string) {
	for _, c := range s.clients {
		c.send(msg)
	}
}

func (s *GraphServer) sendAllInfo(client *clientConn) {
	for _, p := range s.players {
		local := 0
		if owner, _ := s.findAnywhere(p.id); owner == client {
			local = 1
		}
		ready := 0
		if p.ready {
			ready = 1
		}
		client.send(join("&", AddPlayer, p.id, p.name, p.team, local, p.numSoldiers, ready))
	}
	client.send(itoa(SetMode) + "&" + itoa(s.gameMode))
}

func (s *GraphServer) status() {
	if s.hooks.OnStatus != nil {
		s.hooks.OnStatus(s.gameMode, len(s.players))
	}
}

func (s *GraphServer) findInClient(c *clientConn, id int) *splayer {
	for _, p := range c.players {
		if p.id == id {
			return p
		}
	}
	return nil
}
func (s *GraphServer) findAnywhere(id int) (*clientConn, *splayer) {
	for _, p := range s.players {
		if p.id == id && p.computer {
			return nil, p
		}
	}
	for _, c := range s.clients {
		for _, p := range c.players {
			if p.id == id {
				return c, p
			}
		}
	}
	return nil, nil
}

func (s *GraphServer) removePlayerFromAll(p *splayer) {
	for i, x := range s.players {
		if x == p {
			s.players = append(s.players[:i], s.players[i+1:]...)
			break
		}
	}
}

func (s *GraphServer) setEveryoneNotReady() {
	if s.startTimer != nil {
		s.startTimer.Stop()
		s.startTimer = nil
		s.countingDown = false
	}
	for _, c := range s.clients {
		for _, p := range c.players {
			if p.ready {
				p.ready = false
				s.sendAll(join("&", SetReady, p.id, 0))
			}
		}
	}
	for _, p := range s.players {
		if p.computer && !p.ready {
			p.ready = true
			s.sendAll(join("&", SetReady, p.id, 1))
		}
	}
}

func (s *GraphServer) checkAllReady() bool {
	any := false
	for _, p := range s.players {
		any = true
		if !p.ready {
			return false
		}
	}
	return any
}

func (s *GraphServer) removeClient(c *clientConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i, x := range s.clients {
		if x == c {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	s.clients = append(s.clients[:idx], s.clients[idx+1:]...)
	for _, p := range c.players {
		s.sendAll(join("&", RemovePlayer, p.id))
		s.removePlayerFromAll(p)
	}
	if len(s.clients) > 0 && c.leader {
		s.clients[0].leader = true
		s.clients[0].send(itoa(NewLeader))
	}
	s.checkNextTurnLocked()
	if len(s.clients) == 0 {
		if s.gameState == StateGame {
			s.goPreGameLocked()
		}
		if s.hooks.OnEmpty != nil {
			s.hooks.OnEmpty()
		}
	}
	s.status()
}

// ---- helpers for building messages ----
func join(sep string, parts ...interface{}) string {
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			b.WriteString(sep)
		}
		switch v := p.(type) {
		case int:
			b.WriteString(strconv.Itoa(v))
		case string:
			b.WriteString(v)
		case float64:
			b.WriteString(strconv.FormatFloat(v, 'f', -1, 64))
		}
	}
	return b.String()
}

func dist(x1, y1, x2, y2 float64) float64 {
	return math.Sqrt((x1-x2)*(x1-x2) + (y1-y2)*(y1-y2))
}
