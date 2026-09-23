package engine

import "fmt"

// backtrack unwinds one failed attempt and restores the captured groups that
// belonged to it. Captures made by attempts that ultimately withdraw are never
// observable after the restore.
func (vm *VM) backtrack(detail string) bool {
	vm.record(EvBacktrack, vm.pc, vm.pos, detail, nil)
	for len(vm.choices) > 0 {
		top := vm.choices[len(vm.choices)-1]
		vm.choices = vm.choices[:len(vm.choices)-1]
		vm.trimGuards()

		switch top.Kind {
		case "search":
			_, size := vm.input.RuneAt(top.Pos)
			if size <= 0 {
				vm.status = StatusNoMatch
				vm.record(EvFail, top.Pc, top.Pos, "no match in the whole input", nil)
				return false
			}
			vm.pos = top.Pos + size
			vm.caps = top.Caps
			vm.guards = nil
			vm.pc = top.Pc
			vm.record(EvAdvance, top.Pc, vm.pos, fmt.Sprintf("search from %d", vm.pos), nil)
			return vm.keepRunning()
		case "assert":
			if vm.handleAssertRetry(top.AssertRef) {
				return vm.keepRunning()
			}
			continue
		default:
			vm.pos = top.Pos
			vm.caps = top.Caps
			if top.Guard {
				vm.markGuardConsumed(top.Pc, top.Pos)
			}
			vm.pc = top.AltPc
			vm.record(EvTryBranch, top.Pc, top.Pos, "retry alternative "+vm.labelAt(top.Pc), nil)
			return vm.keepRunning()
		}
	}
	vm.status = StatusNoMatch
	vm.record(EvFail, vm.pc, vm.pos, "all alternatives exhausted", nil)
	return false
}

func (vm *VM) markGuardConsumed(pc, pos int) {
	for i := range vm.guards {
		if vm.guards[i].Pc == pc && vm.guards[i].Pos == pos {
			vm.guards[i].Consumed = true
		}
	}
}

func (vm *VM) labelAt(pc int) string {
	if pc >= 0 && pc < len(vm.prog.Insts) {
		return vm.prog.Insts[pc].Label
	}
	return ""
}

func (vm *VM) failAll(detail string) bool {
	vm.record(EvFail, vm.pc, vm.pos, detail, nil)
	vm.status = StatusNoMatch
	return false
}

// doLook enters a lookaround. Lookbehind probes rune boundaries nearest the
// assertion position first, which is the documented branch order.
func (vm *VM) doLook(inst Inst) bool {
	af := AssertFrame{
		Negative: inst.A == 1,
		Behind:   inst.B == 1,
		HomePos:  vm.pos,
		ResumePc: vm.pc + 1,
		BodyPc:   inst.C,
		MaxLen:   inst.ClassID,
		TryPos:   vm.pos,
		Caps:     cloneCaps(vm.caps),
		Depth:    len(vm.choices),
	}
	if af.Behind {
		af.TryPos = vm.firstBehindCandidate(vm.pos, af.MaxLen)
		if af.TryPos < 0 {
			// No admissible candidate at all.
			if af.Negative {
				vm.record(EvAssertDone, vm.pc, vm.pos, inst.Label+" vacuously succeeds", nil)
				vm.pc++
				return vm.keepRunning()
			}
			return vm.backtrack(inst.Label + " has no candidate start")
		}
	}
	idx := len(vm.asserts)
	vm.asserts = append(vm.asserts, af)
	snap := vm.takeSnapshot()
	if snap == nil {
		return false
	}
	vm.choices = append(vm.choices, ChoiceFrame{
		Pc: vm.pc, Pos: af.TryPos, Caps: snap, Kind: "assert", AssertRef: idx,
	})
	vm.record(EvAssertEnter, vm.pc, af.TryPos, inst.Label, nil)
	vm.pos = af.TryPos
	vm.pc = inst.C
	return vm.keepRunning()
}

func (vm *VM) firstBehindCandidate(home, maxLen int) int {
	if home == 0 {
		return -1
	}
	p, used := home, 0
	for p > 0 {
		p--
		used++
		for p > 0 && !utf8RuneStart(vm.input.Text[p]) {
			p--
			used++
		}
		if maxLen >= 0 && used > maxLen {
			return -1
		}
		return p
	}
	return -1
}

// handleAssertRetry is invoked when the marker frame of an assertion body is
// popped: the body failed. It returns true if control transferred (new
// candidate or negative success), false if outer backtracking must continue.
func (vm *VM) handleAssertRetry(idx int) bool {
	if idx >= len(vm.asserts) {
		return false
	}
	af := &vm.asserts[idx]
	// Drop assert frames nested inside this one and any frames above its depth.
	if len(vm.choices) > af.Depth {
		vm.choices = vm.choices[:af.Depth]
	}
	for len(vm.asserts) > idx {
		vm.asserts = vm.asserts[:len(vm.asserts)-1]
	}
	vm.trimGuards()

	if af.Behind {
		if cand := vm.nextBehindCandidate(af); cand >= 0 {
			af2 := *af
			af2.TryPos = cand
			newIdx := len(vm.asserts)
			vm.asserts = append(vm.asserts, af2)
			snap := vm.takeSnapshot()
			if snap == nil {
				return false
			}
			vm.choices = append(vm.choices, ChoiceFrame{
				Pc: af.BodyPc - 1, Pos: cand, Caps: snap, Kind: "assert", AssertRef: newIdx,
			})
			vm.caps = cloneCaps(af.Caps)
			vm.pos = cand
			vm.pc = af.BodyPc
			vm.record(EvAssertFail, af.BodyPc-1, cand, "lookbehind retry at earlier start", nil)
			return true
		}
	}
	return vm.assertionGaveUp(af)
}

func (vm *VM) nextBehindCandidate(af *AssertFrame) int {
	next := af.TryPos
	for next > 0 {
		next--
		for next > 0 && !utf8RuneStart(vm.input.Text[next]) {
			next--
		}
		if af.MaxLen >= 0 && af.HomePos-next > af.MaxLen {
			return -1
		}
		return next
	}
	return -1
}

// assertionGaveUp resolves "all candidates failed": a negative assertion then
// succeeds at its home position; a positive one withdraws and outer
// backtracking resumes.
func (vm *VM) assertionGaveUp(af *AssertFrame) bool {
	vm.pos = af.HomePos
	vm.caps = cloneCaps(af.Caps)
	if af.Negative {
		vm.record(EvAssertDone, 0, af.HomePos, "negative assertion succeeds (body never matched)", nil)
		vm.pc = af.ResumePc
		return true
	}
	return vm.backtrackFrom(af.Depth, "positive assertion had no successful candidate")
}

// backtrackFrom continues unwinding as if the frames above depth were gone.
func (vm *VM) backtrackFrom(depth int, detail string) bool {
	if len(vm.choices) > depth {
		vm.choices = vm.choices[:depth]
	}
	vm.trimGuards()
	return vm.backtrack(detail)
}

func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }

// resolveAssert runs when a body reaches its AssertEnd.
func (vm *VM) resolveAssert(negative bool) bool {
	if len(vm.asserts) == 0 {
		return vm.failAll("assertEnd without assertion frame")
	}
	idx := len(vm.asserts) - 1
	af := &vm.asserts[idx]
	// Discard the marker frame and everything the body pushed above it.
	if len(vm.choices) > af.Depth {
		vm.choices = vm.choices[:af.Depth]
	}
	vm.asserts = vm.asserts[:len(vm.asserts)-1]
	vm.trimGuards()

	if negative {
		vm.pos = af.HomePos
		vm.caps = cloneCaps(af.Caps)
		vm.record(EvAssertFail, 0, af.HomePos, "negative assertion fails (body matched)", nil)
		return vm.backtrack("negative lookaround matched")
	}
	vm.record(EvAssertDone, 0, af.HomePos, "positive assertion succeeds", vm.captureView())
	vm.pos = af.HomePos
	vm.pc = af.ResumePc
	return vm.keepRunning()
}

func (vm *VM) doAdvance() bool {
	return vm.backtrack("advance terminal")
}

func (vm *VM) finishMatch() bool {
	start, end := 0, vm.pos
	if s0 := vm.caps[0]; s0 >= 0 {
		start = s0
	}
	vm.caps[0] = start
	vm.caps[1] = end
	vm.status = StatusMatched
	vm.record(EvMatch, vm.pc, vm.pos, fmt.Sprintf("match [%d,%d)", start, end), vm.captureView())
	return false
}

func (vm *VM) matchBackref(group int, ci bool) bool {
	s, e := vm.caps[2*group], vm.caps[2*group+1]
	if s < 0 || e < 0 {
		return true
	}
	if vm.pos+(e-s) > vm.input.Len() {
		return false
	}
	off := 0
	for off < e-s {
		want, wsize := decodeRune(vm.input.Text[s+off:])
		got, gsize := vm.input.RuneAt(vm.pos + off)
		if gsize <= 0 || (want != got && !(ci && foldEqual(want, got))) {
			return false
		}
		off += wsize
	}
	vm.record(EvConsume, vm.pc, vm.pos, fmt.Sprintf("backref %d matches %q", group, vm.input.Text[s:e]), nil)
	vm.pos += e - s
	return true
}

func (vm *VM) captureView() map[string][2]int {
	out := map[string][2]int{}
	for i := 0; i < len(vm.prog.Groups); i++ {
		s, e := vm.caps[2*i], vm.caps[2*i+1]
		if s >= 0 && e >= 0 {
			key := fmt.Sprintf("%d", i)
			if i > 0 && vm.prog.Groups[i].Name != "" {
				key = vm.prog.Groups[i].Name
			}
			out[key] = [2]int{s, e}
		}
	}
	return out
}

// Run executes microsteps until match / no-match / paused.
func (vm *VM) Run() *MatchResult {
	for vm.Step() {
	}
	return vm.Result()
}

// Result finalizes the outcome. An indeterminate run carries the frontier and
// must never be reported as no-match.
func (vm *VM) Result() *MatchResult {
	res := &MatchResult{
		Status:         vm.status,
		Reason:         vm.reason,
		Steps:          vm.steps,
		States:         vm.states,
		Snapshots:      vm.snaps,
		SyntaxVersion:  SyntaxVersion,
		UnicodeVersion: UnicodeVersion,
	}
	if vm.status == StatusMatched {
		res.Match = true
		res.Start, res.End = vm.caps[0], vm.caps[1]
		res.RuneStart = vm.input.RuneIndex(res.Start)
		res.RuneEnd = vm.input.RuneIndex(res.RuneEndSafe())
		res.Captures = make([]Capture, len(vm.prog.Groups))
		for i, g := range vm.prog.Groups {
			s, e := vm.caps[2*i], vm.caps[2*i+1]
			c := Capture{Index: g.Index, Name: g.Name, Start: s, End: e}
			if s >= 0 && e >= 0 {
				c.Text = vm.input.Slice(s, e)
			}
			res.Captures[i] = c
		}
	}
	return res
}

func (vm *VM) RuneEndSafe() int { return vm.pos }
