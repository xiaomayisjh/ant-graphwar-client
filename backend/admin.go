package backend

import (
	"net/url"
	"strings"
)

// admin.go exposes room-management APIs for the embedded host.
// All methods lock the GraphServer mutex and are safe to call from the Wails App.

// decodeName reverts the URL-encoding clients use for player names.
func decodeName(enc string) string {
	s, err := url.QueryUnescape(strings.ReplaceAll(enc, "+", " "))
	if err != nil {
		return enc
	}
	return s
}

func (s *GraphServer) nameBanned(encName string) bool {
	if len(s.bannedNames) == 0 {
		return false
	}
	return s.bannedNames[strings.ToLower(decodeName(encName))]
}

// PlayerInfo is a snapshot of one connected player for the admin UI.
type PlayerInfo struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Team     int    `json:"team"`
	IP       string `json:"ip"`
	Leader   bool   `json:"leader"`
	Ready    bool   `json:"ready"`
	Soldiers int    `json:"soldiers"`
	Computer bool   `json:"computer"`
	Level    int    `json:"level"`
}

// ListPlayers returns every connected player with its IP (admin view).
func (s *GraphServer) ListPlayers() []PlayerInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []PlayerInfo
	for _, c := range s.clients {
		ip := c.conn.IP()
		for _, p := range c.players {
			out = append(out, PlayerInfo{ID: p.id, Name: p.name, Team: p.team, IP: ip,
				Leader: c.leader, Ready: p.ready, Soldiers: p.numSoldiers, Computer: p.computer, Level: p.level})
		}
	}
	for _, p := range s.players {
		if !p.computer {
			continue
		}
		out = append(out, PlayerInfo{ID: p.id, Name: p.name, Team: p.team, IP: "computer",
			Ready: p.ready, Soldiers: p.numSoldiers, Computer: true, Level: p.level})
	}
	return out
}

// findClientByPlayerID returns the client owning a given player id (lock held).
func (s *GraphServer) findClientByPlayerID(id int) *clientConn {
	for _, c := range s.clients {
		for _, p := range c.players {
			if p.id == id {
				return c
			}
		}
	}
	return nil
}

func (s *GraphServer) findComputerByPlayerID(id int) *splayer {
	for _, p := range s.players {
		if p.id == id && p.computer {
			return p
		}
	}
	return nil
}

// kickClient disconnects a client (lock held). Broadcasts player removals.
func (s *GraphServer) kickClient(c *clientConn) {
	if c == nil {
		return
	}
	s.removeClientInline(c)
	c.conn.onClose = nil
	c.conn.Close()
}

// KickPlayer disconnects the client owning the given player id.
func (s *GraphServer) KickPlayer(playerID int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.findComputerByPlayerID(playerID); p != nil {
		s.removePlayerFromAll(p)
		s.setEveryoneNotReady()
		s.sendAll(join("&", RemovePlayer, p.id))
		s.status()
		return true
	}
	c := s.findClientByPlayerID(playerID)
	if c == nil {
		return false
	}
	s.kickClient(c)
	return true
}

// BanPlayer bans a player's NAME (and optionally their IP) and kicks them.
func (s *GraphServer) BanPlayer(playerID int, alsoIP bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.findComputerByPlayerID(playerID); p != nil {
		s.bannedNames[strings.ToLower(decodeName(p.name))] = true
		s.removePlayerFromAll(p)
		s.setEveryoneNotReady()
		s.sendAll(join("&", RemovePlayer, p.id))
		s.status()
		return true
	}
	c := s.findClientByPlayerID(playerID)
	if c == nil {
		return false
	}
	for _, p := range c.players {
		if p.id == playerID {
			s.bannedNames[strings.ToLower(decodeName(p.name))] = true
		}
	}
	if alsoIP {
		s.bannedIPs[c.conn.IP()] = true
	}
	s.kickClient(c)
	return true
}

// BanName adds a name to the ban set (case-insensitive) and kicks any match.
func (s *GraphServer) BanName(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bannedNames[strings.ToLower(name)] = true
	for _, c := range append([]*clientConn{}, s.clients...) {
		for _, p := range c.players {
			if strings.EqualFold(decodeName(p.name), name) {
				s.kickClient(c)
				break
			}
		}
	}
}

// BanIP adds an IP to the ban set and kicks anyone currently on it.
func (s *GraphServer) BanIP(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bannedIPs[ip] = true
	for _, c := range append([]*clientConn{}, s.clients...) {
		if c.conn.IP() == ip {
			s.kickClient(c)
		}
	}
}

func (s *GraphServer) Unban(nameOrIP string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.bannedIPs, nameOrIP)
	delete(s.bannedNames, strings.ToLower(nameOrIP))
}

// Bans returns the current ban lists for the admin UI.
func (s *GraphServer) Bans() ([]string, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var names, ips []string
	for n := range s.bannedNames {
		names = append(names, n)
	}
	for ip := range s.bannedIPs {
		ips = append(ips, ip)
	}
	return names, ips
}

// SetLocked toggles whether new connections are accepted.
func (s *GraphServer) SetLocked(locked bool) {
	s.mu.Lock()
	s.locked = locked
	s.mu.Unlock()
}

// SetMaxClients caps how many connections the room accepts (0 = default).
func (s *GraphServer) SetMaxClients(n int) {
	s.mu.Lock()
	if n < 0 {
		n = 0
	}
	s.maxClients = n
	s.mu.Unlock()
}

// SetName changes the room's display name (re-registration handled by caller).
func (s *GraphServer) SetName(name string) {
	s.mu.Lock()
	s.name = name
	s.mu.Unlock()
}

// ForceReset ends any in-progress game and returns the room to pre-game.
func (s *GraphServer) ForceReset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gameState == StateGame {
		s.goPreGameLocked()
		s.setEveryoneNotReady()
		s.sendAll(itoa(GameFinished))
	}
}

// Status reports a snapshot for the admin UI.
type RoomStatusInfo struct {
	Name       string `json:"name"`
	Port       int    `json:"port"`
	Players    int    `json:"players"`
	Clients    int    `json:"clients"`
	GameMode   int    `json:"gameMode"`
	InGame     bool   `json:"inGame"`
	Locked     bool   `json:"locked"`
	MaxClients int    `json:"maxClients"`
}

func (s *GraphServer) Status() RoomStatusInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	max := s.maxClients
	if max <= 0 {
		max = MaxClients
	}
	return RoomStatusInfo{Name: s.name, Port: s.port, Players: len(s.players), Clients: len(s.clients),
		GameMode: s.gameMode, InGame: s.gameState == StateGame, Locked: s.locked, MaxClients: max}
}
