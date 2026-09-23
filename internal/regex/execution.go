package regex

import (
	"fmt"
)

const (
	callLA = iota + 1
	callLB
)

func (vm *VM) resetSearch(start int) {
	caps := make([]Capture, vm.program.Groups+1)
	for i := range caps {
		caps[i] = Capture{-1, -1}
	}
	vm.state = State{PC: 0, Pos: start, SearchStart: start, Caps: caps}
	vm.emitEvent("search", 0, start, "start anchored search", nil)
}

func (vm *VM) run() {
	for {
		if vm.reason != "" {
			vm.emitEvent("pause", vm.state.PC, vm.state.Pos, vm.reason, nil)
			return
		}
		if vm.state.PC < 0 {
			vm.advanceSearchOrFail()
			continue
		}
		if vm.state.PC >= len(vm.program.Ops) {
			vm.state.PC = -1
			continue
		}
		op := vm.program.Ops[vm.state.PC]
		if op.Kind == OpMatch {
			vm.emitEvent("match", op.NodeID, vm.state.Pos, "thread reached match", vm.state.Caps)
			return
		}
		vm.used.StatesPeak = max(vm.used.StatesPeak, len(vm.state.Choices)+1)
		if len(vm.state.Choices)+1 > vm.limits.States {
			vm.pause("state budget exhausted; frontier retains ordered branches")
			continue
		}
		vm.used.Steps++
		if vm.used.Steps > vm.limits.Steps {
			vm.pause("step budget exhausted; frontier can resume")
			continue
		}
		activeBefore := len(vm.state.Choices)
		vm.execute(op)
		if len(vm.state.Choices) > activeBefore && vm.used.Snapshots > vm.limits.Snapshots {
			vm.pause("capture snapshot budget exhausted; frontier can resume")
		}
	}
}

func (vm *VM) pause(reason string) {
	if vm.reason == "" {
		vm.reason = reason
	}
}

func (vm *VM) advanceSearchOrFail() {
	next := vm.state.SearchStart + 1
	if next > len(vm.input.Bytes) {
		vm.state.Pos = -1
		vm.reason = ""
		vm.emitEvent("no_match", 0, -1, "all ordered branches and starts exhausted", nil)
		return
	}
	vm.resetSearch(next)
}

func (vm *VM) execute(op Op) {
	switch op.Kind {
	case OpJmp:
		vm.state.PC = op.Target
	case OpChar:
		r, size := vm.input.Decode(vm.state.Pos)
		if r >= 0 && r == op.Rune {
			vm.consume(op, size)
		} else {
			vm.fail(op.NodeID, "literal mismatch")
		}
	case OpAny:
		r, size := vm.input.Decode(vm.state.Pos)
		if r >= 0 && r != '\n' {
			vm.consume(op, size)
		} else {
			vm.fail(op.NodeID, "any-char mismatch")
		}
	case OpClass:
		r, size := vm.input.Decode(vm.state.Pos)
		if size > 0 && classMatches(r, op.Class) {
			vm.consume(op, size)
		} else {
			vm.fail(op.NodeID, "character class mismatch")
		}
	case OpBackref:
		vm.execBackref(op)
	case OpAnchor:
		if vm.anchorMatch(op.Rune) {
			vm.emitEvent("anchor", op.NodeID, vm.state.Pos, "anchor accepted", vm.state.Caps)
			vm.state.PC++
		} else {
			vm.fail(op.NodeID, "anchor mismatch")
		}
	case OpSave:
		vm.execSave(op)
	case OpSplit:
		vm.execSplit(op)
	case OpLookahead, OpLookbehind:
		vm.execAssertStart(op)
	case OpAssertEnd, OpLBEnd:
		vm.execAssertEnd(op)
	case OpLoopStart:
		vm.state.Loops = append(cloneLoops(vm.state.Loops), LoopFrame{StartPC: op.Target, BodyStart: vm.state.PC + 1, LastEnd: vm.state.Pos})
		vm.state.PC++
	case OpLoopEnd:
		vm.execLoopEnd(op)
	default:
		vm.pause(fmt.Sprintf("unknown opcode %d", op.Kind))
	}
}

func (vm *VM) consume(op Op, size int) {
	vm.emitEvent("consume", op.NodeID, vm.state.Pos, fmt.Sprintf("consume %d byte(s)", size), vm.state.Caps)
	vm.state.Pos += size
	vm.state.PC++
}

func (vm *VM) execSave(op Op) {
	vm.state.Caps = cloneCaps(vm.state.Caps)
	vm.used.Snapshots++
	if op.SaveEnd {
		vm.state.Caps[op.Group].End = vm.state.Pos
	} else {
		vm.state.Caps[op.Group].Start = vm.state.Pos
		vm.state.Caps[op.Group].End = -1
	}
	what := "start"
	if op.SaveEnd {
		what = "end"
	}
	vm.emitEvent("capture", op.NodeID, vm.state.Pos, fmt.Sprintf("group %d %s", op.Group, what), vm.state.Caps)
	vm.state.PC++
}

func (vm *VM) execSplit(op Op) {
	if !vm.pushChoice(Choice{Kind: choiceAlt, PC: op.B, NodeID: op.NodeID}) {
		return
	}
	vm.emitEvent("branch", op.NodeID, vm.state.Pos, "priority branch selected; alternative retained in documented order", vm.state.Caps)
	vm.state.PC = op.A
}

func (vm *VM) execLoopEnd(op Op) {
	if len(vm.state.Loops) == 0 {
		vm.pause("loop end without frame")
		return
	}
	frame := &vm.state.Loops[len(vm.state.Loops)-1]
	empty := vm.state.Pos == frame.LastEnd
	frame.Count++
	if empty {
		if frame.Count < op.Min {
			vm.fail(op.NodeID, "zero-width repetition cannot satisfy minimum")
			return
		}
		vm.emitEvent("zero_width_stop", op.NodeID, vm.state.Pos, "zero-width repetition terminated", vm.state.Caps)
		vm.exitLoop(op)
		return
	}
	frame.LastEnd = vm.state.Pos
	if frame.Count < op.Min {
		vm.state.PC = frame.BodyStart
		return
	}
	canRepeat := !op.HasMax || frame.Count < op.Max
	if op.Greedy {
		if canRepeat {
			if !vm.pushChoice(Choice{Kind: choiceLoop, PC: op.EndPC, StartPC: frame.StartPC, EndPC: op.EndPC, Count: frame.Count, NodeID: op.NodeID}) {
				return
			}
			vm.emitEvent("branch", op.NodeID, vm.state.Pos, "greedy repetition continues; shorter match retained", vm.state.Caps)
			vm.state.PC = frame.BodyStart
		} else {
			vm.exitLoop(op)
		}
	} else {
		if canRepeat {
			if !vm.pushChoice(Choice{Kind: choiceLoop, PC: frame.BodyStart, StartPC: frame.StartPC, EndPC: op.EndPC, Count: frame.Count, NodeID: op.NodeID}) {
				return
			}
			vm.emitEvent("branch", op.NodeID, vm.state.Pos, "lazy repetition exits; longer match retained", vm.state.Caps)
			vm.exitLoop(op)
		} else {
			vm.exitLoop(op)
		}
	}
}

func (vm *VM) exitLoop(op Op) {
	if len(vm.state.Loops) == 0 {
		vm.pause("loop exit without frame")
		return
	}
	vm.state.Loops = vm.state.Loops[:len(vm.state.Loops)-1]
	vm.state.PC = op.EndPC
}
