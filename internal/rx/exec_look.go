package rx

// Lookaround frame protocol
//
// FLook fields for lookahead:
//   Pos/LookPos = outer position; child runs at Pos and must reach its
//   opMatch with normal (unbounded) consumption.
//
// FLook fields for lookbehind:
//   LookPos = the fixed end boundary
//   CandStart iterates candidate START positions, nearest-first (from
//   LookPos-1 downward; the empty start LookPos is tried last)
//   StartPos holds the inner pattern's search start, always 0.
// The inner pattern is run left-to-right from 0 and required to end exactly
// at LookPos. This matches documented variable-length lookbehind semantics
// where the longest/nearest candidate wins.

func (e *Executor) execLookStart(ins *Instr) {
	m := e.M
	e.M.States++
	if e.M.Budget.MaxStates > 0 && e.M.States > e.M.Budget.MaxStates {
		e.pauseBudget("states")
		return
	}
	if !ins.Behind {
		m.Frames = append(m.Frames, Frame{
			Kind: FLook, PC: ins.ChildEnd + 1, Pos: m.Pos, LookPos: m.Pos,
			CapDepth: len(m.Undo), NegLook: ins.Negate, Behind: false,
			NodeID: ins.NodeID,
		})
		ev := e.event(EvLookEnter, ins)
		ev.Detail = lookName(ins)
		e.emit(ev)
		m.PC++
		return
	}

	// Candidate START positions are tried nearest-first: LookPos (empty),
	// LookPos-1, LookPos-2, ... bounded by MaxBehind.
	cand := m.Pos
	minStart := m.Pos - e.M.Cfg.MaxBehind
	if minStart < 0 {
		minStart = 0
	}
	m.Frames = append(m.Frames, Frame{
		Kind: FLook, PC: ins.ChildEnd + 1, Pos: m.Pos, LookPos: m.Pos,
		CapDepth: len(m.Undo), NegLook: ins.Negate, Behind: true,
		CandStart: cand, CandStep: -1, CandCount: cand - minStart + 1,
		StartPos: cand, NodeID: ins.NodeID,
	})
	m.StartPos = cand
	m.Pos = cand
	ev := e.event(EvLookEnter, ins)
	ev.Detail = lookName(ins)
	e.emit(ev)
	m.PC++
}

func lookName(ins *Instr) string {
	if ins.Behind {
		if ins.Negate {
			return "negative-lookbehind"
		}
		return "positive-lookbehind"
	}
	if ins.Negate {
		return "negative-lookahead"
	}
	return "positive-lookahead"
}

func (e *Executor) execLookEnd(ins *Instr) {
	m := e.M
	top := e.findLookFrame()
	if top < 0 {
		e.failf(ins, "missing look frame")
		return
	}
	// Inner branches may leave FAlt/FQ frames above FLook even on a fully
	// successful child evaluation (e.g. alternation continuations). They
	// describe alternatives inside the child, which are now moot; prune
	// them and roll their capture writes back before applying semantics.
	e.pruneLookInterior(top, ins)
	f := m.Frames[top]
	ev := e.event(EvLookExit, ins)
	ev.Detail = lookName(ins)

	if !f.Behind {
		if !f.NegLook {
			ev.Detail += ": satisfied (captures preserved)"
			e.emit(ev)
			m.Frames = m.Frames[:top]
			m.Pos = f.LookPos
			m.PC = f.PC
			return
		}
		ev.Detail += ": matched but negative (captures rolled back)"
		e.emit(ev)
		e.restoreDepth(f.CapDepth)
		m.Pos = f.LookPos
		e.failf(ins, "negative lookahead matched")
		return
	}

	// Lookbehind: inner execution must end exactly at LookPos.
	if m.Pos != f.LookPos {
		e.behindTryNext(top, ins, f)
		return
	}
	if !f.NegLook {
		ev.Detail += ": satisfied (captures preserved)"
		e.emit(ev)
		m.Frames = m.Frames[:top]
		m.Pos = f.LookPos
		m.StartPos = f.LookPos
		m.PC = f.PC
		return
	}
	ev.Detail += ": matched but negative"
	e.emit(ev)
	e.behindTryNext(top, ins, f)
}

func (e *Executor) behindTryNext(top int, ins *Instr, f Frame) {
	m := e.M
	e.restoreDepth(f.CapDepth)
	next := f.CandStart - 1
	lowest := f.LookPos - (f.CandCount - 1)
	if next >= lowest {
		m.Frames[top].CandStart = next
		m.Frames[top].StartPos = next
		m.StartPos = next
		m.Pos = next
		m.PC = e.lookBodyPC(f)
		ev := Event{Kind: EvLookEnter, NodeID: f.NodeID, Pos: next,
			BytePos: e.Idx.BytePos(next), Detail: "lookbehind next candidate"}
		e.emit(ev)
		return
	}
	// Candidates exhausted.
	m.Frames = m.Frames[:top]
	if f.NegLook {
		ev := Event{Kind: EvLookExit, NodeID: f.NodeID, Pos: f.LookPos,
			BytePos: e.Idx.BytePos(f.LookPos), Detail: "negative-lookbehind satisfied (no candidate matched)"}
		e.emit(ev)
		m.Pos = f.LookPos
		m.StartPos = f.LookPos
		m.PC = f.PC
		return
	}
	m.Pos = f.LookPos
	m.StartPos = f.LookPos
	e.failf(ins, "lookbehind exhausted")
}

func (e *Executor) handleLookFrame(f Frame) bool {
	m := e.M
	if !f.Behind {
		topL := len(m.Frames) - 1
		if f.NegLook {
			e.restoreDepth(f.CapDepth)
			m.Frames = m.Frames[:topL]
			ev := Event{Kind: EvLookExit, NodeID: f.NodeID, Pos: f.LookPos,
				BytePos: e.Idx.BytePos(f.LookPos), Detail: "negative satisfied by failure"}
			e.emit(ev)
			m.Pos = f.LookPos
			m.PC = f.PC
			return true
		}
		e.restoreDepth(f.CapDepth)
		m.Frames = m.Frames[:topL]
		m.Pos = f.LookPos
		ev := Event{Kind: EvFail, NodeID: f.NodeID, Pos: f.LookPos,
			BytePos: e.Idx.BytePos(f.LookPos), Detail: "lookahead failed"}
		e.emit(ev)
		return false
	}

	// Lookbehind inner failure: try the next candidate. The frame is still
	// on the stack (backtrack did not pop FLook).
	top := len(m.Frames) - 1
	next := f.CandStart - 1
	lowest := f.LookPos - (f.CandCount - 1)
	e.restoreDepth(f.CapDepth)
	if next >= lowest {
		m.Frames[top].CandStart = next
		m.Frames[top].StartPos = next
		m.StartPos = next
		m.Pos = next
		m.PC = e.lookBodyPC(f)
		ev := Event{Kind: EvLookEnter, NodeID: f.NodeID, Pos: next,
			BytePos: e.Idx.BytePos(next), Detail: "lookbehind candidate"}
		e.emit(ev)
		return true
	}
	if f.NegLook {
		m.Frames = m.Frames[:top]
		ev := Event{Kind: EvLookExit, NodeID: f.NodeID, Pos: f.LookPos,
			BytePos: e.Idx.BytePos(f.LookPos), Detail: "negative-lookbehind satisfied (no candidate matched)"}
		e.emit(ev)
		m.Pos = f.LookPos
		m.StartPos = f.LookPos
		m.PC = f.PC
		return true
	}
	m.Frames = m.Frames[:top]
	ev := Event{Kind: EvFail, NodeID: f.NodeID, Pos: f.LookPos,
		BytePos: e.Idx.BytePos(f.LookPos), Detail: "lookbehind exhausted"}
	e.emit(ev)
	return false
}


func (e *Executor) findLookFrame() int {
	for i := len(e.M.Frames) - 1; i >= 0; i-- {
		if e.M.Frames[i].Kind == FLook {
			return i
		}
	}
	return -1
}

func (e *Executor) pruneLookInterior(top int, ins *Instr) {
	m := e.M
	for len(m.Frames) > top+1 {
		f := m.Frames[len(m.Frames)-1]
		e.restoreDepth(f.CapDepth)
		ev := Event{Kind: EvPrune, NodeID: ins.NodeID, Pos: m.Pos,
			BytePos: e.Idx.BytePos(m.Pos), Detail: "look child complete; prune interior alternatives"}
		e.emit(ev)
		m.Frames = m.Frames[:len(m.Frames)-1]
	}
}

func (e *Executor) lookBodyPC(f Frame) int {
	end := f.PC - 1
	return e.findLookStart(end) + 1
}

func (e *Executor) findLookStart(endIdx int) int {
	for i := endIdx; i >= 0; i-- {
		if e.Prog.Code[i].Op == opLookStart && e.Prog.Code[i].ChildEnd == endIdx {
			return i
		}
	}
	return endIdx
}
