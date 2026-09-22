package engine

import (
	"traceroom/compile"
	"traceroom/syntax"
)

// Run executes until match / no-match / budget / pause.
func (vm *VM) Run() *Result {
	vm.steps = vm.r.steps
	vm.snapshots = vm.r.snapshots
	vm.events = vm.r.events
	vm.pruned = nil

	if vm.cfg.StartTo == 0 {
		vm.cfg.StartTo = len(vm.input.Bytes)
	}
	if vm.cur == nil {
		vm.upcoming = vm.planStarts()
		if len(vm.upcoming) == 0 {
			return vm.finishNoMatch()
		}
		vm.startPos = vm.upcoming[0]
		vm.upcoming = vm.upcoming[1:]
		vm.beginAttempt(vm.startPos)
	}

	stepBudget := vm.cfg.StepLimit
	used := 0
	for {
		if vm.cur == nil {
			if len(vm.upcoming) == 0 {
				return vm.finishNoMatch()
			}
			vm.startPos = vm.upcoming[0]
			vm.upcoming = vm.upcoming[1:]
			vm.beginAttempt(vm.startPos)
		}

		// state budget: live states = stack depth + active machine
		if vm.cfg.MaxStates > 0 && len(vm.cur.stack)+1 > vm.cfg.MaxStates {
			return vm.finishIndeterminate(LimitStates, "state budget exceeded")
		}

		res := vm.cur.step()
		used++
		vm.steps = vm.r.steps
		vm.snapshots = vm.r.snapshots
		vm.events = vm.r.events

		switch res {
		case srMatch:
			return vm.finishMatch()
		case srFail:
			vm.cur = nil
		case srBudget:
			return vm.finishIndeterminate(vm.r.reason, "execution budget exhausted")
		case srPause:
			vm.paused = true
			return vm.finishPaused()
		}

		if stepBudget > 0 && used >= stepBudget {
			return vm.finishPaused()
		}
	}
}

// planStarts enumerates legal leftmost-start byte offsets (rune boundaries).
func (vm *VM) planStarts() []int {
	from := vm.cfg.StartFrom
	to := len(vm.input.Bytes)
	if vm.cfg.StartTo > 0 && vm.cfg.StartTo < to {
		to = vm.cfg.StartTo
	}
	if vm.cfg.Anchored || vm.cfg.FullMatch {
		return []int{0}
	}
	var out []int
	for p := from; p <= to; p++ {
		if vm.input.RuneStart[p] {
			out = append(out, p)
		}
	}
	return out
}

func (vm *VM) beginAttempt(pos int) {
	vm.r.events = append(vm.r.events, Event{
		Seq: len(vm.r.events), Kind: EvStartAttempt,
		Pos: vm.input.PosOf(pos), Detail: map[string]any{"byte": pos, "cp": vm.input.CP(pos)},
	})
	caps := initialCaps(vm.prog.NumCaps)
	caps[0] = pos
	m := vm.r.newMachine(vm.prog, pos, caps)
	vm.cur = m
}

func (vm *VM) finishMatch() *Result {
	m := vm.cur
	// set group 0 end
	if len(m.caps) >= 2 {
		m.caps[1] = m.pos
	}
	vm.r.events = append(vm.r.events, Event{
		Seq: len(vm.r.events), Kind: EvDone, Pos: vm.input.PosOf(m.pos),
		Detail: map[string]any{"match": true},
	})
	caps := vm.resolveCaps(m)
	return &Result{
		Status:         StatusMatch,
		Match:          true,
		Caps:           caps,
		Start:          vm.input.PosOf(m.caps[0]),
		End:            vm.input.PosOf(m.pos),
		Events:         vm.r.events,
		Steps:          vm.r.steps,
		Snapshots:      vm.r.snapshots,
		SyntaxVersion:  syntax.Version,
		UnicodeVersion: UnicodeDataVersion,
		ActiveStates:   len(m.stack) + 1,
		Budget:         vm.budgetInfo(),
	}
}

func (vm *VM) finishNoMatch() *Result {
	vm.r.events = append(vm.r.events, Event{
		Seq: len(vm.r.events), Kind: EvDone, Pos: vm.input.PosOf(0),
		Detail: map[string]any{"match": false},
	})
	return &Result{
		Status:         StatusNoMatch,
		Match:          false,
		Events:         vm.r.events,
		Steps:          vm.r.steps,
		Snapshots:      vm.r.snapshots,
		SyntaxVersion:  syntax.Version,
		UnicodeVersion: UnicodeDataVersion,
		Budget:         vm.budgetInfo(),
	}
}

func (vm *VM) finishIndeterminate(reason LimitReason, why string) *Result {
	f := vm.buildFrontier()
	vm.r.events = append(vm.r.events, Event{
		Seq:    len(vm.r.events),
		Kind:   EvPause,
		Pos:    vm.input.PosOf(vm.cur.pos),
		Detail: map[string]any{"budget": string(reason), "why": why, "indeterminate": true},
	})
	return &Result{
		Status:         StatusIndeterminate,
		Reason:         reason,
		Match:          false,
		Frontier:       f,
		Events:         vm.r.events,
		Steps:          vm.r.steps,
		Snapshots:      vm.r.snapshots,
		SyntaxVersion:  syntax.Version,
		UnicodeVersion: UnicodeDataVersion,
		ActiveStates:   len(vm.cur.stack) + 1,
		Budget:         vm.budgetInfo(),
	}
}

func (vm *VM) finishPaused() *Result {
	f := vm.buildFrontier()
	return &Result{
		Status:         StatusPaused,
		Frontier:       f,
		Events:         vm.r.events,
		Steps:          vm.r.steps,
		Snapshots:      vm.r.snapshots,
		SyntaxVersion:  syntax.Version,
		UnicodeVersion: UnicodeDataVersion,
		ActiveStates:   len(vm.cur.stack) + 1,
		Budget:         vm.budgetInfo(),
	}
}

func (vm *VM) budgetInfo() BudgetInfo {
	return BudgetInfo{
		MaxSteps:     vm.cfg.MaxSteps,
		MaxStates:    vm.cfg.MaxStates,
		MaxSnapshots: vm.cfg.MaxSnapshots,
	}
}

func (vm *VM) buildFrontier() *Frontier {
	m := vm.cur
	ms := Machine{
		PC:    m.pc,
		Pos:   m.pos,
		Caps:  append([]int(nil), m.caps...),
		Loops: append([]LoopState(nil), m.loops...),
		Stack: append([]Frame(nil), m.stack...),
	}
	if m.active != nil {
		a := *m.active
		ms.Active = &a
		ms.Active.OuterStack = append([]Frame(nil), a.OuterStack...)
		ms.Active.Stack = append([]Frame(nil), a.Stack...)
	}
	return &Frontier{
		Machine:    &ms,
		NextStart:  vm.startPos,
		StartScans: append([]int(nil), vm.upcoming...),
		Config:     vm.cfg,
	}
}

func (vm *VM) resolveCaps(m *machine) []Capture {
	out := make([]Capture, vm.prog.NumCaps+1)
	for g := 0; g <= vm.prog.NumCaps; g++ {
		s, e := m.caps[2*g], m.caps[2*g+1]
		c := Capture{Index: g, Start: s, End: e, Name: vm.prog.CapNames[g], Participated: s >= 0 && e >= 0}
		if s >= 0 && e >= 0 && e >= s && e <= len(vm.input.Bytes) {
			c.Text = string(vm.input.Bytes[s:e])
		}
		if s < 0 {
			c.Start, c.End = -1, -1
		}
		out[g] = c
	}
	return out
}

// Resume continues execution from a frontier using the same program.
func Resume(prog *compile.Program, input string, f *Frontier, extraSteps int) *Result {
	vm := NewVM(prog, input, f.Config)
	if extraSteps > 0 {
		vm.cfg.StepLimit = extraSteps
	}
	vm.upcoming = append([]int(nil), f.StartScans...)
	if f.Machine != nil {
		m := vm.restoreMachine(f.Machine)
		vm.cur = m
		vm.startPos = f.NextStart
	}
	return vm.Run()
}

func (vm *VM) restoreMachine(ms *Machine) *machine {
	m := &machine{
		r:     vm.r,
		prog:  vm.prog,
		pc:    ms.PC,
		pos:   ms.Pos,
		caps:  append([]int(nil), ms.Caps...),
		loops: append([]LoopState(nil), ms.Loops...),
		stack: append([]Frame(nil), ms.Stack...),
	}
	if ms.Active != nil {
		a := *ms.Active
		m.active = &a
		sub := vm.r.findSub(a.SubID)
		if sub != nil {
			m.prog = sub
		}
	}
	return m
}
