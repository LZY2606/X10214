package engine

// runConfig steers a single-thread VM run.
type runConfig struct {
	startPc     int
	startPos    int
	initCap     []int
	initPath    [][2]int
	initAlts    []alt
	budget      Budget
	breakpoints map[int]map[int]bool
	stopAfter   int // pause after this many dispatched instructions (0 = run)
}

func run(prog *Program, in *Input, cfg runConfig) *cursor {
	c := newCursor(prog, in, cfg.startPc, cfg.startPos, cfg.initCap)
	if cfg.initPath != nil {
		c.path = cfg.initPath
	} else {
		c.path = [][2]int{{cfg.startPc, cfg.startPos}}
	}
	c.alts = cfg.initAlts
	c.budget = cfg.budget
	c.breakpoints = cfg.breakpoints
	for {
		if c.status == StatusMatch {
			return c
		}
		if cfg.stopAfter > 0 && c.steps >= cfg.stopAfter {
			c.status = StatusPausedStep
			c.emitAt(EvPause, c.pc, c.pos).Note = "step limit reached"
			return c
		}
		if c.budget.stepLimited(c.steps) || c.budget.stateLimited(c.states) ||
			c.budget.snapLimited(c.snaps) {
			c.status = StatusPausedBudg
			c.pausedReason = c.overBudgetReason()
			c.emitAt(EvPause, c.pc, c.pos).Note = c.pausedReason
			return c
		}
		if hit, posAny := c.breakpointHit(); hit {
			c.status = StatusPausedBP
			note := "breakpoint"
			if !posAny {
				note = "breakpoint@" + itoa(c.pos)
			}
			c.emitAt(EvPause, c.pc, c.pos).Note = note
			return c
		}
		if c.pc < 0 || c.pc >= len(c.prog.Code) {
			if !c.backtrack() {
				c.status = StatusNoMatch
				c.emitAt(EvFail, c.pc, c.pos)
				return c
			}
			continue
		}
		ins := c.prog.Code[c.pc]
		c.steps++
		switch ins.Op {
		case OpChar:
			if c.pos < c.in.Len() && c.in.Syms[c.pos].R == ins.R {
				e := c.emit(EvConsume)
				e.Rune = string(ins.R)
				c.pos++
				c.pc++
			} else if !c.backtrack() {
				c.finishFail()
				return c
			}
		case OpAny:
			if c.pos < c.in.Len() && c.in.Syms[c.pos].R != '\n' {
				e := c.emit(EvConsume)
				e.Rune = string(c.in.Syms[c.pos].R)
				c.pos++
				c.pc++
			} else if !c.backtrack() {
				c.finishFail()
				return c
			}
		case OpClass:
			if c.pos < c.in.Len() && ins.M.Match(c.in.Syms[c.pos]) {
				e := c.emit(EvConsume)
				e.Rune = string(c.in.Syms[c.pos].R)
				c.pos++
				c.pc++
			} else if !c.backtrack() {
				c.finishFail()
				return c
			}
		case OpAnchor:
			ok := evalAnchor(ins.Anchor, c.in, c.pos)
			e := c.emit(EvAnchor)
			e.Note = string(ins.Anchor)
			if ok {
				c.pc++
			} else if !c.backtrack() {
				c.finishFail()
				return c
			}
		case OpBackref:
			lo, hi := c.cap[2*ins.A], c.cap[2*ins.A+1]
			e := c.emit(EvBackref)
			e.Group = ins.A
			if lo < 0 || hi < 0 {
				// Unset reference: matches empty (documented CTR choice).
				e.Note = "unset->empty"
				c.pc++
				continue
			}
			if matchBackref(c.in, c.pos, lo, hi) {
				e.Note = "ok"
				c.pos += hi - lo
				c.pc++
			} else if !c.backtrack() {
				c.finishFail()
				return c
			}
		case OpJump:
			c.pathPush(ins.A, c.pos)
			c.pc = ins.A
		case OpSplit:
			c.handleSplit(ins)
		case OpSave:
			oldLo, oldHi := c.cap[ins.A], c.cap[ins.A^1]
			c.cap[ins.A] = c.pos
			e := c.emit(EvCapSet)
			e.Group = ins.Group
			e.OldLo, e.OldHi = oldLo, oldHi
			e.NewLo, e.NewHi = c.pos, c.cap[ins.A^1]
			c.pathPush(c.pc+1, c.pos)
			c.pc++
		case OpMatch:
			if ins.LookID == 0 {
				c.finishMatch()
				return c
			}
			if !c.finishLook(ins) {
				return c
			}
		case OpFail:
			if !c.backtrack() {
				c.finishFail()
				return c
			}
		case OpLAMark:
			c.enterLook(ins)
		default:
			if !c.backtrack() {
				c.finishFail()
				return c
			}
		}
	}
}

func (c *cursor) breakpointHit() (bool, bool) {
	if c.breakpoints == nil {
		return false, false
	}
	set := c.breakpoints[c.pc]
	if set == nil {
		return false, false
	}
	if set[-1] {
		return true, true
	}
	return set[c.pos], false
}

func (c *cursor) pathPush(pc, pos int) { c.path = append(c.path, [2]int{pc, pos}) }

func (c *cursor) handleSplit(ins Instr) {
	// Compiled branch A is the preferred one; B is suspended. This preserves
	// the documented source order for both greedy and lazy quantifiers.
	chosen, deferred := ins.A, ins.B
	label := "primary"
	if ins.QuantID != 0 {
		if ins.Greedy {
			label = "repeat"
		} else {
			label = "stop"
		}
	}
	e := c.emit(EvSplit)
	e.QuantID = ins.QuantID
	e.Chosen = label
	c.states++
	// Zero-width repetition guard: deferred (pc,pos) already on path -> prune.
	if deferred >= 0 && c.onPath(deferred, c.pos) {
		pe := c.emitAt(EvPrune, deferred, c.pos)
		pe.QuantID = ins.QuantID
		pe.Note = "zero-width repetition suppressed"
	} else if deferred >= 0 {
		c.snaps++
		c.alts = append(c.alts, alt{pc: deferred, pos: c.pos,
			cap: cloneInts(c.cap), pathLen: len(c.path)})
	}
	c.pathPush(chosen, c.pos)
	c.pc = chosen
}

func (c *cursor) onPath(pc, pos int) bool {
	for i := len(c.path) - 1; i >= 0; i-- {
		if c.path[i][0] == pc && c.path[i][1] == pos {
			return true
		}
	}
	return false
}

func (c *cursor) backtrack() bool {
	for len(c.alts) > 0 {
		a := c.alts[len(c.alts)-1]
		c.alts = c.alts[:len(c.alts)-1]
		if a.isMarker {
			if c.advanceMarker(a.marker) {
				c.alts = append(c.alts, a)
				return true
			}
			continue
		}
		// Restore path and captures.
		c.path = c.path[:a.pathLen]
		c.restoreCaps(a.cap, a.pos)
		if c.onPath(a.pc, a.pos) {
			pe := c.emitAt(EvPrune, a.pc, a.pos)
			pe.Note = "zero-width repetition suppressed on backtrack"
			continue
		}
		e := c.emitAt(EvBacktrack, a.pc, a.pos)
		_ = e
		c.pc, c.pos = a.pc, a.pos
		c.cap = a.cap
		c.pathPush(a.pc, a.pos)
		return true
	}
	return false
}

func (c *cursor) restoreCaps(target []int, pos int) {
	for i := 1; i < len(target)/2; i++ {
		if c.cap[2*i] != target[2*i] || c.cap[2*i+1] != target[2*i+1] {
			e := c.emitAt(EvCapRoll, 0, pos)
			e.Group = i
			e.OldLo, e.OldHi = c.cap[2*i], c.cap[2*i+1]
			e.NewLo, e.NewHi = target[2*i], target[2*i+1]
		}
	}
}

func (c *cursor) finishFail() {
	c.status = StatusNoMatch
	c.emit(EvFail)
}

func (c *cursor) finishMatch() {
	c.status = StatusMatch
	c.matched = true
	c.matchStart = c.cap[0]
	c.matchEnd = c.pos
	c.cap[0] = c.matchStart
	c.cap[1] = c.matchEnd
	c.matchCaps = make([]Cap, len(c.cap)/2)
	for i := range c.matchCaps {
		c.matchCaps[i] = Cap{c.cap[2*i], c.cap[2*i+1]}
	}
	e := c.emit(EvMatch)
	e.Note = "matched"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [16]byte
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
