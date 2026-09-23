package engine

// enterLook suspends the outer thread and starts the first lookaround
// candidate from the LAMark instruction.
func (c *cursor) enterLook(ins Instr) {
	c.states++
	c.snaps++
	st := &markerState{
		lookID: ins.LookID, body: ins.Body, cont: ins.Cont,
		anchorPos: c.pos, neg: ins.Neg, ahead: ins.Ahead, maxW: ins.MaxW,
		capBefore: cloneInts(c.cap), pathLenBefore: len(c.path),
	}
	c.alts = append(c.alts, alt{isMarker: true, marker: st, pc: c.pc, pos: c.pos,
		cap: cloneInts(c.cap), pathLen: len(c.path)})
	// Outer path continues virtually into the marker; the body path is tracked
	// separately from here.
	c.startAttempt(st)
}

func (c *cursor) startAttempt(st *markerState) {
	var start int
	if st.ahead {
		start = st.anchorPos
	} else {
		start = st.nextStart
	}
	if !st.emittedEnter {
		e := c.emitAt(EvEnterLook, st.body, start)
		e.LookID = st.lookID
		e.Neg = st.neg
		e.Ahead = st.ahead
		e.LookStart = start
		e.ByteStart = c.in.BytePos(start)
		e.CPS = c.in.CPPos(start)
		st.emittedEnter = true
	}
	// Isolated inside-look path.
	c.path = c.path[:st.pathLenBefore]
	c.pathPush(st.body, start)
	c.cap = cloneInts(st.capBefore)
	c.pc, c.pos = st.body, start
}

// finishLook handles an OpMatch belonging to a lookaround body.
func (c *cursor) finishLook(ins Instr) bool {
	st := c.activeMarker()
	if st == nil || st.lookID != ins.LookID {
		c.finishFail()
		return false
	}
	consumed := c.pos - st.anchorPos
	ok := true
	if !st.ahead && c.pos != st.anchorPos {
		ok = false
	}
	if ok {
		e := c.emitAt(EvExitLook, ins.Pc, c.pos)
		e.LookID = st.lookID
		e.Neg = st.neg
		e.Ahead = st.ahead
		e.LookStart = st.anchorPos
		if !st.neg {
			// Positive: captures produced inside are committed outward.
			e.Note = "committed"
			st.capBefore = cloneInts(c.cap)
			c.exitSuccess(st)
			return true
		}
		// Negative success: discard inner captures, continue outer.
		e.Note = "negated"
		c.cap = cloneInts(st.capBefore)
		c.path = c.path[:st.pathLenBefore]
		c.pathPush(st.cont, st.anchorPos)
		c.removeMarker(st.lookID)
		c.pc, c.pos = st.cont, st.anchorPos
		return true
	}
	// Failed requirement for this candidate.
	if !c.backtrackInside(st) {
		c.finishFail()
		return false
	}
	return true
}

func (c *cursor) activeMarker() *markerState {
	for i := len(c.alts) - 1; i >= 0; i-- {
		if c.alts[i].isMarker {
			return c.alts[i].marker
		}
	}
	return nil
}

func (c *cursor) removeMarker(id int) {
	for i := range c.alts {
		if c.alts[i].isMarker && c.alts[i].marker.lookID == id {
			c.alts = append(c.alts[:i], c.alts[i+1:]...)
			return
		}
	}
}

func (c *cursor) exitSuccess(st *markerState) {
	c.path = c.path[:st.pathLenBefore]
	c.pathPush(st.cont, st.anchorPos)
	c.removeMarker(st.lookID)
	c.pc, c.pos = st.cont, st.anchorPos
}

// backtrackInside unwinds non-marker alternatives until reaching the marker.
func (c *cursor) backtrackInside(st *markerState) bool {
	for len(c.alts) > 0 {
		top := c.alts[len(c.alts)-1]
		if top.isMarker {
			break
		}
		a := top
		c.alts = c.alts[:len(c.alts)-1]
		c.path = c.path[:st.pathLenBefore]
		// Rebuild current attempt path is not possible directly; simply
		// restore captured snapshot and resume alternative.
		c.cap = a.cap
		if !c.onPath(a.pc, a.pos) {
			c.pathPush(st.body, st.nextStartSafe())
			c.pathPush(a.pc, a.pos)
			c.pc, c.pos = a.pc, a.pos
			return true
		}
	}
	return c.advanceMarker(st)
}

func (st *markerState) nextStartSafe() int {
	if st.ahead {
		return st.anchorPos
	}
	return st.nextStart
}

// advanceMarker moves a marker to its next candidate; for a positive
// lookaround it returns false when candidates are exhausted (outer backtrack),
// while negative lookarounds invert the result.
func (c *cursor) advanceMarker(st *markerState) bool {
	// Called when an inside attempt has fully failed.
	if st.ahead {
		// Only one candidate exists for lookahead.
		return c.resolveExhausted(st)
	}
	// Behind: candidates nearest-first (shortest consumed), start grows down.
	consumed := st.anchorPos - st.nextStart
	_ = consumed
	next := st.nextStart - 1
	if st.anchorPos-next > st.maxW || next < 0 {
		return c.resolveExhausted(st)
	}
	st.nextStart = next
	st.attempts++
	c.startAttempt(st)
	return true
}

func (c *cursor) resolveExhausted(st *markerState) bool {
	if st.neg {
		// Every candidate failed => negative lookaround succeeds.
		e := c.emitAt(EvExitLook, st.cont, st.anchorPos)
		e.LookID = st.lookID
		e.Neg = true
		e.Ahead = st.ahead
		e.LookStart = st.anchorPos
		e.Note = "negated (exhausted)"
		c.cap = cloneInts(st.capBefore)
		c.path = c.path[:st.pathLenBefore]
		c.pathPush(st.cont, st.anchorPos)
		c.removeMarker(st.lookID)
		c.pc, c.pos = st.cont, st.anchorPos
		return true
	}
	// Positive exhausted: pop marker and let the outer continuation backtrack.
	c.removeMarker(st.lookID)
	c.cap = cloneInts(st.capBefore)
	c.path = c.path[:st.pathLenBefore]
	return c.backtrack()
}
