package engine

import "fmt"

// Step executes exactly one micro-transition and returns false when the run
// has reached a terminal or paused state.
func (vm *VM) Step() bool {
	if vm.status != StatusRunning {
		return false
	}
	if !vm.chargeStep() {
		return false
	}
	if !vm.chargeState() {
		return false
	}
	if vm.pc < 0 || vm.pc >= len(vm.prog.Insts) {
		return vm.failAll("program counter out of range")
	}
	inst := vm.prog.Insts[vm.pc]
	switch inst.Op {
	case OpChar:
		r, size := vm.input.RuneAt(vm.pos)
		if size > 0 && (r == inst.R || (inst.A == 1 && foldEqual(r, inst.R))) {
			vm.record(EvConsume, vm.pc, vm.pos, fmt.Sprintf("consume %q", string(r)), vm.captureView())
			vm.pos += size
			vm.pc++
			return vm.keepRunning()
		}
		return vm.backtrack("literal mismatch")
	case OpAnyChar:
		r, size := vm.input.RuneAt(vm.pos)
		if size > 0 && (inst.A == 1 || r != '\n') {
			vm.record(EvConsume, vm.pc, vm.pos, fmt.Sprintf("dot %q", string(r)), nil)
			vm.pos += size
			vm.pc++
			return vm.keepRunning()
		}
		return vm.backtrack("dot mismatch")
	case OpClass:
		r, size := vm.input.RuneAt(vm.pos)
		cc := vm.prog.Classes[inst.A]
		if size > 0 && classMatches(cc, r) {
			vm.record(EvConsume, vm.pc, vm.pos, fmt.Sprintf("class %q", string(r)), nil)
			vm.pos += size
			vm.pc++
			return vm.keepRunning()
		}
		return vm.backtrack("class mismatch")
	case OpAnchor:
		if anchorOK(inst.A, inst.B == 1, vm.input, vm.pos) {
			vm.record(EvTryBranch, vm.pc, vm.pos, "anchor holds", nil)
			vm.pc++
			return vm.keepRunning()
		}
		return vm.backtrack("anchor fails")
	case OpRef:
		if vm.matchBackref(inst.A, inst.B == 1) {
			vm.pc++
			return vm.keepRunning()
		}
		return vm.backtrack("backreference mismatch")
	case OpSave:
		prev := vm.caps[inst.A]
		vm.caps[inst.A] = vm.pos
		vm.record(EvSave, vm.pc, vm.pos, fmt.Sprintf("slot %d %d->%d (%s)", inst.A, prev, vm.pos, inst.Label), vm.captureView())
		vm.pc++
		return vm.keepRunning()
	case OpStart:
		return vm.doStart(inst)
	case OpJmp:
		vm.pc = inst.A
		return true
	case OpMatch:
		return vm.finishMatch()
	case OpFail:
		return vm.backtrack("explicit fail")
	case OpLook:
		return vm.doLook(inst)
	case OpAssertEnd:
		return vm.resolveAssert(inst.A == 1, true)
	case OpAdvance:
		return vm.doAdvance()
	default:
		return vm.failAll("unknown instruction")
	}
}

func (vm *VM) keepRunning() bool {
	return vm.status == StatusRunning
}

func (vm *VM) doStart(inst Inst) bool {
	greedy := inst.A == 1
	// Pop stale guards belonging to already abandoned choice depths.
	vm.trimGuards()
	if greedy {
		// Body first. The same (split pc, position) must not be retried in
		// one unbroken choice lineage: that is the empty-repeat termination.
		if vm.guardExists(vm.pc, vm.pos) {
			vm.record(EvPruneZW, vm.pc, vm.pos, fmt.Sprintf("zero-width repeat pruned at pc=%d", vm.pc), nil)
			// Skip the body entirely: continue at the alternative.
			vm.pc = inst.B
			return vm.keepRunning()
		}
		snap := vm.takeSnapshot()
		if snap == nil {
			return false
		}
		vm.choices = append(vm.choices, ChoiceFrame{
			Pc: vm.pc, AltPc: inst.B, Pos: vm.pos, Caps: snap,
			Kind: choiceKind(inst.Label),
		})
		vm.guards = append(vm.guards, Guard{Pc: vm.pc, Pos: vm.pos, Depth: len(vm.choices) - 1})
		vm.record(EvPushChoice, vm.pc, vm.pos, "push greedy "+inst.Label, nil)
		vm.pc++
		return vm.keepRunning()
	}
	// Lazy: alternative (skip body) first; retry body on backtrack.
	if vm.guardExists(vm.pc, vm.pos) {
		vm.record(EvPruneZW, vm.pc, vm.pos, fmt.Sprintf("zero-width lazy repeat pruned at pc=%d", vm.pc), nil)
		vm.pc = inst.B
		return vm.keepRunning()
	}
	snap := vm.takeSnapshot()
	if snap == nil {
		return false
	}
	vm.choices = append(vm.choices, ChoiceFrame{
		Pc: vm.pc, AltPc: vm.pc + 1, Pos: vm.pos, Caps: snap,
		Kind: choiceKind(inst.Label), Guard: true,
	})
	vm.guards = append(vm.guards, Guard{Pc: vm.pc, Pos: vm.pos, Depth: len(vm.choices) - 1})
	vm.record(EvPushChoice, vm.pc, vm.pos, "push lazy "+inst.Label+" (defer body)", nil)
	vm.pc = inst.B
	return vm.keepRunning()
}

func choiceKind(label string) string {
	switch label {
	case "search-start":
		return "search"
	default:
		if len(label) >= 3 && label[:3] == "alt" {
			return "alt"
		}
		return "repeat"
	}
}

func (vm *VM) guardExists(pc, pos int) bool {
	for _, g := range vm.guards {
		if g.Pc == pc && g.Pos == pos && !g.Consumed {
			return true
		}
	}
	return false
}

func (vm *VM) trimGuards() {
	for len(vm.guards) > 0 {
		g := vm.guards[len(vm.guards)-1]
		if g.Depth >= len(vm.choices) || vm.choices[g.Depth].Pc != g.Pc {
			vm.guards = vm.guards[:len(vm.guards)-1]
			continue
		}
		break
	}
}
