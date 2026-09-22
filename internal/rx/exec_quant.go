package rx

// Quantifier frame protocol
//
// At every visit to a loop head we know how many successful iterations are
// committed (Count). The head decides between running another body and
// stopping; the rejected option is recorded as a frame:
//
//   FQChoice = "stop with Count iterations" (kept by greedy-more and lazy-stop)
//   FQGuard  = mandatory iteration marker (below minimum). On unwind it
//              propagates failure; it is never taken as an alternative.
//
// QRepeat (body succeeded) pops the body's dangling choice frames together
// with the head frame, increments Count and jumps back to the head. If the
// body made no positional progress, the loop terminates immediately while
// retaining that empty iteration's capture writes.

func (e *Executor) pushFrame(f Frame) bool {
	e.M.States++
	if e.M.Budget.MaxStates > 0 && e.M.States > e.M.Budget.MaxStates {
		e.pauseBudget("states")
		return false
	}
	e.M.Frames = append(e.M.Frames, f)
	return true
}

func (e *Executor) execQEnter(ins *Instr) {
	e.M.PC++
}

func (e *Executor) execQGuard(ins *Instr) {
	m := e.M
	count := e.qcount(ins.QID)
	canStop := count >= ins.Min
	canMore := ins.Max < 0 || count < ins.Max

	ev := e.event(EvQuant, ins)
	ev.Detail = "head"
	e.emit(ev)

	head := Frame{Pos: m.Pos, CapDepth: len(m.Undo), QID: ins.QID, Count: count,
		StartPos: m.StartPos, LastPos: -1, NodeID: ins.NodeID}

	if !ins.Lazy {
		// Greedy: try more first, keep stop as a frame when allowed.
		if !canMore {
			m.PC = ins.Target
			return
		}
		if canStop {
			f := head
			f.Kind = FQChoice
			f.PC = ins.Target
			if !e.pushFrame(f) {
				return
			}
		} else {
			f := head
			f.Kind = FQGuard
			f.PC = ins.Target
			if !e.pushFrame(f) {
				return
			}
		}
		m.PC = ins.Target2
		return
	}

	// Lazy: stop first when allowed, keep "more" as FQChoice frame.
	if canStop {
		f := head
		f.Kind = FQChoice
		f.PC = ins.Target2
		if !e.pushFrame(f) {
			return
		}
		m.PC = ins.Target
		return
	}
	if !canMore {
		m.PC = ins.Target
		return
	}
	f := head
	f.Kind = FQGuard
	f.PC = ins.Target
	if !e.pushFrame(f) {
		return
	}
	m.PC = ins.Target2
}

func (e *Executor) execQRepeat(ins *Instr) {
	m := e.M
	top := e.findTopFrame(ins.QID)
	if top < 0 {
		// Defensive: no frame means zero-width guard already consumed.
		m.PC = ins.Target
		return
	}
	gf := m.Frames[top]
	if gf.Pos == m.Pos {
		// Empty successful iteration: terminate per zero-width rule. The
		// empty iteration's own capture writes are retained; alternatives
		// opened while evaluating it are discarded.
		e.pruneAbove(top, "zero-width iteration prune")
		m.Frames[top].Count++
		ev := e.event(EvPrune, ins)
		ev.Detail = "zero-width loop terminated"
		e.emit(ev)
		after := e.Prog.Code[ins.Target].Target
		m.PC = after
		return
	}
	// Commit this iteration: discard alternatives opened inside the body but
	// keep the head frame so its iteration count survives to the next head.
	e.pruneAbove(top, "quantifier commit prune")
	m.Frames[top].Count++
	ev := e.event(EvQuant, ins)
	ev.Detail = "commit-iteration"
	e.emit(ev)
	_ = gf
	m.PC = ins.Target
}

// qcount returns the committed count of the innermost frame for qid.
func (e *Executor) qcount(qid int) int {
	i := e.findTopFrame(qid)
	if i < 0 {
		return 0
	}
	return e.M.Frames[i].Count
}

func (e *Executor) findTopFrame(qid int) int {
	for i := len(e.M.Frames) - 1; i >= 0; i-- {
		f := e.M.Frames[i]
		if f.Kind == FQChoice || f.Kind == FQGuard {
			if f.QID == qid {
				return i
			}
		}
	}
	return -1
}

func (e *Executor) pruneAbove(top int, detail string) {
	m := e.M
	for len(m.Frames) > top+1 {
		f := m.Frames[len(m.Frames)-1]
		ev := Event{Kind: EvPrune, NodeID: f.NodeID, Pos: m.Pos, BytePos: e.Idx.BytePos(m.Pos), Detail: detail}
		e.emit(ev)
		m.Frames = m.Frames[:len(m.Frames)-1]
	}
}
