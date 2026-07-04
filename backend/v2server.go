package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var lastGraphwar2ServerID uint64

type Graphwar2Server struct {
	mu                 sync.Mutex
	ln                 net.Listener
	port               int
	name               string
	locked             bool
	axisMode           string
	function           string
	turnMode           string
	timeMode           string
	gameState          string
	conns              map[uint64]*v2ServerConn
	players            map[int]*v2ServerPlayer
	order              []int
	nextConnID         int
	nextPlayer         int
	nextSoldier        int
	turnIndex          int
	turnCount          int
	nextEffect         int
	tick               int
	obstacles          []v2Obstacle
	closed             bool
	botSubmitScheduled bool
	publisher          *Graphwar2OfficialPublisher
	publishHost        string
	publishLocalHost   string
}

type v2ServerConn struct {
	id     int
	sock   net.Conn
	server *Graphwar2Server
	leader bool
	player int
	key    uint64
}

type v2ServerPlayer struct {
	id        int
	connID    int
	name      string
	team      string
	soldiers  []int
	bot       bool
	botLevel  int
	lastFunc  string
	submitted bool
	skin      string
	face      string
	hat       string
	pos       map[int]v2Point
	alive     map[int]bool
}

type v2Point struct {
	x float64
	y float64
}

type v2Obstacle struct {
	x float64
	y float64
	r float64
}

var v2SpawnSafePoints = []v2Point{
	{x: 120, y: 90}, {x: 120, y: 132}, {x: 120, y: 174}, {x: 120, y: 216}, {x: 120, y: 258},
	{x: 650, y: 90}, {x: 650, y: 132}, {x: 650, y: 174}, {x: 650, y: 216}, {x: 650, y: 258},
}

var graphwar2HostedServers struct {
	sync.Mutex
	m map[int]*Graphwar2Server
}

func init() {
	graphwar2HostedServers.m = map[int]*Graphwar2Server{}
}

func StartGraphwar2CompatRoom(name string, port int) (*Graphwar2Server, error) {
	name = sanitizePlain(name, MaxRoomNameLen)
	if name == "" {
		name = "Graphwar II Room"
	}
	s := &Graphwar2Server{
		name:      name,
		port:      port,
		axisMode:  "EveryUnit",
		function:  "NormalFunction",
		turnMode:  "SimultaneousTurns",
		timeMode:  "Timer1m",
		gameState: "Setup",
		conns:     map[uint64]*v2ServerConn{},
		players:   map[int]*v2ServerPlayer{},
	}
	if err := s.Listen(); err != nil {
		return nil, err
	}
	graphwar2HostedServers.Lock()
	graphwar2HostedServers.m[s.port] = s
	graphwar2HostedServers.Unlock()
	return s, nil
}

func Graphwar2HostedServer(port int) *Graphwar2Server {
	graphwar2HostedServers.Lock()
	defer graphwar2HostedServers.Unlock()
	return graphwar2HostedServers.m[port]
}

func graphwar2CompatLocalRooms() []Graphwar2LocalRoom {
	graphwar2HostedServers.Lock()
	defer graphwar2HostedServers.Unlock()
	rooms := make([]Graphwar2LocalRoom, 0, len(graphwar2HostedServers.m))
	for port := range graphwar2HostedServers.m {
		rooms = append(rooms, graphwar2CompatLocalRoom(port))
	}
	return rooms
}

func graphwar2CompatLocalRoom(port int) Graphwar2LocalRoom {
	host := "::1"
	return Graphwar2LocalRoom{
		Host:       host,
		Port:       port,
		Address:    net.JoinHostPort(host, strconv.Itoa(port)),
		ListenHost: "::",
		Process:    "GraphwarDesktop",
	}
}

func (s *Graphwar2Server) Listen() error {
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(s.port))
	if err != nil {
		return err
	}
	s.ln = ln
	s.port = ln.Addr().(*net.TCPAddr).Port
	go s.acceptLoop()
	go s.tickLoop()
	return nil
}

func (s *Graphwar2Server) Port() int { return s.port }

func (s *Graphwar2Server) Close() {
	s.mu.Lock()
	s.closed = true
	conns := make([]*v2ServerConn, 0, len(s.conns))
	for _, c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()
	if s.ln != nil {
		_ = s.ln.Close()
	}
	for _, c := range conns {
		_ = c.sock.Close()
	}
	graphwar2HostedServers.Lock()
	delete(graphwar2HostedServers.m, s.port)
	graphwar2HostedServers.Unlock()
}

func (s *Graphwar2Server) acceptLoop() {
	for {
		sock, err := s.ln.Accept()
		if err != nil {
			return
		}
		key := atomic.AddUint64(&lastGraphwar2ServerID, 1)
		c := &v2ServerConn{sock: sock, server: s, key: key}
		s.mu.Lock()
		c.leader = len(s.conns) == 0
		s.conns[key] = c
		s.mu.Unlock()
		go s.handleConn(c)
	}
}

func (s *Graphwar2Server) tickLoop() {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for range t.C {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return
		}
		s.tick++
		tick := s.tick
		s.mu.Unlock()
		s.broadcast("Tick", map[string]interface{}{"current_tick": tick})
	}
}

func (s *Graphwar2Server) handleConn(c *v2ServerConn) {
	defer s.removeConn(c)
	for {
		payload, err := readV2Frame(c.sock)
		if err != nil {
			return
		}
		var ev map[string]json.RawMessage
		if json.Unmarshal(payload, &ev) != nil || len(ev) != 1 {
			continue
		}
		for variant, raw := range ev {
			s.handleEvent(c, variant, raw)
		}
	}
}

func (s *Graphwar2Server) handleEvent(c *v2ServerConn, variant string, raw json.RawMessage) {
	switch variant {
	case "NewConnectionRequest":
		s.mu.Lock()
		if c.id == 0 {
			s.nextConnID++
			c.id = s.nextConnID
		}
		id := c.id
		leader := c.leader
		s.mu.Unlock()
		c.send("ConnectedToServer", map[string]interface{}{})
		c.send("NewConnection", map[string]interface{}{"connection_id": id})
		if leader {
			c.send("RankInfo", map[string]interface{}{"entity_id": id, "rank": 1})
		}
		c.send("VersionInfo", map[string]interface{}{"version": "2.050", "system": "windows"})
		s.sendSnapshot(c)
	case "TickRequest":
		s.mu.Lock()
		tick := s.tick
		s.mu.Unlock()
		c.send("TickReply", map[string]interface{}{"current_tick": tick})
	case "NewPlayerRequest":
		var f struct {
			ConnectionID int `json:"connection_id"`
		}
		_ = json.Unmarshal(raw, &f)
		if f.ConnectionID == 0 || f.ConnectionID == c.id {
			s.addPlayer(c, false, 0)
		}
	case "NewBotRequest":
		var f struct {
			ConnectionID int `json:"connection_id"`
			Level        int `json:"level"`
		}
		_ = json.Unmarshal(raw, &f)
		if c.leader && (f.ConnectionID == 0 || f.ConnectionID == c.id) {
			if f.Level < 0 {
				f.Level = 0
			}
			if f.Level > 5 {
				f.Level = 5
			}
			s.addPlayer(c, true, f.Level)
		}
	case "NameRequest":
		fields := parseV2Fields(raw)
		entityID := firstV2ID(fields, "entity_id", "player_id", "owner_id")
		entityID = s.resolvePlayerRef(c, entityID)
		name := sanitizePlain(stringV2Field(fields, "name", "player_name"), MaxNameLen)
		s.mu.Lock()
		if p := s.players[entityID]; p != nil && (p.connID == c.id || c.leader) {
			p.name = name
			s.mu.Unlock()
			s.broadcast("NameInfo", map[string]interface{}{"entity_id": entityID, "name": name})
			return
		}
		s.mu.Unlock()
	case "RankRequest":
		fields := parseV2Fields(raw)
		entityID := firstV2ID(fields, "entity_id", "player_id", "connection_id")
		if entityID == 0 {
			entityID = c.player
		}
		if entityID == 0 {
			entityID = c.id
		}
		rank := 0
		if c.leader || (c.player != 0 && entityID == c.player) || entityID == c.id {
			if c.leader {
				rank = 1
			}
			c.send("RankInfo", map[string]interface{}{"entity_id": entityID, "rank": rank})
		}
	case "GameNameRequest":
		var f struct {
			GameName string `json:"game_name"`
		}
		_ = json.Unmarshal(raw, &f)
		if c.leader {
			name := sanitizePlain(f.GameName, MaxRoomNameLen)
			if name == "" {
				name = s.name
			}
			s.mu.Lock()
			s.name = name
			s.mu.Unlock()
			s.broadcast("GameNameInfo", map[string]interface{}{"game_name": name})
			s.broadcast("GameInfo", s.gameInfoFields())
			s.updatePublisher()
		}
	case "ChangeTeamRequest":
		fields := parseV2Fields(raw)
		s.changeTeam(c, firstV2ID(fields, "player_id", "entity_id", "owner_id"))
	case "AddSoldierRequest":
		fields := parseV2Fields(raw)
		pid := firstV2ID(fields, "player_id", "entity_id", "owner_id", "connection_id")
		if pid == 0 {
			pid = s.playerForSoldier(firstV2ID(fields, "soldier_id"))
		}
		s.adjustSoldiers(c, pid, +1)
	case "RemoveSoldierRequest":
		fields := parseV2Fields(raw)
		pid := firstV2ID(fields, "player_id", "entity_id", "owner_id", "connection_id")
		if pid == 0 {
			pid = s.playerForSoldier(firstV2ID(fields, "soldier_id"))
		}
		s.adjustSoldiers(c, pid, -1)
	case "RemovePlayerRequest":
		fields := parseV2Fields(raw)
		s.removePlayerRequest(c, firstV2ID(fields, "player_id", "entity_id", "owner_id"))
	case "AxisModeChangeRequest":
		if c.leader {
			s.cycleMode("axis")
		}
	case "FunctionModeChangeRequest":
		if c.leader {
			s.cycleMode("function")
		}
	case "TurnModeChangeRequest":
		if c.leader {
			s.cycleMode("turn")
		}
	case "TimeModeChangeRequest":
		if c.leader {
			s.cycleMode("time")
		}
	case "LockRequest":
		var f struct {
			Lock bool `json:"lock"`
		}
		_ = json.Unmarshal(raw, &f)
		if c.leader {
			s.mu.Lock()
			s.locked = f.Lock
			s.mu.Unlock()
			s.broadcast("LockInfo", map[string]interface{}{"lock": f.Lock})
			s.broadcast("GameInfo", s.gameInfoFields())
		}
	case "ChatMessageRequest":
		fields := parseV2Fields(raw)
		entityIDIn := firstV2ID(fields, "entity_id", "player_id", "owner_id", "connection_id")
		if entityIDIn == c.id || (c.player != 0 && entityIDIn == c.player) {
			msg := sanitizePlain(stringV2Field(fields, "message", "chat_message", "text"), MaxChatLen)
			s.broadcast("ChatMessage", map[string]interface{}{"entity_id": c.id, "connection_id": c.id, "player_id": c.player, "message": msg})
		}
	case "SkinGraphicsRequest":
		s.handleGraphicsRequest(c, raw, "skin")
	case "FaceGraphicsRequest":
		s.handleGraphicsRequest(c, raw, "face")
	case "HatGraphicsRequest":
		s.handleGraphicsRequest(c, raw, "hat")
	case "FunctionUpdateRequest":
		fields := parseV2Fields(raw)
		playerID := firstV2ID(fields, "player_id", "owner_id", "entity_id")
		playerID = s.resolvePlayerRef(c, playerID)
		if playerID == 0 {
			playerID = s.playerForSoldier(firstV2ID(fields, "soldier_id"))
		}
		if s.ownsPlayer(c, playerID) {
			fn := sanitizePlain(stringV2Field(fields, "function", "corrected_function", "function_str"), MaxFuncLen)
			s.mu.Lock()
			if p := s.players[playerID]; p != nil {
				p.lastFunc = fn
			}
			s.mu.Unlock()
			s.broadcast("FunctionInfo", map[string]interface{}{"owner_id": playerID, "player_id": playerID, "function": fn, "corrected_function": fn})
		}
	case "FunctionFireRequest":
		fields := parseV2Fields(raw)
		playerID := firstV2ID(fields, "player_id", "owner_id", "entity_id")
		playerID = s.resolvePlayerRef(c, playerID)
		if playerID == 0 {
			playerID = s.playerForSoldier(firstV2ID(fields, "soldier_id"))
		}
		if s.ownsPlayer(c, playerID) {
			s.mu.Lock()
			if p := s.players[playerID]; p != nil {
				p.submitted = true
			}
			soldierID := s.currentSoldierForPlayerLocked(playerID)
			ready := s.readyToResolveLocked()
			s.mu.Unlock()
			if soldierID == 0 {
				soldierID = playerID
			}
			s.broadcast("FunctionFire", map[string]interface{}{"soldier_id": soldierID})
			if ready {
				s.resolveSubmittedFunctions()
			}
		}
	case "FunctionsCalculated":
		// Official clients may echo this after local animation/calculation. The
		// authoritative room server resolves turns when FunctionFireRequest is
		// complete, so this is accepted as a harmless synchronization marker.
	case "GameStartRequest":
		if c.leader {
			s.startGame()
		}
	case "ConnectionRemoved":
		fields := parseV2Fields(raw)
		connectionID := firstV2ID(fields, "connection_id", "entity_id")
		if connectionID == 0 || connectionID == c.id || c.leader {
			_ = c.sock.Close()
		}
	}
}

func (s *Graphwar2Server) sendSnapshot(c *v2ServerConn) {
	c.send("GameInfo", s.gameInfoFields())
	c.send("GameNameInfo", map[string]interface{}{"game_name": s.name})
	c.send("GameStateInfo", map[string]interface{}{"game_state": s.gameState})
	c.send("AxisModeInfo", map[string]interface{}{"axis_mode": s.axisMode})
	c.send("FunctionModeInfo", map[string]interface{}{"function_mode": s.function})
	c.send("TurnModeInfo", map[string]interface{}{"turn_mode": s.turnMode})
	c.send("TimeModeInfo", map[string]interface{}{"time_mode": s.timeMode})
	c.send("LockInfo", map[string]interface{}{"lock": s.locked})
	s.mu.Lock()
	ids := append([]int{}, s.order...)
	players := make([]*v2ServerPlayer, 0, len(ids))
	for _, id := range ids {
		if p := s.players[id]; p != nil {
			cp := *p
			cp.soldiers = append([]int{}, p.soldiers...)
			players = append(players, &cp)
		}
	}
	s.mu.Unlock()
	for _, p := range players {
		c.sendPlayerInfo(p)
		for _, sid := range p.soldiers {
			c.send("SoldierInfo", map[string]interface{}{"soldier_id": sid, "player_id": p.id})
		}
	}
}

func (s *Graphwar2Server) addPlayer(c *v2ServerConn, bot bool, level int) {
	s.mu.Lock()
	if !bot && c.player != 0 {
		s.mu.Unlock()
		return
	}
	if len(s.players) >= MaxPlayers {
		s.mu.Unlock()
		return
	}
	s.nextPlayer++
	pid := s.nextPlayer
	team := "Team1"
	if len(s.players)%2 == 1 {
		team = "Team2"
	}
	name := fmt.Sprintf("Player %d", pid)
	connID := c.id
	if bot {
		connID = -pid
		name = fmt.Sprintf("AI Bot %d", level)
	}
	p := &v2ServerPlayer{id: pid, connID: connID, name: name, team: team, bot: bot, botLevel: level}
	for i := 0; i < InitialNumSoldiers; i++ {
		s.nextSoldier++
		p.soldiers = append(p.soldiers, s.nextSoldier)
	}
	s.players[pid] = p
	s.order = append(s.order, pid)
	if !bot {
		c.player = pid
	}
	s.mu.Unlock()

	s.broadcastPlayerInfo(p)
	if bot {
		s.broadcast("BotAdded", map[string]interface{}{"connection_id": connID, "player_id": pid, "level": level})
	}
	for _, sid := range p.soldiers {
		s.broadcast("SoldierInfo", map[string]interface{}{"soldier_id": sid, "player_id": pid})
	}
	s.broadcast("NameInfo", map[string]interface{}{"entity_id": pid, "name": name})
	s.broadcast("TeamInfo", map[string]interface{}{"entity_id": pid, "team": team})
	s.broadcast("GameInfo", s.gameInfoFields())
	s.updatePublisher()
}

func (s *Graphwar2Server) changeTeam(c *v2ServerConn, pid int) {
	s.mu.Lock()
	pid = s.resolvePlayerRefLocked(c, pid)
	p := s.players[pid]
	if p == nil || (p.connID != c.id && !c.leader) {
		s.mu.Unlock()
		return
	}
	if p.team == "Team1" {
		p.team = "Team2"
	} else {
		p.team = "Team1"
	}
	team := p.team
	s.mu.Unlock()
	s.broadcast("TeamInfo", map[string]interface{}{"entity_id": pid, "team": team})
	s.broadcast("ColorInfo", map[string]interface{}{"entity_id": pid, "color": v2ColorForPlayer(pid, team)})
	s.updatePublisher()
}

func (s *Graphwar2Server) adjustSoldiers(c *v2ServerConn, pid, delta int) {
	var added, removed int
	s.mu.Lock()
	pid = s.resolvePlayerRefLocked(c, pid)
	p := s.players[pid]
	if p == nil || (p.connID != c.id && !c.leader) {
		s.mu.Unlock()
		return
	}
	if delta > 0 && len(p.soldiers) < 5 {
		s.nextSoldier++
		added = s.nextSoldier
		p.soldiers = append(p.soldiers, added)
	} else if delta < 0 && len(p.soldiers) > 0 {
		removed = p.soldiers[len(p.soldiers)-1]
		p.soldiers = p.soldiers[:len(p.soldiers)-1]
	}
	s.mu.Unlock()
	if added != 0 {
		s.broadcast("SoldierInfo", map[string]interface{}{"soldier_id": added, "player_id": pid})
	}
	if removed != 0 {
		s.broadcast("EntityRemoved", map[string]interface{}{"entity_id": removed})
	}
	if added != 0 || removed != 0 {
		s.updatePublisher()
	}
}

func (s *Graphwar2Server) removePlayerRequest(c *v2ServerConn, pid int) {
	var removedSoldiers []int
	s.mu.Lock()
	pid = s.resolvePlayerRefLocked(c, pid)
	p := s.players[pid]
	if p == nil || (p.connID != c.id && !c.leader) {
		s.mu.Unlock()
		return
	}
	removedSoldiers = append(removedSoldiers, p.soldiers...)
	delete(s.players, pid)
	for i, id := range s.order {
		if id == pid {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	for _, cc := range s.conns {
		if cc.player == pid {
			cc.player = 0
		}
	}
	s.mu.Unlock()
	s.broadcastRemovedEntities(removedSoldiers, pid)
	s.broadcast("GameInfo", s.gameInfoFields())
	s.updatePublisher()
}

func (s *Graphwar2Server) broadcastRemovedEntities(soldierIDs []int, playerID int) {
	for _, sid := range soldierIDs {
		s.broadcast("EntityRemoved", map[string]interface{}{"entity_id": sid})
	}
	s.broadcast("EntityRemoved", map[string]interface{}{"entity_id": playerID})
}

func (s *Graphwar2Server) cycleMode(kind string) {
	s.mu.Lock()
	switch kind {
	case "axis":
		s.axisMode = nextString(s.axisMode, []string{"NoAxis", "OnlyMain", "EveryFive", "EveryUnit"})
	case "function":
		s.function = nextString(s.function, []string{"NormalFunction", "DiffEqFunction"})
	case "turn":
		s.turnMode = nextString(s.turnMode, []string{"SequentialTurns", "SimultaneousTurns"})
	case "time":
		s.timeMode = nextString(s.timeMode, []string{"Timer30s", "Timer1m", "Timer2m", "Timer3m", "Timer5m", "TimerInf"})
	}
	axis, fn, turn, tm := s.axisMode, s.function, s.turnMode, s.timeMode
	s.mu.Unlock()
	s.broadcast("AxisModeInfo", map[string]interface{}{"axis_mode": axis})
	s.broadcast("FunctionModeInfo", map[string]interface{}{"function_mode": fn})
	s.broadcast("TurnModeInfo", map[string]interface{}{"turn_mode": turn})
	s.broadcast("TimeModeInfo", map[string]interface{}{"time_mode": tm})
	s.broadcast("GameInfo", s.gameInfoFields())
	s.updatePublisher()
}

func (s *Graphwar2Server) startGame() {
	s.mu.Lock()
	if len(s.players) == 0 {
		s.mu.Unlock()
		return
	}
	s.gameState = "WaitingForFunctions"
	s.turnIndex = 0
	s.turnCount = 1
	for _, p := range s.players {
		p.submitted = false
		if p.alive == nil {
			p.alive = map[int]bool{}
		}
		for _, sid := range p.soldiers {
			p.alive[sid] = true
		}
	}
	ids := append([]int{}, s.order...)
	turnCount := s.turnCount
	s.obstacles = generateV2Obstacles(time.Now().UnixNano() ^ int64(s.port) ^ int64(s.tick))
	obstacles := append([]v2Obstacle{}, s.obstacles...)
	s.mu.Unlock()
	s.broadcast("ClearObstacles", map[string]interface{}{})
	for _, ob := range obstacles {
		s.broadcast("AddObstacle", map[string]interface{}{"pos_x": pxToGameX(ob.x), "pos_y": pyToGameY(ob.y), "radius": radiusToGame(ob.r)})
	}
	s.assignStartPositions()
	s.broadcastSoldierState()
	s.broadcast("GameStateInfo", map[string]interface{}{"game_state": "WaitingForFunctions"})
	s.broadcast("GameInfo", s.gameInfoFields())
	s.broadcast("TurnLimitInfo", map[string]interface{}{"turn_limit": timeModeSeconds(s.timeMode)})
	s.broadcast("TurnCountInfo", map[string]interface{}{"turn_count": turnCount})
	for i, pid := range ids {
		s.broadcast("TurnInfo", map[string]interface{}{"player_id": pid, "soldier_id": s.firstSoldier(pid)})
		if i > 0 && s.turnMode == "SequentialTurns" {
			break
		}
	}
	s.updatePublisher()
	s.scheduleBotTurns()
}

func (s *Graphwar2Server) resolveSubmittedFunctions() {
	s.mu.Lock()
	shots := s.submittedShotsLocked()
	currentTick := s.tick
	if currentTick == 0 {
		currentTick = 1
	}
	endTick := currentTick + 20
	results := make([]v2ShotResult, 0, len(shots))
	for _, shot := range shots {
		result := s.resolveShotLocked(shot, currentTick)
		if result.targetSoldier != 0 {
			if target := s.players[result.targetPlayer]; target != nil {
				if target.alive == nil {
					target.alive = map[int]bool{}
				}
				target.alive[result.targetSoldier] = false
			}
		}
		results = append(results, result)
	}
	for _, p := range s.players {
		p.submitted = false
	}
	s.advanceTurnLocked()
	nextTurns := s.currentTurnInfosLocked()
	turnCount := s.turnCount
	gameOver := s.activeTeamCountLocked() <= 1
	if gameOver {
		s.gameState = "GameOver"
	} else {
		s.gameState = "WaitingForFunctions"
	}
	gameState := s.gameState
	s.mu.Unlock()
	for _, result := range results {
		s.broadcast("FunctionActive", map[string]interface{}{"owner_id": result.playerID, "active_tick": result.activeTick})
		s.broadcast("FunctionPoints", map[string]interface{}{"owner_id": result.playerID, "start_x": pxToGameX(result.start.x), "start_y": pyToGameY(result.start.y), "end_x": pxToGameX(result.end.x)})
		s.broadcast("EffectInfo", map[string]interface{}{"effect_id": result.effectID, "effect_type": "Explosion", "start_tick": result.activeTick, "pos_x": pxToGameX(result.end.x), "pos_y": pyToGameY(result.end.y)})
		if result.obstacleHit {
			fields := map[string]interface{}{"pos_x": pxToGameX(result.obstaclePoint.x), "pos_y": pyToGameY(result.obstaclePoint.y), "radius": radiusToGame(result.obstacleRad), "tick": result.activeTick + 8}
			s.broadcast("QueueDestroyObstacle", fields)
			s.broadcast("DestroyObstacle", fields)
		}
		if result.targetSoldier != 0 {
			s.broadcast("EntityEffectInfo", map[string]interface{}{"entity_id": result.targetSoldier, "effect_type": "Explosion"})
			s.broadcast("LifeQueuedDeath", map[string]interface{}{"entity_id": result.targetSoldier, "tick": result.activeTick + 8})
			s.broadcast("LifeInfo", map[string]interface{}{"entity_id": result.targetSoldier, "alive": false})
		}
	}
	s.broadcast("FunctionsCalculated", map[string]interface{}{})
	s.broadcast("EndOfTurnInfo", map[string]interface{}{"end_of_turn_tick": endTick, "time_mode": s.timeMode})
	s.broadcast("TurnCountInfo", map[string]interface{}{"turn_count": turnCount})
	s.broadcast("GameStateInfo", map[string]interface{}{"game_state": gameState})
	if !gameOver {
		for _, turn := range nextTurns {
			s.broadcast("TurnInfo", map[string]interface{}{"player_id": turn.playerID, "soldier_id": turn.soldierID})
		}
	}
	s.updatePublisher()
	if !gameOver {
		s.scheduleBotTurns()
	}
}

type v2TurnInfo struct {
	playerID  int
	soldierID int
}

type v2Shot struct {
	playerID  int
	soldierID int
	start     v2Point
	function  string
}

type v2ShotResult struct {
	playerID      int
	soldierID     int
	targetPlayer  int
	targetSoldier int
	start         v2Point
	end           v2Point
	activeTick    int
	effectID      int
	obstacleHit   bool
	obstaclePoint v2Point
	obstacleRad   float64
}

func (s *Graphwar2Server) readyToResolveLocked() bool {
	if s.gameState == "Setup" {
		return false
	}
	turns := s.currentTurnInfosLocked()
	if len(turns) == 0 {
		return false
	}
	for _, turn := range turns {
		p := s.players[turn.playerID]
		if p == nil || !p.submitted {
			return false
		}
	}
	return true
}

func (s *Graphwar2Server) scheduleBotTurns() {
	s.mu.Lock()
	if s.closed || s.botSubmitScheduled || s.gameState == "Setup" {
		s.mu.Unlock()
		return
	}
	s.botSubmitScheduled = true
	s.mu.Unlock()
	time.AfterFunc(120*time.Millisecond, func() {
		s.submitBotTurnsNow()
	})
}

func (s *Graphwar2Server) submitBotTurnsNow() {
	type botShot struct {
		playerID  int
		soldierID int
		function  string
	}
	var shots []botShot
	ready := false
	s.mu.Lock()
	s.botSubmitScheduled = false
	if !s.closed && s.gameState != "Setup" {
		for _, turn := range s.currentTurnInfosLocked() {
			p := s.players[turn.playerID]
			if p == nil || !p.bot || p.submitted || turn.soldierID == 0 {
				continue
			}
			fn := s.botFunctionLocked(p, turn.soldierID)
			p.lastFunc = fn
			p.submitted = true
			shots = append(shots, botShot{playerID: turn.playerID, soldierID: turn.soldierID, function: fn})
		}
		ready = len(shots) > 0 && s.readyToResolveLocked()
	}
	s.mu.Unlock()
	for _, shot := range shots {
		s.broadcast("FunctionInfo", map[string]interface{}{"owner_id": shot.playerID, "player_id": shot.playerID, "function": shot.function, "corrected_function": shot.function})
		s.broadcast("FunctionFire", map[string]interface{}{"soldier_id": shot.soldierID})
	}
	if ready {
		s.resolveSubmittedFunctions()
	}
}

func (s *Graphwar2Server) botFunctionLocked(p *v2ServerPlayer, soldierID int) string {
	if p == nil {
		return "0"
	}
	start := p.pos[soldierID]
	if start == (v2Point{}) {
		start = s.defaultSoldierPosLocked(p.id, soldierID)
	}
	_, _, target := s.nearestEnemyLocked(p.id, start)
	candidates := s.botFunctionCandidatesLocked(p, start, target)
	bestFunc := "0"
	bestScore := math.Inf(-1)
	for _, fn := range candidates {
		result, ok := s.sampleFunctionShotLocked(v2Shot{playerID: p.id, soldierID: soldierID, start: start, function: fn})
		score := s.scoreBotShotLocked(p, result, ok, target)
		if score > bestScore {
			bestScore = score
			bestFunc = fn
		}
		if ok && result.targetPlayer != 0 {
			if targetPlayer := s.players[result.targetPlayer]; targetPlayer != nil && targetPlayer.team != p.team {
				return fn
			}
		}
	}
	return bestFunc
}

func (s *Graphwar2Server) botFunctionCandidatesLocked(p *v2ServerPlayer, start, target v2Point) []string {
	if target == (v2Point{}) {
		return []string{"0", "x", "-x"}
	}
	a := v2PixelToGameForTeam(start, p.team)
	b := v2PixelToGameForTeam(target, p.team)
	dx := b.x - a.x
	if math.Abs(dx) < 0.25 {
		if dx < 0 {
			dx = -0.25
		} else {
			dx = 0.25
		}
	}
	slope := (b.y - a.y) / dx
	intercept := a.y - slope*a.x
	base := formatV2BotFloat(slope) + "*x+" + formatV2BotFloat(intercept)
	return []string{
		base,
		formatV2BotFloat(slope*0.85) + "*x+" + formatV2BotFloat(intercept),
		formatV2BotFloat(slope*1.15) + "*x+" + formatV2BotFloat(intercept),
		base + "+" + formatV2BotFloat(1.8) + "*sin(0.35*x)",
		base + "-" + formatV2BotFloat(1.8) + "*sin(0.35*x)",
		formatV2BotFloat(b.y),
		"0",
		"x",
		"-x",
	}
}

func (s *Graphwar2Server) scoreBotShotLocked(p *v2ServerPlayer, result v2SampledShot, ok bool, target v2Point) float64 {
	if !ok {
		return -1e9
	}
	score := -math.Hypot(result.end.x-target.x, result.end.y-target.y)
	if result.obstacleHit {
		score -= 5000
	}
	if result.targetPlayer != 0 {
		if hit := s.players[result.targetPlayer]; hit != nil {
			if hit.team == p.team {
				score -= 200000
			} else {
				score += 1000000
			}
		}
	}
	return score
}

func (s *Graphwar2Server) defaultSoldierPosLocked(playerID, soldierID int) v2Point {
	p := s.players[playerID]
	if p == nil {
		return v2Point{}
	}
	idx := 0
	for i, sid := range p.soldiers {
		if sid == soldierID {
			idx = i
			break
		}
	}
	if p.team == "Team2" {
		return v2Point{x: 650, y: 90 + float64(idx)*42}
	}
	return v2Point{x: 120, y: 90 + float64(idx)*42}
}

func v2PixelToGameForTeam(pt v2Point, team string) v2Point {
	x := pt.x
	if team == "Team2" {
		x = 770 - x
	}
	return v2Point{x: 50 * (x - 770.0/2) / 770.0, y: 50 * (-pt.y + 450.0/2) / 770.0}
}

func formatV2BotFloat(v float64) string {
	if math.Abs(v) < 0.0005 {
		v = 0
	}
	return strconv.FormatFloat(v, 'f', 3, 64)
}

func (s *Graphwar2Server) submittedShotsLocked() []v2Shot {
	turns := s.currentTurnInfosLocked()
	shots := make([]v2Shot, 0, len(turns))
	for _, turn := range turns {
		p := s.players[turn.playerID]
		if p == nil || !p.submitted || turn.soldierID == 0 {
			continue
		}
		shots = append(shots, v2Shot{playerID: turn.playerID, soldierID: turn.soldierID, start: p.pos[turn.soldierID], function: p.lastFunc})
	}
	return shots
}

func (s *Graphwar2Server) currentTurnInfosLocked() []v2TurnInfo {
	alivePlayers := s.alivePlayerIDsLocked()
	if len(alivePlayers) == 0 {
		return nil
	}
	if s.turnMode == "SequentialTurns" {
		idx := s.turnIndex % len(alivePlayers)
		pid := alivePlayers[idx]
		return []v2TurnInfo{{playerID: pid, soldierID: s.firstAliveSoldierLocked(pid)}}
	}
	turns := make([]v2TurnInfo, 0, len(alivePlayers))
	for _, pid := range alivePlayers {
		turns = append(turns, v2TurnInfo{playerID: pid, soldierID: s.firstAliveSoldierLocked(pid)})
	}
	return turns
}

func (s *Graphwar2Server) currentSoldierForPlayerLocked(pid int) int {
	for _, turn := range s.currentTurnInfosLocked() {
		if turn.playerID == pid {
			return turn.soldierID
		}
	}
	return s.firstAliveSoldierLocked(pid)
}

func (s *Graphwar2Server) alivePlayerIDsLocked() []int {
	ids := make([]int, 0, len(s.order))
	for _, pid := range s.order {
		if s.firstAliveSoldierLocked(pid) != 0 {
			ids = append(ids, pid)
		}
	}
	return ids
}

func (s *Graphwar2Server) firstAliveSoldierLocked(pid int) int {
	p := s.players[pid]
	if p == nil {
		return 0
	}
	for _, sid := range p.soldiers {
		if p.alive == nil {
			return sid
		}
		alive, ok := p.alive[sid]
		if !ok || alive {
			return sid
		}
	}
	return 0
}

func (s *Graphwar2Server) resolveShotLocked(shot v2Shot, tick int) v2ShotResult {
	targetPlayer, targetSoldier, end, obstacleHit := s.calculateShotHitLocked(shot)
	s.nextEffect++
	return v2ShotResult{
		playerID:      shot.playerID,
		soldierID:     shot.soldierID,
		targetPlayer:  targetPlayer,
		targetSoldier: targetSoldier,
		start:         shot.start,
		end:           end,
		activeTick:    tick,
		effectID:      s.nextEffect,
		obstacleHit:   obstacleHit,
		obstaclePoint: end,
		obstacleRad:   v2ExplosionRadius,
	}
}

func (s *Graphwar2Server) calculateShotHitLocked(shot v2Shot) (int, int, v2Point, bool) {
	if result, ok := s.sampleFunctionShotLocked(shot); ok {
		return result.targetPlayer, result.targetSoldier, result.end, result.obstacleHit
	}
	targetPlayer, targetSoldier, targetPos := s.nearestEnemyLocked(shot.playerID, shot.start)
	end := targetPos
	if targetSoldier == 0 {
		end = v2Point{x: shot.start.x + 160, y: shot.start.y}
		if s.players[shot.playerID] != nil && s.players[shot.playerID].team == "Team2" {
			end.x = shot.start.x - 160
		}
	}
	return targetPlayer, targetSoldier, end, false
}

func (s *Graphwar2Server) nearestEnemyLocked(playerID int, from v2Point) (int, int, v2Point) {
	shooter := s.players[playerID]
	if shooter == nil {
		return 0, 0, v2Point{}
	}
	bestDist := math.MaxFloat64
	var bestPlayer, bestSoldier int
	var bestPos v2Point
	for _, pid := range s.order {
		p := s.players[pid]
		if p == nil || p.team == shooter.team {
			continue
		}
		for _, sid := range p.soldiers {
			if p.alive != nil {
				alive, ok := p.alive[sid]
				if ok && !alive {
					continue
				}
			}
			pos := p.pos[sid]
			dx, dy := pos.x-from.x, pos.y-from.y
			dist := dx*dx + dy*dy
			if dist < bestDist {
				bestDist = dist
				bestPlayer, bestSoldier, bestPos = pid, sid, pos
			}
		}
	}
	return bestPlayer, bestSoldier, bestPos
}

func (s *Graphwar2Server) advanceTurnLocked() {
	if s.turnCount == 0 {
		s.turnCount = 1
	}
	if s.turnMode == "SequentialTurns" {
		alive := s.alivePlayerIDsLocked()
		if len(alive) > 0 {
			s.turnIndex = (s.turnIndex + 1) % len(alive)
		}
	} else {
		s.turnIndex = 0
	}
	s.turnCount++
}

func (s *Graphwar2Server) activeTeamCountLocked() int {
	teams := map[string]bool{}
	for _, pid := range s.order {
		p := s.players[pid]
		if p == nil || s.firstAliveSoldierLocked(pid) == 0 {
			continue
		}
		teams[p.team] = true
	}
	return len(teams)
}

func (s *Graphwar2Server) firstSoldier(pid int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.players[pid]; p != nil && len(p.soldiers) > 0 {
		return p.soldiers[0]
	}
	return 0
}

func (s *Graphwar2Server) ownsPlayer(c *v2ServerConn, pid int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	pid = s.resolvePlayerRefLocked(c, pid)
	p := s.players[pid]
	return p != nil && p.connID == c.id
}

func (s *Graphwar2Server) resolvePlayerRef(c *v2ServerConn, id int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resolvePlayerRefLocked(c, id)
}

func (s *Graphwar2Server) resolvePlayerRefLocked(c *v2ServerConn, id int) int {
	if id == 0 && c != nil {
		return c.player
	}
	if id == 0 {
		return 0
	}
	if s.players[id] != nil {
		return id
	}
	for _, p := range s.players {
		if p.connID == id {
			return p.id
		}
	}
	return id
}

func (s *Graphwar2Server) playerForSoldier(soldierID int) int {
	if soldierID == 0 {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.players {
		for _, sid := range p.soldiers {
			if sid == soldierID {
				return p.id
			}
		}
	}
	return 0
}

func (s *Graphwar2Server) removeConn(c *v2ServerConn) {
	var removedSoldiers []int
	s.mu.Lock()
	if _, ok := s.conns[c.key]; !ok {
		s.mu.Unlock()
		return
	}
	delete(s.conns, c.key)
	pid := c.player
	if pid != 0 {
		if p := s.players[pid]; p != nil {
			removedSoldiers = append(removedSoldiers, p.soldiers...)
		}
		delete(s.players, pid)
		for i, id := range s.order {
			if id == pid {
				s.order = append(s.order[:i], s.order[i+1:]...)
				break
			}
		}
	}
	needLeader := c.leader && len(s.conns) > 0
	var newLeader *v2ServerConn
	if needLeader {
		for _, cc := range s.conns {
			cc.leader = true
			newLeader = cc
			break
		}
	}
	s.mu.Unlock()
	if pid != 0 {
		s.broadcastRemovedEntities(removedSoldiers, pid)
	}
	s.broadcast("GameInfo", s.gameInfoFields())
	if newLeader != nil {
		newLeader.send("RankInfo", map[string]interface{}{"entity_id": newLeader.player, "rank": 1})
	}
	s.updatePublisher()
	_ = c.sock.Close()
}

func (s *Graphwar2Server) handleGraphicsRequest(c *v2ServerConn, raw json.RawMessage, kind string) {
	fields := parseV2Fields(raw)
	entityID := firstV2ID(fields, "entity_id", "soldier_id", "player_id", "owner_id")
	value := sanitizePlain(stringV2Field(fields, "graphics_str", "graphics", "skin", "face", "hat"), 80)
	if value == "" {
		return
	}
	s.mu.Lock()
	ok := false
	for _, p := range s.players {
		if p.connID != c.id && !c.leader {
			continue
		}
		for _, sid := range p.soldiers {
			if sid != entityID {
				continue
			}
			if p.pos == nil {
				p.pos = map[int]v2Point{}
			}
			switch kind {
			case "skin":
				p.skin = value
			case "face":
				p.face = value
			case "hat":
				p.hat = value
			}
			ok = true
			break
		}
	}
	s.mu.Unlock()
	if !ok {
		return
	}
	switch kind {
	case "skin":
		s.broadcast("SkinGraphicsInfo", map[string]interface{}{"entity_id": entityID, "graphics_str": value})
	case "face":
		s.broadcast("FaceGraphicsInfo", map[string]interface{}{"entity_id": entityID, "graphics_str": value})
	case "hat":
		s.broadcast("HatGraphicsInfo", map[string]interface{}{"entity_id": entityID, "graphics_str": value})
	}
}

func (s *Graphwar2Server) broadcastPlayerInfo(p *v2ServerPlayer) {
	s.broadcast("PlayerInfo", map[string]interface{}{"player_id": p.id, "connection_id": p.connID})
	s.broadcast("NameInfo", map[string]interface{}{"entity_id": p.id, "name": p.name})
	s.broadcast("TeamInfo", map[string]interface{}{"entity_id": p.id, "team": p.team})
	s.broadcast("ColorInfo", map[string]interface{}{"entity_id": p.id, "color": v2ColorForPlayer(p.id, p.team)})
}

func (s *Graphwar2Server) assignStartPositions() {
	s.mu.Lock()
	defer s.mu.Unlock()
	leftY, rightY := 90.0, 90.0
	for _, pid := range s.order {
		p := s.players[pid]
		if p == nil {
			continue
		}
		if p.pos == nil {
			p.pos = map[int]v2Point{}
		}
		for _, sid := range p.soldiers {
			if p.team == "Team2" {
				p.pos[sid] = v2Point{x: 650, y: rightY}
				rightY += 42
				if rightY > 360 {
					rightY = 110
				}
			} else {
				p.pos[sid] = v2Point{x: 120, y: leftY}
				leftY += 42
				if leftY > 360 {
					leftY = 110
				}
			}
		}
	}
}

func (s *Graphwar2Server) broadcastSoldierState() {
	s.mu.Lock()
	type soldierState struct {
		id   int
		pid  int
		pos  v2Point
		skin string
		face string
		hat  string
	}
	var states []soldierState
	for _, pid := range s.order {
		p := s.players[pid]
		if p == nil {
			continue
		}
		for _, sid := range p.soldiers {
			pos := p.pos[sid]
			states = append(states, soldierState{id: sid, pid: pid, pos: pos, skin: p.skin, face: p.face, hat: p.hat})
		}
	}
	s.mu.Unlock()
	for _, st := range states {
		s.broadcast("SoldierInfo", map[string]interface{}{"soldier_id": st.id, "player_id": st.pid})
		s.broadcast("PosInfo", map[string]interface{}{"entity_id": st.id, "pos_x": pxToGameX(st.pos.x), "pos_y": pyToGameY(st.pos.y), "radius": radiusToGame(7)})
		s.broadcast("LifeInfo", map[string]interface{}{"entity_id": st.id, "alive": true})
		if st.skin != "" {
			s.broadcast("SkinGraphicsInfo", map[string]interface{}{"entity_id": st.id, "graphics_str": st.skin})
		}
		if st.face != "" {
			s.broadcast("FaceGraphicsInfo", map[string]interface{}{"entity_id": st.id, "graphics_str": st.face})
		}
		if st.hat != "" {
			s.broadcast("HatGraphicsInfo", map[string]interface{}{"entity_id": st.id, "graphics_str": st.hat})
		}
	}
}

func (c *v2ServerConn) sendPlayerInfo(p *v2ServerPlayer) {
	c.send("PlayerInfo", map[string]interface{}{"player_id": p.id, "connection_id": p.connID})
	c.send("NameInfo", map[string]interface{}{"entity_id": p.id, "name": p.name})
	c.send("TeamInfo", map[string]interface{}{"entity_id": p.id, "team": p.team})
	c.send("ColorInfo", map[string]interface{}{"entity_id": p.id, "color": v2ColorForPlayer(p.id, p.team)})
	if c.leader && c.player == p.id {
		c.send("RankInfo", map[string]interface{}{"entity_id": p.id, "rank": 1})
	}
}

func (s *Graphwar2Server) broadcast(variant string, fields map[string]interface{}) {
	s.mu.Lock()
	conns := make([]*v2ServerConn, 0, len(s.conns))
	for _, c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()
	for _, c := range conns {
		c.send(variant, fields)
	}
}

func (c *v2ServerConn) send(variant string, fields map[string]interface{}) {
	if fields == nil {
		fields = map[string]interface{}{}
	}
	payload, err := json.Marshal(map[string]interface{}{variant: fields})
	if err != nil {
		return
	}
	_ = writeV2Frame(c.sock, payload)
}

func nextString(cur string, vals []string) string {
	for i, v := range vals {
		if v == cur {
			return vals[(i+1)%len(vals)]
		}
	}
	if len(vals) == 0 {
		return cur
	}
	return vals[0]
}

func parseV2Fields(raw json.RawMessage) map[string]interface{} {
	var fields map[string]interface{}
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return map[string]interface{}{}
	}
	return fields
}

func firstV2ID(fields map[string]interface{}, keys ...string) int {
	for _, key := range keys {
		if n := intV2Field(fields, key); n != 0 {
			return n
		}
	}
	return 0
}

func intV2Field(fields map[string]interface{}, key string) int {
	v, ok := fields[key]
	if !ok {
		return 0
	}
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case json.Number:
		n, _ := strconv.Atoi(x.String())
		return n
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(x))
		return n
	default:
		return 0
	}
}

func stringV2Field(fields map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		v, ok := fields[key]
		if !ok {
			continue
		}
		switch x := v.(type) {
		case string:
			if strings.TrimSpace(x) != "" {
				return x
			}
		case fmt.Stringer:
			s := x.String()
			if strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	return ""
}

func generateV2Obstacles(seed int64) []v2Obstacle {
	rng := rand.New(rand.NewSource(seed))
	obstacles := make([]v2Obstacle, 0, v2ObstacleCount)
	for attempts := 0; len(obstacles) < v2ObstacleCount && attempts < v2ObstacleCount*80; attempts++ {
		r := randomV2ObstacleRadius(rng)
		x := r + rng.Float64()*(v2PlaneLength-2*r)
		y := r + rng.Float64()*(v2PlaneHeight-2*r)
		ob := v2Obstacle{x: x, y: y, r: r}
		if !validV2Obstacle(ob, obstacles) {
			continue
		}
		obstacles = append(obstacles, ob)
	}
	return obstacles
}

func randomV2ObstacleRadius(rng *rand.Rand) float64 {
	if rng.Float64() < 0.32 {
		return gameRadiusToPixel(0.8 + rng.Float64()*1.9)
	}
	return gameRadiusToPixel(2.4 + rng.Float64()*5.2)
}

func validV2Obstacle(ob v2Obstacle, existing []v2Obstacle) bool {
	for _, spawn := range v2SpawnSafePoints {
		if distSq(ob.x, ob.y, spawn.x, spawn.y) < sq(ob.r+v2SpawnSafeRadius) {
			return false
		}
	}
	for _, other := range existing {
		if distSq(ob.x, ob.y, other.x, other.y) < sq(ob.r+other.r+v2ObstacleMinGap) {
			return false
		}
	}
	return true
}

func pxToGameX(px float64) float64 {
	return 50 * (px - 770.0/2.0) / 770.0
}

func pyToGameY(py float64) float64 {
	return 50 * (-py + 450.0/2.0) / 770.0
}

func radiusToGame(r float64) float64 {
	return 50 * r / 770.0
}

func gameRadiusToPixel(r float64) float64 {
	return 770.0 * r / 50.0
}

func distSq(x1, y1, x2, y2 float64) float64 {
	dx, dy := x1-x2, y1-y2
	return dx*dx + dy*dy
}

func sq(v float64) float64 {
	return v * v
}

func timeModeSeconds(mode string) int {
	switch mode {
	case "Timer30s":
		return 30
	case "Timer2m":
		return 120
	case "Timer3m":
		return 180
	case "Timer5m":
		return 300
	case "TimerInf":
		return 0
	default:
		return 60
	}
}

func (s *Graphwar2Server) gameInfoFields() map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return map[string]interface{}{
		"game_info": map[string]interface{}{
			"room_name":            s.name,
			"game_state":           s.gameState,
			"current_player_count": len(s.players),
			"address":              s.advertisedAddressLocked(),
			"axis_mode":            s.axisMode,
			"function_mode":        s.function,
			"turn_mode":            s.turnMode,
			"time_mode":            s.timeMode,
			"locked":               s.locked,
		},
	}
}

func (s *Graphwar2Server) advertisedAddressLocked() string {
	host := s.publishHost
	if host == "" {
		host = localOutboundHost()
	}
	return net.JoinHostPort(host, strconv.Itoa(s.port))
}

func v2ColorForPlayer(playerID int, team string) map[string]interface{} {
	palette := []map[string]int{
		{"r": 231, "g": 76, "b": 60},
		{"r": 52, "g": 152, "b": 219},
		{"r": 46, "g": 204, "b": 113},
		{"r": 241, "g": 196, "b": 15},
		{"r": 155, "g": 89, "b": 182},
		{"r": 230, "g": 126, "b": 34},
	}
	if team == "Team2" {
		palette = []map[string]int{
			{"r": 52, "g": 152, "b": 219},
			{"r": 41, "g": 128, "b": 185},
			{"r": 26, "g": 188, "b": 156},
			{"r": 22, "g": 160, "b": 133},
			{"r": 149, "g": 165, "b": 166},
			{"r": 127, "g": 140, "b": 141},
		}
	}
	if playerID <= 0 {
		playerID = 1
	}
	c := palette[(playerID-1)%len(palette)]
	return map[string]interface{}{"r": c["r"], "g": c["g"], "b": c["b"]}
}

func (s *Graphwar2Server) publishOptionsLocked() Graphwar2PublishOptions {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.publishOptionsSnapshotLocked()
}

func (s *Graphwar2Server) publishOptionsSnapshotLocked() Graphwar2PublishOptions {
	host := s.publishHost
	if host == "" {
		host = localOutboundHost()
	}
	address := s.advertisedAddressLocked()
	return Graphwar2PublishOptions{
		RoomName:     s.name,
		Address:      address,
		Host:         host,
		LocalHost:    s.publishLocalHost,
		Port:         s.port,
		AxisMode:     s.axisMode,
		FunctionMode: s.function,
		TurnMode:     s.turnMode,
		TimeMode:     s.timeMode,
		Locked:       s.locked,
		GameState:    s.gameState,
		NumPlayers:   len(s.players),
	}
}

func (s *Graphwar2Server) updatePublisher() {
	s.mu.Lock()
	publisher := s.publisher
	opts := s.publishOptionsSnapshotLocked()
	s.mu.Unlock()
	if publisher != nil {
		_ = publisher.Update(opts)
	}
}

func StartGraphwar2CompatRoomAndPublish(ctx context.Context, name string, port int, official bool) (*Graphwar2Server, Graphwar2PublisherStatus, error) {
	s, err := StartGraphwar2CompatRoom(name, port)
	if err != nil {
		return nil, Graphwar2PublisherStatus{}, err
	}
	if !official {
		return s, Graphwar2PublisherStatus{}, nil
	}
	host := localOutboundHost()
	s.mu.Lock()
	s.publisher = DefaultGraphwar2Publisher()
	s.publishHost = host
	s.publishLocalHost = "127.0.0.1"
	opts := s.publishOptionsSnapshotLocked()
	s.mu.Unlock()
	st, err := DefaultGraphwar2Publisher().Start(ctx, opts)
	if err != nil {
		s.Close()
		return nil, st, err
	}
	return s, st, nil
}

func StopGraphwar2CompatRoom(port int) error {
	s := Graphwar2HostedServer(port)
	if s == nil {
		return errors.New("Graphwar II room not found")
	}
	s.Close()
	return nil
}

var _ = rand.Int
