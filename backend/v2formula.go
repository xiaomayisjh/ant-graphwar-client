package backend

import (
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
)

const (
	v2PlaneLength              = 770.0
	v2PlaneHeight              = 450.0
	v2PlaneGameLength          = 50.0
	v2SoldierRadius            = 7.0
	v2ExplosionRadius          = 12.0
	v2StepSize                 = 0.01
	v2FuncMaxSteps             = 20000
	v2FuncMaxStepDistanceSq    = 0.001
	v2FuncMinXStepDistance     = 0.00001
	v2AngleError               = math.Pi / 360.0
	v2MaxAngleLoops            = 100
	v2MaxFormulaTokens         = 4000
	v2MaxFormulaRecursionDepth = 2000
	v2ObstacleCount            = 18
	v2ObstacleMinGap           = 2.0
	v2SpawnSafeRadius          = 62.0
)

type v2TokenType int

const (
	v2TokAdd v2TokenType = iota + 1
	v2TokSubtract
	v2TokMultiply
	v2TokDivide
	v2TokPow
	v2TokSqrt
	v2TokLog
	v2TokAbs
	v2TokSin
	v2TokCos
	v2TokTan
	v2TokLn
	v2TokVarX
	v2TokVarY
	v2TokVarDY
	v2TokValue
	v2TokLeftBracket
	v2TokRightBracket
)

type v2FormulaToken struct {
	typ   v2TokenType
	value float64
}

type v2Formula struct {
	tokens []v2FormulaToken
	read   int
	x      float64
	y      float64
	dy     float64
}

type v2SampledShot struct {
	targetPlayer  int
	targetSoldier int
	end           v2Point
	obstacleHit   bool
}

var v2FormulaTokenRE = regexp.MustCompile(`[0-9]*\.?[0-9]+|\(|\)|x|y|y'|\+|\*|/|\^|sqrt|log|abs|sin|sen|cos|tan|tg|-|ln|e|pi`)

func parseV2Formula(input string) (*v2Formula, error) {
	if len(input) == 0 || len(input) > MaxFuncLen {
		return nil, errors.New("bad formula length")
	}
	regular := regularV2FormulaTokens(input)
	if len(regular) == 0 || len(regular) > v2MaxFormulaTokens {
		return nil, errors.New("bad formula tokens")
	}
	polish := make([]v2FormulaToken, 0, len(regular))
	if !reorderV2Formula(&polish, regular, 0, len(regular)-1, 0) {
		return nil, errors.New("malformed formula")
	}
	if valuesNeededV2Formula(polish) != 0 {
		return nil, errors.New("malformed formula")
	}
	return &v2Formula{tokens: polish}, nil
}

func regularV2FormulaTokens(input string) []v2FormulaToken {
	input = strings.ToLower(input)
	input = strings.ReplaceAll(input, "-", "+-")
	input = strings.ReplaceAll(input, "exp", "e^")
	input = strings.ReplaceAll(input, ",", ".")
	matches := v2FormulaTokenRE.FindAllString(input, -1)
	out := make([]v2FormulaToken, 0, len(matches))
	for _, m := range matches {
		switch m {
		case "x":
			out = append(out, v2FormulaToken{typ: v2TokVarX})
		case "y":
			out = append(out, v2FormulaToken{typ: v2TokVarY})
		case "y'":
			out = append(out, v2FormulaToken{typ: v2TokVarDY})
		case "+":
			out = append(out, v2FormulaToken{typ: v2TokAdd})
		case "-":
			out = append(out, v2FormulaToken{typ: v2TokSubtract})
		case "*":
			out = append(out, v2FormulaToken{typ: v2TokMultiply})
		case "/":
			out = append(out, v2FormulaToken{typ: v2TokDivide})
		case "^":
			out = append(out, v2FormulaToken{typ: v2TokPow})
		case "sqrt":
			out = append(out, v2FormulaToken{typ: v2TokSqrt})
		case "log":
			out = append(out, v2FormulaToken{typ: v2TokLog})
		case "abs":
			out = append(out, v2FormulaToken{typ: v2TokAbs})
		case "sin", "sen":
			out = append(out, v2FormulaToken{typ: v2TokSin})
		case "cos":
			out = append(out, v2FormulaToken{typ: v2TokCos})
		case "tan", "tg":
			out = append(out, v2FormulaToken{typ: v2TokTan})
		case "ln":
			out = append(out, v2FormulaToken{typ: v2TokLn})
		case "e":
			out = append(out, v2FormulaToken{typ: v2TokValue, value: math.E})
		case "pi":
			out = append(out, v2FormulaToken{typ: v2TokValue, value: math.Pi})
		case "(":
			out = append(out, v2FormulaToken{typ: v2TokLeftBracket})
		case ")":
			out = append(out, v2FormulaToken{typ: v2TokRightBracket})
		default:
			if v, err := strconv.ParseFloat(m, 64); err == nil {
				out = append(out, v2FormulaToken{typ: v2TokValue, value: v})
			}
		}
	}
	return adjustV2ImplicitMultiplication(out)
}

func adjustV2ImplicitMultiplication(tokens []v2FormulaToken) []v2FormulaToken {
	if len(tokens) == 0 {
		return tokens
	}
	out := []v2FormulaToken{tokens[0]}
	for i := 1; i < len(tokens); i++ {
		if isV2Implicit(out[len(out)-1].typ, tokens[i].typ) {
			out = append(out, v2FormulaToken{typ: v2TokMultiply})
		}
		out = append(out, tokens[i])
	}
	return out
}

func isV2Implicit(left, right v2TokenType) bool {
	leftValue := left == v2TokValue || left == v2TokVarX || left == v2TokVarY || left == v2TokVarDY || left == v2TokRightBracket
	rightValue := right == v2TokValue || right == v2TokVarX || right == v2TokVarY || right == v2TokVarDY || right == v2TokLeftBracket || numV2Params(right) == 1
	return leftValue && rightValue
}

func reorderV2Formula(polish *[]v2FormulaToken, tokens []v2FormulaToken, start, end, depth int) bool {
	if depth > v2MaxFormulaRecursionDepth {
		return false
	}
	if start > end || start >= len(tokens) {
		return false
	}
	next := -1
	nextNest := math.MaxInt
	nest := 0
	for i := start; i <= end; i++ {
		typ := tokens[i].typ
		if typ == v2TokLeftBracket {
			nest++
		} else if typ == v2TokRightBracket {
			nest--
		} else if nest < nextNest || (nest == nextNest && (next == -1 || typ < tokens[next].typ)) {
			next = i
			nextNest = nest
		}
	}
	if next == -1 {
		return false
	}
	switch numV2Params(tokens[next].typ) {
	case 0:
		*polish = append(*polish, tokens[next])
	case 1:
		*polish = append(*polish, tokens[next])
		reorderV2Formula(polish, tokens, next+1, end, depth+1)
	case 2:
		*polish = append(*polish, tokens[next])
		left := reorderV2Formula(polish, tokens, start, next-1, depth+1)
		if tokens[next].typ == v2TokAdd && !left {
			*polish = append(*polish, v2FormulaToken{typ: v2TokValue})
		}
		reorderV2Formula(polish, tokens, next+1, end, depth+1)
	}
	return true
}

func valuesNeededV2Formula(tokens []v2FormulaToken) int {
	needed := 1
	for i, tok := range tokens {
		if isV2Operation(tok.typ) {
			needed += numV2Params(tok.typ) - 1
		} else {
			needed--
		}
		if needed == 0 && i+1 < len(tokens) {
			return -1
		}
	}
	return needed
}

func isV2Operation(typ v2TokenType) bool {
	return typ >= v2TokAdd && typ <= v2TokLn
}

func numV2Params(typ v2TokenType) int {
	if typ == v2TokSubtract {
		return 1
	}
	if typ >= v2TokAdd && typ <= v2TokPow {
		return 2
	}
	if typ >= v2TokSqrt && typ <= v2TokLn {
		return 1
	}
	return 0
}

func (f *v2Formula) eval(x, y, dy float64) float64 {
	f.x, f.y, f.dy = x, y, dy
	f.read = 0
	return f.evalRec()
}

func (f *v2Formula) evalRec() float64 {
	if f.read >= len(f.tokens) {
		return math.NaN()
	}
	cur := f.tokens[f.read]
	f.read++
	switch cur.typ {
	case v2TokVarX:
		return f.x
	case v2TokVarY:
		return f.y
	case v2TokVarDY:
		return f.dy
	case v2TokValue:
		return cur.value
	case v2TokAdd:
		return f.evalRec() + f.evalRec()
	case v2TokSubtract:
		return -f.evalRec()
	case v2TokMultiply:
		return f.evalRec() * f.evalRec()
	case v2TokDivide:
		return f.evalRec() / f.evalRec()
	case v2TokSqrt:
		return math.Sqrt(f.evalRec())
	case v2TokLog:
		return math.Log10(f.evalRec())
	case v2TokAbs:
		return math.Abs(f.evalRec())
	case v2TokSin:
		return math.Sin(f.evalRec())
	case v2TokCos:
		return math.Cos(f.evalRec())
	case v2TokTan:
		return math.Tan(f.evalRec())
	case v2TokPow:
		return math.Pow(f.evalRec(), f.evalRec())
	case v2TokLn:
		return math.Log(f.evalRec())
	default:
		return 0
	}
}

func (s *Graphwar2Server) sampleFunctionShotLocked(shot v2Shot) (v2SampledShot, bool) {
	if shot.function == "" {
		return v2SampledShot{}, false
	}
	formula, err := parseV2Formula(shot.function)
	if err != nil {
		return v2SampledShot{}, false
	}
	shooter := s.players[shot.playerID]
	if shooter == nil {
		return v2SampledShot{}, false
	}
	switch s.function {
	case "NormalFunction":
		return s.sampleNormalFunctionShotLocked(shot, formula, shooter), true
	case "DiffEqFunction", "FirstOrderODE", "FirstOrderOde", "FstODE":
		return s.sampleFirstOrderODEShotLocked(shot, formula, shooter), true
	case "SecondDiffEqFunction", "SecondOrderODE", "SecondOrderOde", "SndODE":
		return s.sampleSecondOrderODEShotLocked(shot, formula, shooter, 0), true
	default:
		return v2SampledShot{}, false
	}
}

func (s *Graphwar2Server) sampleNormalFunctionShotLocked(shot v2Shot, formula *v2Formula, shooter *v2ServerPlayer) v2SampledShot {
	inverted := shooter.team == "Team2"
	gameRadius := v2PlaneGameLength * v2SoldierRadius / v2PlaneLength
	gx := pixelToV2GameX(shot.start.x, inverted)
	gy := pixelToV2GameY(shot.start.y)
	angle := formula.startAngle(gx, gameRadius)
	if !isFiniteNumber(angle) {
		angle = 0
	}
	gx += gameRadius * math.Cos(angle)
	gy += gameRadius * math.Sin(angle)
	offset := -formula.eval(gx, 0, 0) + gy
	prevGX, prevGY := gx, gy
	last := gameToV2Pixel(gx, gy, inverted)
	for i := 1; i < v2FuncMaxSteps; i++ {
		step := v2StepSize
		nextGX := prevGX + step
		nextGY := formula.eval(nextGX, 0, 0) + offset
		endFunc := false
		for math.Pow(nextGX-prevGX, 2)+math.Pow(nextGY-prevGY, 2) > v2FuncMaxStepDistanceSq {
			if nextGX-prevGX <= v2FuncMinXStepDistance {
				endFunc = true
				break
			}
			step /= 2
			nextGX = prevGX + step
			nextGY = formula.eval(nextGX, 0, 0) + offset
		}
		if endFunc || !isFiniteNumber(nextGY) {
			return v2SampledShot{end: last}
		}
		pos := gameToV2Pixel(nextGX, nextGY, inverted)
		if sampled, hit := s.sampledShotCollisionLocked(shot, pos); hit {
			return sampled
		}
		prevGX, prevGY, last = nextGX, nextGY, pos
	}
	return v2SampledShot{end: last}
}

func (s *Graphwar2Server) sampleFirstOrderODEShotLocked(shot v2Shot, formula *v2Formula, shooter *v2ServerPlayer) v2SampledShot {
	inverted := shooter.team == "Team2"
	gameRadius := v2PlaneGameLength * v2SoldierRadius / v2PlaneLength
	gx := pixelToV2GameX(shot.start.x, inverted)
	gy := pixelToV2GameY(shot.start.y)
	angle := formula.rk4StartAngle(gx, gy, gameRadius)
	if !isFiniteNumber(angle) {
		angle = 0
	}
	gx += gameRadius * math.Cos(angle)
	gy += gameRadius * math.Sin(angle)
	last := gameToV2Pixel(gx, gy, inverted)
	for i := 1; i < v2FuncMaxSteps; i++ {
		h := v2StepSize
		nextGX, nextGY := firstOrderODEStep(formula, gx, gy, h)
		endFunc := false
		for math.Pow(nextGX-gx, 2)+math.Pow(nextGY-gy, 2) > v2FuncMaxStepDistanceSq && nextGX-gx > v2FuncMinXStepDistance {
			if nextGX-gx <= v2FuncMinXStepDistance {
				endFunc = true
				break
			}
			h /= 2
			nextGX, nextGY = firstOrderODEStep(formula, gx, gy, h)
		}
		if endFunc || !isFiniteNumber(nextGY) {
			return v2SampledShot{end: last}
		}
		pos := gameToV2Pixel(nextGX, nextGY, inverted)
		if sampled, hit := s.sampledShotCollisionLocked(shot, pos); hit {
			return sampled
		}
		gx, gy, last = nextGX, nextGY, pos
	}
	return v2SampledShot{end: last}
}

func (s *Graphwar2Server) sampleSecondOrderODEShotLocked(shot v2Shot, formula *v2Formula, shooter *v2ServerPlayer, angle float64) v2SampledShot {
	inverted := shooter.team == "Team2"
	px := shot.start.x
	if inverted {
		px = v2PlaneLength - px
	}
	px += v2SoldierRadius * math.Cos(angle)
	py := shot.start.y - v2SoldierRadius*math.Sin(angle)
	gx := v2PlaneGameLength * (px - v2PlaneLength/2) / v2PlaneLength
	gy := pixelToV2GameY(py)
	gdy := math.Tan(angle)
	last := gameToV2Pixel(gx, gy, inverted)
	for i := 1; i < v2FuncMaxSteps; i++ {
		h := v2StepSize
		nextGX, nextGY, nextGDY := secondOrderODEStep(formula, gx, gy, gdy, h)
		endFunc := false
		for math.Pow(nextGX-gx, 2)+math.Pow(nextGY-gy, 2) > v2FuncMaxStepDistanceSq && nextGX-gx > v2FuncMinXStepDistance {
			if nextGX-gx <= v2FuncMinXStepDistance {
				endFunc = true
				break
			}
			h /= 2
			nextGX, nextGY, nextGDY = secondOrderODEStep(formula, gx, gy, gdy, h)
		}
		if endFunc || !isFiniteNumber(nextGY) || !isFiniteNumber(nextGDY) {
			return v2SampledShot{end: last}
		}
		pos := gameToV2Pixel(nextGX, nextGY, inverted)
		if sampled, hit := s.sampledShotCollisionLocked(shot, pos); hit {
			return sampled
		}
		gx, gy, gdy, last = nextGX, nextGY, nextGDY, pos
	}
	return v2SampledShot{end: last}
}

func firstOrderODEStep(formula *v2Formula, x, y, h float64) (float64, float64) {
	k1 := formula.eval(x, y, 0)
	k2 := formula.eval(x+0.5*h, y+0.5*h*k1, 0)
	k3 := formula.eval(x+0.5*h, y+0.5*h*k2, 0)
	k4 := formula.eval(x+h, y+h*k3, 0)
	return x + h, y + (h/6)*(k1+2*k2+2*k3+k4)
}

func secondOrderODEStep(formula *v2Formula, x, y, dy, h float64) (float64, float64, float64) {
	k11 := dy
	k12 := formula.eval(x, y, dy)
	x1 := x + h/2
	y1 := y + (h/2)*k11
	y2 := dy + (h/2)*k12
	k21 := y2
	k22 := formula.eval(x1, y1, y2)
	y1 = y + (h/2)*k21
	y2 = dy + (h/2)*k22
	k31 := y2
	k32 := formula.eval(x1, y1, y2)
	x1 = x + h
	y1 = y + h*k31
	y2 = dy + h*k32
	k41 := y2
	k42 := formula.eval(x1, y1, y2)
	return x + h, y + (h/6)*(k11+2*k21+2*k31+k41), dy + (h/6)*(k12+2*k22+2*k32+k42)
}

func (s *Graphwar2Server) sampledShotCollisionLocked(shot v2Shot, pos v2Point) (v2SampledShot, bool) {
	if targetPlayer, targetSoldier := s.hitSoldierAtLocked(shot, pos); targetSoldier != 0 {
		return v2SampledShot{targetPlayer: targetPlayer, targetSoldier: targetSoldier, end: pos}, true
	}
	if s.obstacleAtLocked(pos) {
		return v2SampledShot{end: pos, obstacleHit: true}, true
	}
	return v2SampledShot{}, false
}

func (f *v2Formula) startAngle(x, radius float64) float64 {
	angle := 0.0
	tangent := (f.eval(x+v2StepSize, 0, 0) - f.eval(x, 0, 0)) / v2StepSize
	angle = math.Atan(tangent)
	err := 10000.0
	for i := 0; err > v2AngleError && i < v2MaxAngleLoops; i++ {
		finalX := x + radius*math.Cos(angle)
		tangent = (f.eval(finalX+v2StepSize, 0, 0) - f.eval(finalX, 0, 0)) / v2StepSize
		next := math.Atan(tangent)
		err = math.Abs(next - angle)
		angle = next
	}
	return angle
}

func (f *v2Formula) rk4StartAngle(x, y, radius float64) float64 {
	angle := 0.0
	err := 10000.0
	for i := 0; err > v2AngleError && i < v2MaxAngleLoops; i++ {
		finalX := x + radius*math.Cos(angle)
		finalY := y + radius*math.Sin(angle)
		nextX, nextY := firstOrderODEStep(f, finalX, finalY, v2StepSize)
		tangent := (nextY - finalY) / (nextX - finalX)
		next := math.Atan(tangent)
		err = math.Abs(next - angle)
		angle = next
	}
	return angle
}

func (s *Graphwar2Server) hitSoldierAtLocked(shot v2Shot, pos v2Point) (int, int) {
	for _, pid := range s.order {
		p := s.players[pid]
		if p == nil {
			continue
		}
		for _, sid := range p.soldiers {
			if pid == shot.playerID && sid == shot.soldierID {
				continue
			}
			if p.alive != nil {
				alive, ok := p.alive[sid]
				if ok && !alive {
					continue
				}
			}
			soldierPos := p.pos[sid]
			dx, dy := soldierPos.x-pos.x, soldierPos.y-pos.y
			if dx*dx+dy*dy < v2SoldierRadius*v2SoldierRadius {
				return pid, sid
			}
		}
	}
	return 0, 0
}

func (s *Graphwar2Server) obstacleAtLocked(pos v2Point) bool {
	if pos.x < 0 || pos.x >= v2PlaneLength || pos.y < 0 || pos.y >= v2PlaneHeight {
		return true
	}
	for _, ob := range s.obstacles {
		dx, dy := pos.x-ob.x, pos.y-ob.y
		if dx*dx+dy*dy < ob.r*ob.r {
			return true
		}
	}
	return false
}

func pixelToV2GameX(px float64, inverted bool) float64 {
	if inverted {
		px = v2PlaneLength - px
	}
	return v2PlaneGameLength * (px - v2PlaneLength/2) / v2PlaneLength
}

func pixelToV2GameY(py float64) float64 {
	return v2PlaneGameLength * (-py + v2PlaneHeight/2) / v2PlaneLength
}

func gameToV2Pixel(gx, gy float64, inverted bool) v2Point {
	x := v2PlaneLength*gx/v2PlaneGameLength + v2PlaneLength/2
	y := -v2PlaneLength*gy/v2PlaneGameLength + v2PlaneHeight/2
	if inverted {
		x = v2PlaneLength - x
	}
	return v2Point{x: x, y: y}
}

func isFiniteNumber(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
