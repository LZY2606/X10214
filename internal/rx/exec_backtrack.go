package rx

// backtrack unwinds the choice stack until an alternative can run.
func (e *Executor) backtrack() {
	m := e.M
	for len(m.Frames) > 0 {
		top := len(m.Frames) - 1
		f := m.Frames[top]
		if f.Kind == FLook {
			// The handler manages the frame itself (candidate stepping or
			// popping), including capture restoration.
			cont := e.handleLookFrame(f)
			if cont {
				return
			}
			continue
		}
		// Always undo capture writes back to the frame's save depth.
		e.restoreDepth(f.CapDepth)
		m.Frames = m.Frames[:top]

		switch f.Kind {
		case FAlt:
			ev := Event{Kind: EvRollback, NodeID: f.NodeID, Pos: f.Pos,
				BytePos: e.Idx.BytePos(f.Pos), Detail: "take alternative"}
			e.emit(ev)
			m.PC = f.PC
			m.Pos = f.Pos
			m.StartPos = f.StartPos
			return
		case FQChoice:
			ev := Event{Kind: EvRollback, NodeID: f.NodeID, Pos: f.Pos,
				BytePos: e.Idx.BytePos(f.Pos), Detail: "quantifier stop/continue switch"}
			e.emit(ev)
			m.PC = f.PC
			m.Pos = f.Pos
			m.StartPos = f.StartPos
			return
		case FQGuard:
			ev := Event{Kind: EvPrune, NodeID: f.NodeID, Pos: f.Pos,
				BytePos: e.Idx.BytePos(f.Pos), Detail: "below minimum; mandatory iteration fails"}
			e.emit(ev)
			continue
		}
	}
	// No choices at this start position: try the next search position.
	if m.Cfg.Mode == ModeSearch && m.StartPos < e.Idx.Len() {
		m.StartPos++
		m.Pos = m.StartPos
		m.PC = 0
		m.Frames = nil
		// Restore all captures for a fresh attempt (undo should already be
		// empty after unwinding, but be explicit).
		e.restoreDepth(0)
		ev := Event{Kind: EvSearch, Pos: m.StartPos, BytePos: e.Idx.BytePos(m.StartPos), Detail: "retry at next position"}
		e.emit(ev)
		return
	}
	m.Status = StatusNoMatch
	m.StopReason = "exhausted"
}

func (e *Executor) restoreDepth(depth int) {
	m := e.M
	for len(m.Undo) > depth {
		u := m.Undo[len(m.Undo)-1]
		m.Undo = m.Undo[:len(m.Undo)-1]
		m.Caps[u.Group] = Cap{Start: u.Start, End: u.End, Set: u.Set}
		ev := Event{Kind: EvRollback, Group: u.Group, Pos: m.Pos,
			BytePos: e.Idx.BytePos(m.Pos), Detail: "restore capture"}
		if name := e.groupName(u.Group); name != "" {
			ev.Name = name
		}
		e.emit(ev)
	}
}
