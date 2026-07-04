package backend

import (
	"fmt"
	"math"
	"math/rand"
	"net/url"
	"strings"
	"time"
)

var computerNames = []string{
	"Deep Thought", "HAL", "Skynet", "Agent Smith", "Multivac", "Deep Blue", "Cleverbot", "Alpha Zordon", "Wolfram",
}

const aiPlaneGameLength = 50.0
const aiFuncMaxSteps = 3500

func defaultComputerName() string {
	return computerNames[rand.Intn(len(computerNames))]
}

func (s *GraphServer) AddComputerPlayer(name string, level int) (*splayer, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gameState != PreGame || len(s.players) >= MaxPlayers {
		return nil, false
	}
	name = sanitizePlain(name, MaxNameLen)
	if name == "" {
		name = defaultComputerName()
	}
	p := newComputerPlayer(name, level)
	s.players = append(s.players, p)
	s.setEveryoneNotReady()
	s.sendAddPlayer(p, nil)
	if p.ready {
		s.sendAll(join("&", SetReady, p.id, 1))
	}
	if s.checkAllReady() {
		s.sendStartCountdownLocked()
	}
	return p, true
}

func (s *GraphServer) maybeScheduleComputerTurnLocked() {
	if s.gameState != StateGame || len(s.players) == 0 {
		return
	}
	idx := s.currentTurnPlayerIndexLocked()
	if idx < 0 {
		return
	}
	p := s.players[idx]
	if p == nil || !p.computer {
		return
	}
	playerID := p.id
	delay := time.Duration(350+rand.Intn(500)) * time.Millisecond
	if p.level > 0 {
		delay = time.Duration(250+rand.Intn(350)) * time.Millisecond
	}
	time.AfterFunc(delay, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.gameState != StateGame || len(s.players) == 0 {
			return
		}
		idx := s.currentTurnPlayerIndexLocked()
		if idx < 0 || s.players[idx] == nil || s.players[idx].id != playerID || !s.players[idx].computer {
			return
		}
		fn := s.computerFunctionLocked(s.players[idx], idx)
		if fn == "" {
			fn = "0"
		}
		s.sendAll(join("&", FireFunc, playerID, url.QueryEscape(fn)))
	})
}

func (s *GraphServer) currentTurnPlayerIndexLocked() int {
	if s.gameState != StateGame || len(s.players) == 0 {
		return -1
	}
	if s.turnIndex >= 0 && s.turnIndex < len(s.players) {
		return s.turnIndex
	}
	return -1
}

func (s *GraphServer) advanceTurnIndexLocked() {
	if len(s.players) == 0 {
		s.turnIndex = -1
		return
	}
	start := s.turnIndex
	if start < 0 || start >= len(s.players) {
		start = 0
	}
	for i := 0; i < len(s.players); i++ {
		start = (start + 1) % len(s.players)
		if s.players[start] != nil && s.players[start].numSoldiers > 0 {
			s.turnIndex = start
			return
		}
	}
	s.turnIndex = -1
}

func (s *GraphServer) computerFunctionLocked(bot *splayer, botIndex int) string {
	targets := s.computerTargetsLocked(bot)
	if len(targets) == 0 {
		return "0"
	}
	popSize := 50
	generations := bot.level / 4
	if generations < 6 {
		generations = 6
	}
	if generations > 24 && bot.level <= 9000 {
		generations = 24
	}
	if bot.level > 9000 {
		generations = 42
	}
	pop := make([]aiFunctionCandidate, 0, popSize)
	for _, seed := range s.computerSeedFunctionsLocked(bot, targets) {
		pop = append(pop, aiFunctionCandidate{fn: seed})
	}
	for len(pop) < popSize {
		pop = append(pop, aiFunctionCandidate{fn: randomAIFormula(0)})
	}
	deadline := time.Now().Add(650 * time.Millisecond)
	for gen := 0; gen < generations && time.Now().Before(deadline); gen++ {
		for i := range pop {
			pop[i].score = s.evaluateComputerFunctionLocked(bot, botIndex, pop[i].fn)
		}
		sortAICandidates(pop)
		if pop[0].score > 2500000 {
			break
		}
		next := make([]aiFunctionCandidate, 0, popSize)
		elite := 5
		mutated := 25
		for i := 0; i < elite && i < len(pop); i++ {
			next = append(next, pop[i])
		}
		for len(next) < elite+mutated && len(next) < popSize {
			parent := selectAIParent(pop)
			next = append(next, aiFunctionCandidate{fn: mutateAIFormula(parent.fn)})
		}
		for len(next) < popSize {
			a := selectAIParent(pop)
			b := selectAIParent(pop)
			next = append(next, aiFunctionCandidate{fn: crossoverAIFormula(a.fn, b.fn)})
		}
		pop = next
	}
	for i := range pop {
		pop[i].score = s.evaluateComputerFunctionLocked(bot, botIndex, pop[i].fn)
	}
	sortAICandidates(pop)
	if strings.TrimSpace(pop[0].fn) == "" {
		return "0"
	}
	return pop[0].fn
}

type aiTarget struct {
	x int
	y int
}

type aiFunctionCandidate struct {
	fn    string
	score float64
}

func (s *GraphServer) computerTargetsLocked(bot *splayer) []aiTarget {
	var targets []aiTarget
	for _, p := range s.players {
		if p == nil || p.team == bot.team || p.numSoldiers <= 0 {
			continue
		}
		if pos := s.soldierPos[p.id]; len(pos) > 0 {
			for i := 0; i < p.numSoldiers && i < len(pos); i++ {
				targets = append(targets, aiTarget{x: pos[i].x, y: pos[i].y})
			}
		} else {
			targets = append(targets, aiTarget{x: teamTargetX(p.team), y: PlaneHeight / 2})
		}
	}
	return targets
}

func teamTargetX(team int) int {
	if team == Team1 {
		return PlaneLength / 4
	}
	return PlaneLength * 3 / 4
}

func (s *GraphServer) computerSeedFunctionsLocked(bot *splayer, targets []aiTarget) []string {
	var seeds []string
	origin := s.computerOriginLocked(bot)
	for _, target := range targets {
		a := pixelToAIGame(origin.x, origin.y, bot.team == Team2)
		b := pixelToAIGame(target.x, target.y, bot.team == Team2)
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
		seeds = append(seeds,
			formatAIFloat(slope)+"*x+"+formatAIFloat(intercept),
			formatAIFloat(slope*0.85)+"*x+"+formatAIFloat(intercept),
			formatAIFloat(slope*1.15)+"*x+"+formatAIFloat(intercept),
			formatAIFloat(b.y),
			formatAIFloat(slope)+"*x+"+formatAIFloat(intercept)+"+"+formatAIFloat(2.2)+"*sin(0.4*x)",
			formatAIFloat(slope)+"*x+"+formatAIFloat(intercept)+"-"+formatAIFloat(2.2)+"*sin(0.4*x)",
		)
	}
	seeds = append(seeds, "0", "x", "-x", "sin(x)", "cos(x)")
	return seeds
}

func (s *GraphServer) computerOriginLocked(bot *splayer) pt {
	if pos := s.soldierPos[bot.id]; len(pos) > 0 {
		return pos[0]
	}
	if bot.team == Team2 {
		return pt{x: PlaneLength * 3 / 4, y: PlaneHeight / 2}
	}
	return pt{x: PlaneLength / 4, y: PlaneHeight / 2}
}

type aiGamePoint struct{ x, y float64 }

func pixelToAIGame(px, py int, inverted bool) aiGamePoint {
	x := float64(px)
	if inverted {
		x = float64(PlaneLength) - x
	}
	return aiGamePoint{
		x: aiPlaneGameLength * (x - float64(PlaneLength)/2) / float64(PlaneLength),
		y: aiPlaneGameLength * (-float64(py) + float64(PlaneHeight)/2) / float64(PlaneLength),
	}
}

func aiGameToPixel(gx, gy float64, inverted bool) aiGamePoint {
	x := float64(PlaneLength)*gx/aiPlaneGameLength + float64(PlaneLength)/2
	y := -float64(PlaneLength)*gy/aiPlaneGameLength + float64(PlaneHeight)/2
	if inverted {
		x = float64(PlaneLength) - x
	}
	return aiGamePoint{x: x, y: y}
}

func (s *GraphServer) evaluateComputerFunctionLocked(bot *splayer, botIndex int, fn string) float64 {
	formula, err := parseV2Formula(fn)
	if err != nil {
		return -1e12
	}
	origin := s.computerOriginLocked(bot)
	inverted := bot.team == Team2
	radius := aiPlaneGameLength * float64(SoldierRadius) / float64(PlaneLength)
	g := pixelToAIGame(origin.x, origin.y, inverted)
	angle := formula.startAngle(g.x, radius)
	if !isFiniteNumber(angle) {
		angle = 0
	}
	g.x += radius * math.Cos(angle)
	g.y += radius * math.Sin(angle)
	offset := -formula.eval(g.x, 0, 0) + g.y
	prevX, prevY := g.x, g.y
	points := []aiGamePoint{aiGameToPixel(prevX, prevY, inverted)}
	hitPlayer, hitFriendly := s.aiHitAtLocked(bot, botIndex, points[0])
	if hitPlayer != nil {
		if hitFriendly {
			return -2000000
		}
		return 3000000
	}
	for i := 1; i < aiFuncMaxSteps; i++ {
		step := v2StepSize
		nextX := prevX + step
		nextY := formula.eval(nextX, 0, 0) + offset
		endFunc := false
		for math.Pow(nextX-prevX, 2)+math.Pow(nextY-prevY, 2) > v2FuncMaxStepDistanceSq {
			if nextX-prevX <= v2FuncMinXStepDistance {
				endFunc = true
				break
			}
			step /= 2
			nextX = prevX + step
			nextY = formula.eval(nextX, 0, 0) + offset
		}
		if endFunc || !isFiniteNumber(nextY) {
			break
		}
		p := aiGameToPixel(nextX, nextY, inverted)
		points = append(points, p)
		if s.aiObstacleAtLocked(p) {
			break
		}
		hit, friendly := s.aiHitAtLocked(bot, botIndex, p)
		if hit != nil {
			if friendly {
				return -2000000
			}
			return 3000000
		}
		prevX, prevY = nextX, nextY
	}
	minDistSq := 1000000.0
	for _, p := range s.players {
		if p == nil || p.team == bot.team {
			continue
		}
		for _, sp := range s.soldierPos[p.id] {
			for _, tp := range points {
				dx := tp.x - float64(sp.x)
				dy := tp.y - float64(sp.y)
				d := dx*dx + dy*dy
				if d < minDistSq {
					minDistSq = d
				}
			}
		}
	}
	return 1000000 - minDistSq
}

func (s *GraphServer) aiHitAtLocked(bot *splayer, botIndex int, p aiGamePoint) (*splayer, bool) {
	for idx, pl := range s.players {
		if pl == nil {
			continue
		}
		for si, sp := range s.soldierPos[pl.id] {
			if idx == botIndex && pl.id == bot.id && si == 0 {
				continue
			}
			dx := p.x - float64(sp.x)
			dy := p.y - float64(sp.y)
			if dx*dx+dy*dy < float64(SoldierRadius*SoldierRadius) {
				return pl, pl.team == bot.team
			}
		}
	}
	return nil, false
}

func (s *GraphServer) aiObstacleAtLocked(p aiGamePoint) bool {
	if p.x < 0 || p.x >= float64(PlaneLength) || p.y < 0 || p.y >= float64(PlaneHeight) {
		return true
	}
	for i := 0; i+2 < len(s.circles); i += 3 {
		dx := p.x - float64(s.circles[i])
		dy := p.y - float64(s.circles[i+1])
		r := float64(s.circles[i+2])
		if dx*dx+dy*dy < r*r {
			return true
		}
	}
	return false
}

func sortAICandidates(pop []aiFunctionCandidate) {
	for i := 1; i < len(pop); i++ {
		cur := pop[i]
		j := i - 1
		for j >= 0 && pop[j].score < cur.score {
			pop[j+1] = pop[j]
			j--
		}
		pop[j+1] = cur
	}
}

func selectAIParent(pop []aiFunctionCandidate) aiFunctionCandidate {
	limit := 12
	if len(pop) < limit {
		limit = len(pop)
	}
	return pop[rand.Intn(limit)]
}

func randomAIFormula(depth int) string {
	if depth > 3 || rand.Float64() < 0.45 {
		if rand.Float64() < 0.55 {
			return "x"
		}
		return formatAIFloat(rand.NormFloat64() * 6)
	}
	a := randomAIFormula(depth + 1)
	b := randomAIFormula(depth + 1)
	switch rand.Intn(8) {
	case 0:
		return "(" + a + "+" + b + ")"
	case 1:
		return "(" + a + "-" + b + ")"
	case 2:
		return "(" + a + "*" + b + ")"
	case 3:
		return "sin(" + a + ")"
	case 4:
		return "cos(" + a + ")"
	case 5:
		return "abs(" + a + ")"
	case 6:
		return "(" + a + "/(" + b + "+1))"
	default:
		return "(" + a + "^2)"
	}
}

func mutateAIFormula(fn string) string {
	fn = strings.TrimSpace(fn)
	if fn == "" || rand.Float64() < 0.25 {
		return randomAIFormula(0)
	}
	if rand.Float64() < 0.45 {
		return "(" + fn + "+" + formatAIFloat(rand.NormFloat64()*2) + ")"
	}
	if rand.Float64() < 0.5 {
		return "(" + formatAIFloat(0.8+rand.Float64()*0.4) + "*(" + fn + "))"
	}
	return "(" + fn + "+" + formatAIFloat(rand.NormFloat64()*2) + "*sin(" + formatAIFloat(0.2+rand.Float64()*0.8) + "*x))"
}

func crossoverAIFormula(a, b string) string {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	switch rand.Intn(3) {
	case 0:
		return "((" + a + ")+(" + b + "))/2"
	case 1:
		return "(" + a + "+" + formatAIFloat(rand.Float64()*2-1) + "*(" + b + "))"
	default:
		return "(" + a + "+sin(" + b + "))"
	}
}

func formatAIFloat(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "0"
	}
	if math.Abs(v) < 0.0001 {
		v = 0
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.3f", v), "0"), ".")
}
