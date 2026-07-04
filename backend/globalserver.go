package backend

import (
	"strconv"
	"strings"
	"sync"
	"time"
)

var lastLobbyPlayerID = 1
var lastRoomID = 1

type lobbyPlayer struct {
	conn    *LineConn
	server  *GlobalServer
	id      int
	name    string
	room    *lobbyRoom
	dummy   bool
	gotName bool
}

func (p *lobbyPlayer) send(s string) { p.conn.Send(s) }

type lobbyRoom struct {
	name       string
	ip         string
	port       int
	gameMode   int
	numPlayers int
	id         int
}

func (r *lobbyRoom) getIP(globalIP string) string {
	if strings.HasPrefix(r.ip, "127.0.0.1") {
		return globalIP
	}
	return r.ip
}

// GlobalServer is the lobby.
type GlobalServer struct {
	mu       sync.Mutex
	port     int
	publicIP string
	players  []*lobbyPlayer
	rooms    []*lobbyRoom
	ln       interface{ Close() error }
	closed   chan struct{}
}

func NewGlobalServer(port int, publicIP string) *GlobalServer {
	return &GlobalServer{port: port, publicIP: publicIP, closed: make(chan struct{})}
}

func (s *GlobalServer) Listen() error {
	ln, actual, err := listenDual(s.port, func(lc *LineConn) {
		p := &lobbyPlayer{conn: lc, server: s, id: lastLobbyPlayerID, name: "Player", dummy: true}
		lastLobbyPlayerID++
		lc.onLine = func(line string) { p.onLine(line) }
		lc.onClose = func() { s.removePlayer(p, true) }
		s.mu.Lock()
		s.players = append(s.players, p)
		s.mu.Unlock()
	}, "GlobalServer")
	if err != nil {
		return err
	}
	s.ln = ln
	s.port = actual
	go s.sweep()
	return nil
}

// Port returns the actual bound lobby port (meaningful after Listen).
func (s *GlobalServer) Port() int { return s.port }

func (s *GlobalServer) Close() {
	s.mu.Lock()
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}
	ln := s.ln
	players := append([]*lobbyPlayer{}, s.players...)
	s.mu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
	for _, p := range players {
		if p != nil && p.conn != nil {
			p.conn.Close()
		}
	}
}

func (s *GlobalServer) sweep() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.closed:
			return
		case <-t.C:
			s.mu.Lock()
			now := time.Now()
			for _, p := range append([]*lobbyPlayer{}, s.players...) {
				p.conn.mu.Lock()
				lr, ls := p.conn.lastReceived, p.conn.lastSent
				p.conn.mu.Unlock()
				if now.Sub(lr) > TimeoutDrop {
					p.conn.Close()
				} else if now.Sub(ls) > TimeoutKeepalive {
					p.send(itoa(NoInfo))
				}
			}
			s.mu.Unlock()
		}
	}
}

func (p *lobbyPlayer) onLine(line string) {
	if !p.gotName {
		p.name = sanitizeEncodedField(line, MaxNameLen)
		p.dummy = line == DummyName
		p.gotName = true
		p.server.mu.Lock()
		p.server.registerNewPlayer(p)
		p.server.sendListPlayers(p)
		p.server.sendListRooms(p)
		p.server.mu.Unlock()
		return
	}
	p.server.handleMessage(line, p)
}

func (s *GlobalServer) sendAll(msg string) {
	for _, p := range s.players {
		p.send(msg)
	}
}
func (s *GlobalServer) registerNewPlayer(p *lobbyPlayer) {
	if !p.dummy {
		s.sendAll(join("&", Join, p.name, p.id))
	}
}
func (s *GlobalServer) sendListPlayers(player *lobbyPlayer) {
	body := ""
	i := 0
	for _, p := range s.players {
		if !p.dummy {
			body += "&" + p.name + "&" + itoa(p.id)
			i++
		}
	}
	player.send(itoa(ListPlayers) + "&" + itoa(i) + body)
}
func (s *GlobalServer) sendListRooms(player *lobbyPlayer) {
	body := ""
	i := 0
	for _, r := range s.rooms {
		body += "&" + r.name + "&" + itoa(r.id) + "&" + r.getIP(s.publicIP) + "&" + itoa(r.port) + "&" + itoa(r.gameMode) + "&" + itoa(r.numPlayers)
		i++
	}
	player.send(itoa(ListRooms) + "&" + itoa(i) + body)
}
func (s *GlobalServer) sendNewRoom(r *lobbyRoom) {
	s.sendAll(join("&", CreateRoom, r.name, r.id, r.getIP(s.publicIP), r.port))
}
func (s *GlobalServer) updateRoom(r *lobbyRoom) {
	s.sendAll(join("&", RoomStatus, r.id, r.gameMode, r.numPlayers))
}

func (s *GlobalServer) removePlayer(player *lobbyPlayer, fromClose bool) {
	s.mu.Lock()
	idx := -1
	for i, p := range s.players {
		if p == player {
			idx = i
			break
		}
	}
	if idx < 0 {
		s.mu.Unlock()
		return
	}
	s.sendAll(join("&", Quit, player.id))
	s.players = append(s.players[:idx], s.players[idx+1:]...)
	if player.room != nil {
		s.removeRoomLocked(player.room)
	}
	s.mu.Unlock()
	if !fromClose {
		player.conn.onClose = nil
		player.conn.Close()
	}
}
func (s *GlobalServer) removeRoomLocked(room *lobbyRoom) {
	for i, r := range s.rooms {
		if r == room {
			s.sendAll(join("&", CloseRoom, room.id))
			s.rooms = append(s.rooms[:i], s.rooms[i+1:]...)
			return
		}
	}
}

// RegisterLocalRoom advertises a room hosted by this app's own room pool.
func (s *GlobalServer) RegisterLocalRoom(name, ip string, port int) *lobbyRoom {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := &lobbyRoom{name: name, ip: ip, port: port, id: lastRoomID}
	lastRoomID++
	s.rooms = append(s.rooms, r)
	s.sendNewRoom(r)
	return r
}
func (s *GlobalServer) UpdateLocalRoom(r *lobbyRoom, mode, num int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.gameMode = mode
	r.numPlayers = num
	s.updateRoom(r)
}
func (s *GlobalServer) RemoveLocalRoom(r *lobbyRoom) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeRoomLocked(r)
}

func (s *GlobalServer) handleMessage(message string, player *lobbyPlayer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !player.conn.allowMessage() || len(message) > MaxLineLen {
		return
	}
	info := strings.Split(message, "&")
	if len(info) == 0 || info[0] == "" {
		return
	}
	code, err := strconv.Atoi(info[0])
	if err != nil {
		return
	}
	switch code {
	case NoInfo:
		player.send(itoa(NoInfo))
	case SayChat:
		if len(info) == 2 {
			if !safeEncodedLen(info[1], MaxChatLen) {
				return
			}
			info[1] = sanitizeEncodedField(info[1], MaxChatLen)
			s.sendAll(join("&", SayChat, player.id, info[1]))
		}
	case RoomStatus:
		if len(info) == 3 && player.room != nil {
			player.room.gameMode, _ = strconv.Atoi(info[1])
			player.room.numPlayers, _ = strconv.Atoi(info[2])
			s.updateRoom(player.room)
		}
	case CreateRoom:
		if len(info) == 3 {
			port, _ := strconv.Atoi(info[2])
			if port <= 0 || port > 65535 {
				return
			}
			r := &lobbyRoom{name: sanitizeEncodedField(info[1], MaxRoomNameLen), ip: player.conn.IP(), port: port, id: lastRoomID}
			lastRoomID++
			s.rooms = append(s.rooms, r)
			player.room = r
			s.sendNewRoom(r)
		}
	case Quit:
		if len(info) == 1 {
			if player.room != nil {
				s.removeRoomLocked(player.room)
			}
			// removePlayer needs the lock; unlock then call.
			s.mu.Unlock()
			s.removePlayer(player, false)
			s.mu.Lock()
		}
	case CloseRoom:
		if len(info) == 1 && player.room != nil {
			s.removeRoomLocked(player.room)
		}
	}
}
