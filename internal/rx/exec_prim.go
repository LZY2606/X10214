package rx

import "unicode"

func (e *Executor) advance(ins *Instr) {
	m := e.M
	ev := e.event(EvConsume, ins)
	if r, ok := e.curRune(); ok {
		ev.Rune = string(r)
	}
	e.emit(ev)
	m.Pos++
	m.PC++
}

func (e *Executor) failf(ins *Instr, detail string) {
	ev := e.event(EvFail, ins)
	ev.Detail = detail
	e.emit(ev)
	e.backtrack()
}

func (e *Executor) execChar(ins *Instr) {
	r, ok := e.curRune()
	ev := e.event(EvTry, ins)
	ev.Rune = ifRune(r, ok)
	e.emit(ev)
	if !ok || !foldEqual(ins.Fold, r, ins.Rune) {
		e.failf(ins, "char mismatch")
		return
	}
	e.advance(ins)
}

func ifRune(r rune, ok bool) string {
	if !ok {
		return ""
	}
	return string(r)
}

func (e *Executor) execClass(ins *Instr) {
	r, ok := e.curRune()
	ev := e.event(EvTry, ins)
	ev.Rune = ifRune(r, ok)
	e.emit(ev)
	if !ok {
		e.failf(ins, "end of input")
		return
	}
	if !MatchCharClass(ins.Class, r, ins.Fold) {
		e.failf(ins, "class mismatch")
		return
	}
	e.advance(ins)
}

func (e *Executor) execDot(ins *Instr) {
	r, ok := e.curRune()
	ev := e.event(EvTry, ins)
	ev.Rune = ifRune(r, ok)
	e.emit(ev)
	if !ok {
		e.failf(ins, "end of input")
		return
	}
	if r == '\n' && !ins.DotAll {
		e.failf(ins, "dot does not cross newline")
		return
	}
	if r == utf8RuneError() && !ins.DotAll {
		// invalid byte: dot matches it like any single element
	}
	e.advance(ins)
}

func utf8RuneError() rune { return unicode.ReplacementChar }

func (e *Executor) execSave(ins *Instr) {
	m := e.M
	m.Snapshots++
	if m.Budget.MaxSnapshots > 0 && m.Snapshots > m.Budget.MaxSnapshots {
		e.pauseBudget("snapshots")
		return
	}
	old := m.Caps[ins.Group]
	m.Undo = append(m.Undo, Undo{Group: ins.Group, Start: old.Start, End: old.End, Set: old.Set})
	switch ins.Edge {
	case EdgeStart:
		m.Caps[ins.Group] = Cap{Start: m.Pos, End: -1}
	case EdgeEnd:
		c := m.Caps[ins.Group]
		c.End = m.Pos
		c.Set = true
		m.Caps[ins.Group] = c
	}
	ev := e.event(EvSave, ins)
	ev.Group = ins.Group
	if g := e.groupName(ins.Group); g != "" {
		ev.Name = g
	}
	c := m.Caps[ins.Group]
	ev.Start, ev.End = c.Start, c.End
	ev.Text = e.Idx.Slice(max0(c.Start), max0(c.End))
	e.emit(ev)
	m.PC++
}

func max0(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

func (e *Executor) groupName(idx int) string {
	if idx >= 0 && idx < len(e.Prog.Groups) {
		return e.Prog.Groups[idx].Name
	}
	return ""
}

func (e *Executor) execMatch(ins *Instr) {
	m := e.M
	if m.Cfg.Mode == ModeFull || e.insideFullLook() {
		// Inside a lookbehind body, opMatch means the child consumed the
		// exact candidate span and must end at the stored boundary.
		if f := e.topLook(); f != nil {
			if f.Behind && m.Pos != f.LookPos {
				e.failf(ins, "lookbehind candidate does not end at boundary")
				return
			}
		}
		if m.Cfg.Mode == ModeFull && m.Pos != e.Idx.Len() {
			e.failf(ins, "fullmatch did not consume input")
			return
		}
	}
	m.HasMatch = true
	m.MatchStart = m.StartPos
	m.MatchEnd = m.Pos
	m.Caps[0] = Cap{Start: m.StartPos, End: m.Pos, Set: true}
	m.Status = StatusMatch
	m.StopReason = "matched"
}


func (e *Executor) topLook() *Frame {
	for i := len(e.M.Frames) - 1; i >= 0; i-- {
		if e.M.Frames[i].Kind == FLook {
			return &e.M.Frames[i]
		}
		if e.M.Frames[i].Kind == FAlt || e.M.Frames[i].Kind == FQChoice || e.M.Frames[i].Kind == FQGuard {
			// same execution region; keep scanning
			continue
		}
	}
	return nil
}

func (e *Executor) insideFullLook() bool {
	f := e.topLook()
	return f != nil && f.Behind
}

func (e *Executor) execSplit(ins *Instr) {
	m := e.M
	m.States++
	if m.Budget.MaxStates > 0 && m.States > m.Budget.MaxStates {
		e.pauseBudget("states")
		return
	}
	m.Frames = append(m.Frames, Frame{
		Kind: FAlt, PC: ins.Target, Pos: m.Pos, StartPos: m.StartPos,
		CapDepth: len(m.Undo), NodeID: ins.NodeID,
	})
	ev := e.event(EvBranch, ins)
	ev.Detail = "push alternative"
	e.emit(ev)
	m.PC++
}
