package engine

import "fmt"

// RunOption controls a single run/step call.
type RunOption struct {
	BreakPC  map[int]bool
	BreakNode map[int]bool
	MaxEvents int // 0 = unlimited within budget
}

// Start initializes the frontier for a fresh run.
func (e *Engine) Start() {
	t := e.initialThread()
	e.threads = []*Thread{t}
	e.active = 0
	e.assignID(t)
	e.emit(Event{Type: "start", RunePos: 0, PC: 0, Text: "start", ThreadID: e.threadID[t]})
}

func (e *Engine) assignID(t *Thread) int {
	if id, ok := e.threadID[t]; ok {
		return id
	}
	id := e.tid
	e.tid++
	e.threadID[t] = id
	return id
}

func (e *Engine) current() *Thread {
	if e.active < 0 || e.active >= len(e.threads) {
		return nil
	}
	return e.threads[e.active]
}

// Step executes until one observable boundary: an entering event is emitted,
// a status finalizes, or a breakpoint fires. Returns the current status
// ("halted" when the run is still resumable).
func (e *Engine) Step(opt RunOption) Status {
	for {
		t := e.current()
		if t == nil {
			return e.finish()
		}
		if !e.consumeBudget() {
			return StatusIndeterminate
		}
		ins := &e.prog.Code[t.PC]
		cont, status := e.exec(t, ins, opt)
		if status != "" {
			return status
		}
		if !cont {
			// thread failed; pop to next continuation
			e.killCurrent("fail")
			continue
		}
		if e.halt {
			e.halt = false
			return StatusHalted
		}
		return StatusHalted
	}
}

func (e *Engine) consumeBudget() bool {
	e.steps++
	if e.bud.MaxSteps > 0 && e.steps > e.bud.MaxSteps {
		e.haltReason = "step budget exceeded"
		return false
	}
	if e.bud.MaxStates > 0 && e.states > e.bud.MaxStates {
		e.haltReason = "state budget exceeded"
		return false
	}
	return true
}

// exec executes one instruction. Returns (continueThread, terminalStatus).
func (e *Engine) exec(t *Thread, ins *Inst, opt RunOption) (bool, Status) {
	id := e.assignID(t)
	switch ins.Op {
	case OpChar:
		e.enter(t, ins, id)
		if t.Pos >= len(e.in.Slots) || !matchChar(ins.Match, ins.Flags, e.in.Slots[t.Pos]) {
			e.emit(Event{Type: "fail", PC: t.PC, Node: ins.Node, RunePos: t.Pos, Text: "no match", ThreadID: id, Reason: "char"})
			return false, ""
		}
		e.emit(Event{Type: "advance", PC: t.PC, Node: ins.Node, RunePos: t.Pos, Text: string(e.in.Slots[t.Pos].R), ThreadID: id})
		t.Pos++
		t.PC++
		e.checkBreak(ins, opt)
		return true, ""
	case OpDot:
		e.enter(t, ins, id)
		if t.Pos >= len(e.in.Slots) {
			e.emit(Event{Type: "fail", PC: t.PC, Node: ins.Node, RunePos: t.Pos, Text: "eof", ThreadID: id, Reason: "dot"})
			return false, ""
		}
		r := e.in.Slots[t.Pos].R
		if r == '\n' && !flagOn(ins.Flags, 's') {
			e.emit(Event{Type: "fail", PC: t.PC, Node: ins.Node, RunePos: t.Pos, Text: "newline", ThreadID: id, Reason: "dot"})
			return false, ""
		}
		e.emit(Event{Type: "advance", PC: t.PC, Node: ins.Node, RunePos: t.Pos, Text: string(r), ThreadID: id})
		t.Pos++
		t.PC++
		e.checkBreak(ins, opt)
		return true, ""
	case OpAnchor:
		e.enter(t, ins, id)
		if !e.matchAnchor(ins.Kind, ins.Flags, t.Pos) {
			e.emit(Event{Type: "fail", PC: t.PC, Node: ins.Node, RunePos: t.Pos, Text: ins.Kind, ThreadID: id, Reason: "anchor"})
			return false, ""
		}
		e.emit(Event{Type: "anchor", PC: t.PC, Node: ins.Node, RunePos: t.Pos, Text: ins.Kind, ThreadID: id})
		t.PC++
		e.checkBreak(ins, opt)
		return true, ""
	case OpSave:
		return e.execSave(t, ins, opt, id)
	case OpSplit:
		return e.execSplit(t, ins, id)
	case OpJmp:
		t.PC = ins.Arg
		return true, ""
	case OpRReset:
		t.Loops = t.Loops.set(ins.Arg, loopState{start: t.Pos, count: 0})
		t.PC++
		return true, ""
	case OpRPhase:
		return e.execRPhase(t, ins, id)
	case OpRAgain:
		return e.execRAgain(t, ins, id)
	case OpBackref:
		return e.execBackref(t, ins, id)
	case OpLAOpen:
		return e.execLAOpen(t, ins, id)
	case OpLAClose:
		return e.execLAClose(t, ins, id)
	case OpLABound:
		return e.execLABound(t, ins, id)
	case OpLBOpen:
		return e.execLBOpen(t, ins, id)
	case OpLBClose:
		return e.execLBClose(t, ins, id)
	case OpMatch:
		return e.execMatch(t, ins, id)
	}
	panic(fmt.Sprintf("unknown op %s", ins.Op))
}

func (e *Engine) enter(t *Thread, ins *Inst, id int) {
	e.emit(Event{Type: "enter", PC: t.PC, Node: ins.Node, RunePos: t.Pos, ThreadID: id})
}

func (e *Engine) checkBreak(ins *Inst, opt RunOption) {
	if opt.BreakPC != nil && opt.BreakPC[ins.Arg] {
		e.halt = true
	}
}
