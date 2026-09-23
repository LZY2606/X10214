package regex

import (
	"fmt"
	"unicode/utf8"
)

func (vm *VM) execBackref(op Op) {
	capv := vm.state.Caps[op.Group]
	if capv.Start < 0 || capv.End < 0 {
		vm.fail(op.NodeID, fmt.Sprintf("group %d not captured", op.Group))
		return
	}
	want := vm.input.Bytes[capv.Start:capv.End]
	if vm.state.Pos+len(want) > len(vm.input.Bytes) || string(vm.input.Bytes[vm.state.Pos:vm.state.Pos+len(want)]) != string(want) {
		vm.fail(op.NodeID, fmt.Sprintf("backreference to group %d mismatch", op.Group))
		return
	}
	vm.emitEvent("consume", op.NodeID, vm.state.Pos, fmt.Sprintf("backreference group %d", op.Group), vm.state.Caps)
	vm.state.Pos += len(want)
	vm.state.PC++
}

func (vm *VM) anchorMatch(kind rune) bool {
	p := vm.state.Pos
	n := len(vm.input.Bytes)
	atLineStart := func() bool { return p == 0 || (p <= n && vm.input.Bytes[p-1] == '\n') }
	atLineEnd := func() bool { return p == n || (p < n && vm.input.Bytes[p] == '\n') }
	switch kind {
	case '^':
		return atLineStart()
	case '$':
		return atLineEnd()
	case 'A':
		return p == 0
	case 'z':
		return p == n
	case 'b':
		return atWordBoundary(vm.input, p)
	case 'B':
		return !atWordBoundary(vm.input, p)
	}
	return false
}

func atWordBoundary(in Input, pos int) bool {
	before, after := false, false
	if pos > 0 {
		r, size := utf8.DecodeLastRune(in.Bytes[:pos])
		if r == utf8.RuneError {
			before = isWordRune(-1)
		} else {
			before = isWordRune(r)
			_ = size
		}
	}
	if pos < len(in.Bytes) {
		r, _ := in.Decode(pos)
		after = isWordRune(r)
	}
	return before != after
}

func (vm *VM) execAssertStart(op Op) {
	marker := len(vm.state.Choices)
	call := Call{Negative: op.Negative, OuterPos: vm.state.Pos, AfterPC: op.B, MarkerChoice: marker, Width: op.Width}
	if op.Kind == OpLookahead {
		call.Kind = callLA
	} else {
		call.Kind = callLB
	}
	if op.Kind == OpLookbehind {
		if op.Width > vm.state.Pos {
			vm.fail(op.NodeID, "lookbehind before input start")
			return
		}
		call.OuterPos = vm.state.Pos
		vm.state.Pos -= op.Width
	}
	vm.state.Calls = append(cloneCalls(vm.state.Calls), call)
	vm.state.Caps = cloneCaps(vm.state.Caps)
	vm.used.Snapshots++
	kindName := "lookahead"
	if op.Kind == OpLookbehind {
		kindName = "lookbehind"
	}
	if op.Negative {
		kindName = "negative " + kindName
	}
	vm.emitEvent("assert_enter", op.NodeID, vm.state.Pos, kindName+" starts", vm.state.Caps)
	vm.state.PC = op.A
}

func (vm *VM) execAssertEnd(op Op) {
	if len(vm.state.Calls) == 0 {
		vm.pause("assertion end without frame")
		return
	}
	call := vm.state.Calls[len(vm.state.Calls)-1]
	if op.Kind == OpLBEnd {
		if vm.state.Pos != call.OuterPos {
			vm.pause("lookbehind ended at wrong position")
			return
		}
		vm.state.Pos = call.OuterPos
	}
	if !call.Negative {
		vm.state.Calls = vm.state.Calls[:len(vm.state.Calls)-1]
		vm.emitEvent("assert_match", op.NodeID, call.OuterPos, "positive assertion accepted; captures retained", vm.state.Caps)
		vm.state.Pos = call.AfterPC
		vm.state.Pos = call.OuterPos
		vm.state.PC = call.AfterPC
		return
	}
	caps := cloneCaps(vm.state.Caps)
	vm.used.Snapshots++
	if !vm.pushChoice(Choice{Kind: choiceAssert, PC: call.AfterPC, Pos: call.OuterPos, Caps: caps, Negative: true, AfterPC: call.AfterPC, NodeID: op.NodeID}) {
		return
	}
	vm.state.Calls = vm.state.Calls[:len(vm.state.Calls)-1]
	vm.emitEvent("branch", op.NodeID, call.OuterPos, "negative assertion child unexpectedly matched; prune outer branch and retain continuation", vm.state.Caps)
	vm.fail(op.NodeID, "negative assertion failed because child matched")
}

func (vm *VM) fail(nodeID int, detail string) {
	vm.emitEvent("fail", nodeID, vm.state.Pos, detail, vm.state.Caps)
	for len(vm.state.Choices) > 0 {
		ch := vm.state.Choices[len(vm.state.Choices)-1]
		vm.state.Choices = vm.state.Choices[:len(vm.state.Choices)-1]
		vm.emitPruned(ch)
		if len(vm.state.Calls) > 0 && ch.Kind == choiceAssert {
			call := vm.state.Calls[len(vm.state.Calls)-1]
			if ch.Negative {
				if !call.Negative {
					vm.state.Caps = cloneCaps(ch.Caps)
					vm.used.Snapshots++
					vm.state.Calls = vm.state.Calls[:len(vm.state.Calls)-1]
					vm.emitEvent("assert_match", ch.NodeID, ch.Pos, "negative assertion accepted; child captures rolled back", vm.state.Caps)
					vm.state.Pos = ch.Pos
					vm.state.PC = ch.AfterPC
					return
				}
				vm.state.Caps = cloneCaps(ch.Caps)
				vm.used.Snapshots++
				vm.state.Pos = ch.Pos
				vm.state.PC = ch.AfterPC
				vm.emitEvent("branch", ch.NodeID, ch.Pos, "negative assertion matched; continuation retained", vm.state.Caps)
				continue
			}
			vm.state.Pos = ch.Pos
			vm.state.PC = ch.PC
			vm.emitEvent("assert_fail", ch.NodeID, ch.Pos, "positive assertion backtracked", vm.state.Caps)
			continue
		}
		vm.restoreChoice(ch)
		return
	}
	vm.state.PC = -1
}

func (vm *VM) restoreChoice(ch Choice) {
	vm.state.Pos = ch.Pos
	vm.state.PC = ch.PC
	vm.state.Caps = cloneCaps(ch.Caps)
	vm.used.Snapshots++
	vm.state.Calls = cloneCalls(ch.Calls)
	vm.state.Loops = cloneLoops(ch.Loops)
	if ch.Kind == choiceLoop {
		if ch.PC == ch.EndPC {
			vm.state.Loops = append(vm.state.Loops, LoopFrame{StartPC: ch.StartPC, Count: ch.Count, BodyStart: ch.BodyStart, LastEnd: ch.Pos})
		} else {
			if len(vm.state.Loops) > 0 {
				vm.state.Loops = vm.state.Loops[:len(vm.state.Loops)-1]
			}
		}
	}
	vm.emitEvent("restore", ch.NodeID, ch.Pos, "restored capture snapshot and ordered frontier", vm.state.Caps)
}

func (vm *VM) pushChoice(ch Choice) bool {
	ch.Pos = vm.state.Pos
	ch.Caps = cloneCaps(vm.state.Caps)
	vm.used.Snapshots++
	ch.Calls = cloneCalls(vm.state.Calls)
	ch.Loops = cloneLoops(vm.state.Loops)
	vm.state.Choices = append(vm.state.Choices, ch)
	return true
}

func (vm *VM) emitPruned(ch Choice) {
	kind := "alternative"
	if ch.Kind == choiceLoop {
		kind = "repetition"
	}
	if ch.Kind == choiceAssert {
		kind = "assertion continuation"
	}
	ev := TraceEvent{Step: vm.used.Steps, Type: "prune", NodeID: ch.NodeID, PC: ch.PC, Pos: ch.Pos, Detail: kind + " branch consumed and discarded", CodePoint: vm.input.ByteToCodePoint(ch.Pos), Caps: cloneCaps(ch.Caps), Usage: vm.used, Pruned: &ch}
	vm.appendEvent(ev)
}

func (vm *VM) emitEvent(kind string, nodeID, pos int, detail string, caps []Capture) {
	cp := -1
	if pos >= 0 && pos <= len(vm.input.Bytes) {
		cp = vm.input.ByteToCodePoint(pos)
	}
	vm.appendEvent(TraceEvent{Step: vm.used.Steps, Type: kind, NodeID: nodeID, PC: vm.state.PC, Pos: pos, Detail: detail, CodePoint: cp, Caps: cloneCaps(caps), Usage: vm.used})
}

func (vm *VM) appendEvent(ev TraceEvent) {
	if len(vm.events) < vm.limits.Events {
		vm.events = append(vm.events, ev)
	}
}
