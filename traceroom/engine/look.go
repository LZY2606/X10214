package engine

import (
	"traceroom/compile"
)

// Lookaround execution model
//
// An OpLook snapshots the *entire* enclosing machine context (program id, pc,
// pos, alternative stack, caps, loops) into an active LookFrame and executes
// the subprogram in place. On success/failure the outer context is restored,
// so nested lookaround works and capture rollback is exact.
//
//   - lookahead: one sub evaluation at the current position.
//   - lookbehind: end positions nearest-first (offset 0,1,2,...), giving
//     deterministic behavior for variable-width patterns.
//
// Positive assertions commit the sub evaluation's captures; negative
// assertions discard them.

// outerContext captures the enclosing machine state before entering a look.
type outerContext struct {
	progID int
	pc     int
	pos    int
	stack  []Frame
	caps   []int
	loops  []LoopState
}

func (m *machine) snapshotOuter() outerContext {
	id := -1
	if m.active != nil {
		id = m.active.SubID
	}
	return outerContext{
		progID: id,
		pc:     m.pc,
		pos:    m.pos,
		stack:  append([]Frame(nil), m.stack...),
		caps:   append([]int(nil), m.caps...),
		loops:  append([]LoopState(nil), m.loops...),
	}
}

func (m *machine) enterLook(in compile.Instr) stepResult {
	sub := m.prog.Subs[in.Arg]
	if sub == nil {
		return m.fail("missing-look-sub")
	}
	negative := in.Arg2&compile.LookNegative != 0
	behind := in.Arg2&compile.LookBehind != 0

	outer := m.snapshotOuter()
	m.emit(EvLookEnter, map[string]any{"subId": in.Arg, "behind": behind, "negative": negative})

	lf := &LookFrame{
		SubID:      in.Arg,
		OuterSubID: outer.progID,
		OuterPC:    outer.pc + 1,
		OuterPos:   outer.pos,
		OuterStack: outer.stack,
		Caps:       outer.caps,
		Loops:      outer.loops,
		Behind:     behind,
		Negative:   negative,
		StartPos:   outer.pos,
	}

	if behind {
		// nearest-first: push far..near so the nearest end-aligned candidate
		// runs first. Only rune boundaries are legal starts.
		for cand := outer.pos; cand >= 0; cand-- {
			if m.r.input.RuneStart[cand] {
				lf.Stack = append(lf.Stack, Frame{Kind: FrameAlt, PC: 0, Pos: cand, LookID: in.Arg})
			}
		}
		return m.activateNextBehind(lf)
	}
	return m.activateLook(lf, sub, 0, outer.pos)
}

// outerProgram resolves the program for an outer context id.
func (r *runner) outerProgram(id int) *compile.Program {
	if id < 0 {
		return r.prog
	}
	return r.prog.Subs[id]
}

func (m *machine) activateLook(lf *LookFrame, sub *compile.Program, pc, pos int) stepResult {
	m.active = lf
	lf.PC = pc
	lf.Pos = pos
	m.prog = sub
	m.pc = pc
	m.pos = pos
	m.caps = append([]int(nil), lf.Caps...)
	m.loops = append([]LoopState(nil), lf.Loops...)
	m.stack = nil
	return srContinue
}

func (m *machine) activateNextBehind(lf *LookFrame) stepResult {
	for len(lf.Stack) > 0 {
		cand := lf.Stack[len(lf.Stack)-1]
		lf.Stack = lf.Stack[:len(lf.Stack)-1]
		sub := m.r.findSub(lf.SubID)
		if sub == nil {
			continue
		}
		return m.activateLook(lf, sub, 0, cand.Pos)
	}
	return m.resolveLook(lf, false, nil)
}

// stepLook drives the active sub-machine by one sub instruction.
func (m *machine) stepLook() stepResult {
	lf := m.active
	res := m.execInstr()
	if res == srContinue || res == srPause || res == srBudget {
		// mirror sub context onto the active frame for serialization
		lf.PC = m.pc
		lf.Pos = m.pos
		return res
	}
	if res == srMatch {
		subCaps := append([]int(nil), m.caps...)
		if lf.Behind && m.pos != lf.OuterPos {
			// matched but did not end exactly at the outer position
			return m.activateNextBehind(lf)
		}
		return m.resolveLook(lf, true, subCaps)
	}
	if lf.Behind {
		return m.activateNextBehind(lf)
	}
	return m.resolveLook(lf, false, nil)
}

// resolveLook restores the outer machine and applies polarity.
func (m *machine) resolveLook(lf *LookFrame, matched bool, subCaps []int) stepResult {
	positive := !lf.Negative
	success := matched == positive
	outerProg := m.r.outerProgram(lf.OuterSubID)

	m.emit(EvLookExit, map[string]any{
		"subId": lf.SubID, "matched": matched, "success": success, "behind": lf.Behind,
	})

	m.active = nil
	m.prog = outerProg
	m.pc = lf.OuterPC
	m.pos = lf.OuterPos
	m.stack = lf.OuterStack
	m.loops = lf.Loops

	if success && matched && subCaps != nil {
		m.commitCaps(subCaps, "look")
	} else {
		m.caps = lf.Caps
	}

	if !success {
		return m.fail("lookaround")
	}
	return srContinue
}

func (m *machine) commitCaps(newCaps []int, why string) {
	old := append([]int(nil), m.caps...)
	changed := false
	for i := range m.caps {
		if m.caps[i] != newCaps[i] {
			changed = true
		}
	}
	m.caps = newCaps
	if changed {
		m.r.noteCapsDiff(old, newCaps, why)
	}
}
