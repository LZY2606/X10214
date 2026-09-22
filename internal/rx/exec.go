package rx

import (
	"unicode"
)

// Executor runs a program against a machine.
type Executor struct {
	M    *Machine
	Prog *Program
	Idx  *Idx
}

// NewExecutor binds a freshly created machine to its program/input.
func NewExecutor(prog *Program, m *Machine) *Executor {
	if m.Cfg.MaxBehind == 0 {
		m.Cfg.MaxBehind = 256
	}
	return &Executor{M: m, Prog: prog, Idx: NewIdx(m.Input)}
}

func (e *Executor) event(k EventKind, ins *Instr) Event {
	ev := Event{Seq: len(e.M.Events) + 1 + e.M.Dropped, Kind: k, PC: e.M.PC, Pos: e.M.Pos, BytePos: e.Idx.BytePos(e.M.Pos)}
	if ins != nil {
		ev.NodeID = ins.NodeID
	}
	if r, ok := e.Idx.RuneAt(e.M.Pos); ok {
		ev.Rune = string(r)
	}
	if len(e.M.Events) < e.M.TrailLimit {
		e.M.Events = append(e.M.Events, ev)
	} else {
		e.M.Dropped++
	}
	return ev
}

func (e *Executor) emit(ev Event) {
	ev.Seq = len(e.M.Events) + 1 + e.M.Dropped
	if len(e.M.Events) < e.M.TrailLimit {
		e.M.Events = append(e.M.Events, ev)
	} else {
		e.M.Dropped++
	}
}

func (e *Executor) atBreakpoint(pc int) bool {
	ins := &e.Prog.Code[pc]
	for _, b := range e.M.Cfg.Breakpoints {
		if b == ins.NodeID {
			return true
		}
	}
	return false
}

// Step performs one instruction dispatch. It returns false once the machine
// reaches a terminal state (match / no-match / paused).
func (e *Executor) Step() bool {
	m := e.M
	if isTerminal(m.Status) {
		return false
	}
	if m.PC >= len(e.Prog.Code) {
		e.terminateNoMatch("pc out of range")
		return false
	}
	// Breakpoint pauses before executing the instruction. After a resume we
	// skip the exact PC that paused us once, then re-arm for later visits.
	if e.atBreakpoint(m.PC) && m.PC != m.SkipBreakPC {
		m.Status = StatusPausedBreak
		m.StopReason = "breakpoint"
		m.SkipBreakPC = m.PC
		return false
	}
	if m.PC == m.SkipBreakPC {
		m.SkipBreakPC = -1
	}
	m.Steps++
	if m.Budget.MaxSteps > 0 && m.Steps > m.Budget.MaxSteps {
		e.pauseBudget("steps")
		return false
	}
	ins := &e.Prog.Code[m.PC]
	switch ins.Op {
	case opChar:
		e.execChar(ins)
	case opClass:
		e.execClass(ins)
	case opDot:
		e.execDot(ins)
	case opSave:
		e.execSave(ins)
	case opSplit:
		e.execSplit(ins)
	case opJmp:
		m.PC = ins.Target
	case opMatch:
		e.execMatch(ins)
	case opQEnter:
		e.execQEnter(ins)
	case opQGuard:
		e.execQGuard(ins)
	case opQRepeat:
		e.execQRepeat(ins)
	case opAnchor:
		e.execAnchor(ins)
	case opBackref:
		e.execBackref(ins)
	case opLookStart:
		e.execLookStart(ins)
	case opLookEnd:
		e.execLookEnd(ins)
	}
	return !isTerminal(m.Status)
}

func isTerminal(s Status) bool {
	return s == StatusMatch || s == StatusNoMatch || s == StatusIndeterminate || s == StatusPausedBreak
}

// RunUntil advances until terminal. stopAtStep>0 stops after that many
// instructions (used for single-stepping batches).
func (e *Executor) RunUntil(stopAtStep int) Status {
	for {
		if stopAtStep > 0 && e.M.Steps >= stopAtStep {
			return e.M.Status
		}
		if !e.Step() {
			return e.M.Status
		}
	}
}

// Resume clears a pause and runs to the next pause/terminal.
func (e *Executor) Resume(bud Budget) Status {
	if e.M.Status == StatusPausedBreak {
		// SkipBreakPC already records the PC to pass; clear status only.
		e.M.Status = ""
	}
	if e.M.Status == StatusIndeterminate && bud != (Budget{}) {
		e.M.Budget = addBudget(e.M.Budget, bud)
		e.M.Status = ""
	}
	return e.RunUntil(0)
}

func addBudget(a, b Budget) Budget {
	return Budget{
		MaxSteps:     addCap(a.MaxSteps, b.MaxSteps),
		MaxStates:    addCap(a.MaxStates, b.MaxStates),
		MaxSnapshots: addCap(a.MaxSnapshots, b.MaxSnapshots),
	}
}
func addCap(a, b int) int {
	if a <= 0 || b <= 0 {
		return 0
	}
	return a + b
}

func (e *Executor) pauseBudget(which string) {
	m := e.M
	m.Status = StatusIndeterminate
	m.StopReason = "budget:" + which
	ev := e.event(EvBudget, nil)
	ev.Detail = which
	e.emit(ev)
	m.Frontier = m.StopReason
}

func (e *Executor) terminateNoMatch(reason string) {
	e.M.Status = StatusNoMatch
	e.M.StopReason = reason
}

func (e *Executor) curRune() (rune, bool) {
	return e.Idx.RuneAt(e.M.Pos)
}

func foldEqual(insFold bool, a, b rune) bool {
	if a == b {
		return true
	}
	if insFold {
		for f := unicode.SimpleFold(b); f != b; f = unicode.SimpleFold(f) {
			if f == a {
				return true
			}
		}
	}
	return false
}
