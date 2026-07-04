package backend

import (
	"math"
	"math/rand"
	"strconv"
	"strings"
	"time"
)

func (s *GraphServer) generateCircles() []int {
	numCircles := int(gaussian()*NumCirclesStdDev + NumCirclesMean)
	if numCircles < 1 {
		numCircles = 1
	}
	circles := make([]int, 3*numCircles)
	for i := 0; i < numCircles; i++ {
		circles[3*i] = rand.Intn(PlaneLength)
		circles[3*i+1] = rand.Intn(PlaneHeight)
		r := int(gaussian()*CircleStdDev + CircleMeanRadius)
		for r < 0 {
			r = int(gaussian()*CircleStdDev + CircleMeanRadius)
		}
		circles[3*i+2] = r
	}
	return circles
}

type pt struct{ x, y int }

func testSoldier(sx, sy int, soldiers []pt, circles []int) bool {
	for _, sp := range soldiers {
		if abs(sx-sp.x) < 20 && abs(sy-sp.y) < 20 {
			return false
		}
	}
	n := len(circles) / 3
	for i := 0; i < n; i++ {
		if dist(float64(sx), float64(sy), float64(circles[3*i]), float64(circles[3*i+1])) < float64(circles[3*i+2]+SoldierSelectionRadius) {
			return false
		}
	}
	return true
}

func (s *GraphServer) generateSoldiers(circles []int) []int {
	var soldiers []pt
	for _, p := range s.players {
		for i := 0; i < p.numSoldiers; i++ {
			var x, y int
			for {
				x = rand.Intn(PlaneLength/2-2*SoldierRadius) + SoldierRadius
				y = rand.Intn(PlaneHeight-2*SoldierRadius) + SoldierRadius
				if p.team == Team2 {
					x += PlaneLength / 2
				}
				if testSoldier(x, y, soldiers, circles) {
					break
				}
			}
			soldiers = append(soldiers, pt{x, y})
		}
	}
	pos := make([]int, 0, len(soldiers)*2)
	for _, sp := range soldiers {
		pos = append(pos, sp.x, sp.y)
	}
	return pos
}

func (s *GraphServer) reorderPlayers() {
	var np []*splayer
	team := Team1
	if rand.Intn(2) == 0 {
		team = Team2
	}
	pool := append([]*splayer{}, s.players...)
	for len(pool) > 0 {
		found := false
		for i := 0; i < len(pool); i++ {
			if pool[i].team == team {
				np = append(np, pool[i])
				pool = append(pool[:i], pool[i+1:]...)
				i--
				if team == Team1 {
					team = Team2
				} else {
					team = Team1
				}
				found = true
			}
		}
		if !found {
			if team == Team1 {
				team = Team2
			} else {
				team = Team1
			}
		}
	}
	s.players = np
	msg := itoa(Reorder)
	for _, p := range np {
		msg += "&" + itoa(p.id)
	}
	s.sendAll(msg)
}

func (s *GraphServer) startGameLocked() {
	// Validate we can actually start BEFORE flipping any state (security fix:
	// a lone player who reduced soldiers to 0 must not brick the room).
	circles := s.generateCircles()
	numCircles := len(circles) / 3
	soldiers := s.generateSoldiers(circles)
	if len(soldiers) == 0 {
		// nothing to play with — stay in pre-game, keep accepting connections.
		s.setEveryoneNotReady()
		return
	}
	s.accepting = false
	s.reorderPlayers()
	s.circles = append([]int(nil), circles...)
	s.soldierPos = map[int][]pt{}
	posIdx := 0
	for _, p := range s.players {
		for i := 0; i < p.numSoldiers && posIdx+1 < len(soldiers); i++ {
			s.soldierPos[p.id] = append(s.soldierPos[p.id], pt{x: soldiers[posIdx], y: soldiers[posIdx+1]})
			posIdx += 2
		}
	}
	var b strings.Builder
	b.WriteString(itoa(StartGame) + "&" + itoa(numCircles))
	for _, c := range circles {
		b.WriteString("&" + itoa(c))
	}
	for _, so := range soldiers {
		b.WriteString("&" + itoa(so))
	}
	start := abs(rand.Int()) % len(s.players)
	for s.players[start].numSoldiers == 0 {
		start = abs(rand.Int()) % len(s.players)
	}
	s.turnIndex = start
	b.WriteString("&" + itoa(start))
	s.sendAll(b.String())
	s.timeTurn = time.Now()
	s.gameState = StateGame
	s.setEveryoneNotReady()
	if s.hooks.OnStart != nil {
		s.hooks.OnStart()
	}
	s.maybeScheduleComputerTurnLocked()
}

func (s *GraphServer) sendStartCountdownLocked() {
	if s.countingDown {
		return
	}
	s.countingDown = true
	s.sendAll(itoa(StartCountdown))
	s.startTimer = time.AfterFunc(StartGameDelay, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.startTimer = nil
		if s.checkAllReady() {
			s.startGameLocked()
		}
		s.countingDown = false
	})
}

func (s *GraphServer) checkNextTurnLocked() {
	for _, c := range s.clients {
		if !c.readyNextTurn {
			return
		}
	}
	if len(s.clients) == 0 {
		return
	}
	s.nextTurnLocked()
}
func (s *GraphServer) nextTurnLocked() {
	for _, c := range s.clients {
		c.readyNextTurn = false
	}
	s.advanceTurnIndexLocked()
	s.sendAll(itoa(NextTurn))
	s.timeTurn = time.Now()
	s.maybeScheduleComputerTurnLocked()
}

func (s *GraphServer) finishGameLocked(client *clientConn) {
	client.finished = true
	for _, c := range s.clients {
		if !c.finished {
			return
		}
	}
	for _, c := range s.clients {
		c.finished = false
		c.skipLevel = false
	}
	s.setEveryoneNotReady()
	s.sendAll(itoa(GameFinished))
	s.goPreGameLocked()
}
func (s *GraphServer) goPreGameLocked() {
	s.gameState = PreGame
	s.turnIndex = -1
	s.circles = nil
	s.soldierPos = nil
	s.accepting = true
	if s.hooks.OnFinish != nil {
		s.hooks.OnFinish()
	}
}

func (s *GraphServer) checkSkipLevelLocked() {
	for _, c := range s.clients {
		if !c.skipLevel {
			return
		}
	}
	for _, c := range s.clients {
		c.skipLevel = false
	}
	s.startGameLocked()
}

func (s *GraphServer) handleMessage(message string, client *clientConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !client.allowMessage() || len(message) > MaxLineLen {
		return
	}
	info := strings.Split(message, "&")
	if len(info) == 0 || info[0] == "" {
		return
	}
	typ, err := strconv.Atoi(info[0])
	if err != nil {
		return
	}
	switch typ {
	case NoInfo:
	case AddPlayer:
		if len(s.players) < MaxPlayers && len(info) >= 2 {
			info[1] = sanitizeEncodedField(info[1], MaxNameLen)
			// enforce name ban: if this name is banned, kick the whole client.
			if s.nameBanned(info[1]) {
				s.removeClientInline(client)
				client.conn.onClose = nil
				client.conn.Close()
				return
			}
			p := newSPlayer(info[1])
			client.players = append(client.players, p)
			s.players = append(s.players, p)
			s.setEveryoneNotReady()
			s.sendAddPlayer(p, client)
		}
	case SetTeam:
		if len(info) >= 3 {
			team, _ := strconv.Atoi(info[1])
			id, _ := strconv.Atoi(info[2])
			if s.setTeam(team, id, client) {
				s.setEveryoneNotReady()
				s.sendAll(message)
			}
		}
	case RemovePlayer:
		if len(info) >= 2 {
			id, _ := strconv.Atoi(info[1])
			if s.removePlayerByID(id, client) {
				s.setEveryoneNotReady()
				s.sendAll(message)
			}
		}
	case AddSoldier:
		if len(info) >= 2 {
			id, _ := strconv.Atoi(info[1])
			if s.adjSoldier(id, client, +1) {
				s.setEveryoneNotReady()
				s.sendAll(message)
			}
		}
	case RemoveSoldier:
		if len(info) >= 2 {
			id, _ := strconv.Atoi(info[1])
			if s.adjSoldier(id, client, -1) {
				s.setEveryoneNotReady()
				s.sendAll(message)
			}
		}
	case ChatMsg:
		if len(info) >= 3 {
			id, _ := strconv.Atoi(info[1])
			if s.findInClient(client, id) != nil {
				if !safeEncodedLen(info[2], MaxChatLen) {
					return
				}
				info[2] = sanitizeEncodedField(info[2], MaxChatLen)
				message = join("&", ChatMsg, id, info[2])
				s.handleCommands(info[2], client)
				s.sendAll(message)
			}
		}
	case NextMode:
		if client.leader {
			s.gameMode = (s.gameMode + 1) % 3
			s.setEveryoneNotReady()
			s.sendAll(itoa(SetMode) + "&" + itoa(s.gameMode))
			s.status()
		}
	case SetReady:
		if len(info) >= 3 {
			id, _ := strconv.Atoi(info[1])
			ready := info[2] != "0"
			if s.setReady(id, client, ready) {
				s.sendAll(join("&", SetReady, id, boolInt(ready)))
			}
			if s.checkAllReady() {
				s.sendStartCountdownLocked()
			}
		}
	case ReadyNextTurn:
		if s.gameState == StateGame {
			client.readyNextTurn = true
			s.checkNextTurnLocked()
		}
	case FireFunc:
		if len(info) >= 3 {
			id, _ := strconv.Atoi(info[1])
			// player must belong to this client (original checkPlayer), be in-game,
			// and the function string is length-bounded so a malicious client can't
			// broadcast a pathologically deep/long expression that crashes peers.
			if s.gameState == StateGame && s.findInClient(client, id) != nil && safeEncodedLen(info[2], MaxFuncLen) {
				s.sendAll(join("&", FireFunc, id, info[2]))
			}
		}
	case FunctionPreview:
		if len(info) >= 3 {
			id, _ := strconv.Atoi(info[1])
			if s.gameState == StateGame && s.findInClient(client, id) != nil && safeEncodedLen(info[2], MaxFuncLen) {
				s.sendAll(join("&", FunctionPreview, id, info[2]))
			}
		}
	case TimeUp:
		if s.gameState == StateGame && time.Since(s.timeTurn) > TurnTime {
			s.nextTurnLocked()
		}
	case GameFinished:
		// only meaningful during a game — ignore in pre-game (security fix:
		// prevents a pre-game GAME_FINISHED from forcing a state transition /
		// listener rebind).
		if s.gameState == StateGame {
			s.finishGameLocked(client)
		}
	case SetAngle:
		if len(info) >= 2 {
			id, _ := strconv.Atoi(info[1])
			if s.findInClient(client, id) != nil && s.gameState == StateGame {
				s.sendAll(message)
			}
		}
	case Disconnect:
		s.removeClientInline(client)
		client.conn.onClose = nil // already removed; avoid re-entrant lock
		client.conn.Close()
	}
}

func (s *GraphServer) handleCommands(msg string, client *clientConn) {
	msg = sanitizeEncodedField(msg, MaxChatLen)
	if strings.HasPrefix(msg, "-") && strings.EqualFold(msg, "-skip") && s.gameState == StateGame {
		client.skipLevel = true
		s.checkSkipLevelLocked()
	}
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (s *GraphServer) sendAddPlayer(p *splayer, from *clientConn) {
	for _, c := range s.clients {
		local := 0
		if c == from {
			local = 1
		}
		ready := 0
		if p.ready {
			ready = 1
		}
		c.send(join("&", AddPlayer, p.id, p.name, p.team, local, p.numSoldiers, ready))
	}
	s.status()
}

func (s *GraphServer) setTeam(team, id int, client *clientConn) bool {
	if p := s.findInClient(client, id); p != nil {
		p.team = team
		return true
	}
	if client.leader {
		if _, p := s.findAnywhere(id); p != nil {
			p.team = team
			return true
		}
	}
	return false
}
func (s *GraphServer) removePlayerByID(id int, client *clientConn) bool {
	remove := func(c *clientConn, p *splayer) {
		for i, x := range c.players {
			if x == p {
				c.players = append(c.players[:i], c.players[i+1:]...)
				break
			}
		}
		s.removePlayerFromAll(p)
	}
	if p := s.findInClient(client, id); p != nil {
		remove(client, p)
		s.status()
		return true
	}
	if client.leader {
		if c, p := s.findAnywhere(id); p != nil {
			if c != nil {
				remove(c, p)
			} else {
				s.removePlayerFromAll(p)
			}
			s.status()
			return true
		}
	}
	return false
}
func (s *GraphServer) adjSoldier(id int, client *clientConn, delta int) bool {
	adj := func(p *splayer) bool {
		n := p.numSoldiers + delta
		if n >= 0 && n <= MaxSoldiersPerPlayer {
			p.numSoldiers = n
			return true
		}
		return false
	}
	if p := s.findInClient(client, id); p != nil {
		return adj(p)
	}
	if client.leader {
		if _, p := s.findAnywhere(id); p != nil {
			return adj(p)
		}
	}
	return false
}
func (s *GraphServer) setReady(id int, client *clientConn, ready bool) bool {
	if p := s.findInClient(client, id); p != nil {
		if p.ready == ready {
			return false
		}
		p.ready = ready
		if !ready && s.startTimer != nil {
			s.startTimer.Stop()
			s.startTimer = nil
			s.countingDown = false
		}
		return true
	}
	return false
}

// removeClientInline removes a client when already holding the lock.
func (s *GraphServer) removeClientInline(c *clientConn) {
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
	if len(s.clients) == 0 && s.gameState == StateGame {
		s.goPreGameLocked()
	}
	s.status()
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

var _ = math.Pi
