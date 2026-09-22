package vm

import "fmt"

// doSplit handles greedy and lazy quantifier choice points.
func (r *Run) doSplit(ins Instr) {
	t := r.Thread
	rf := r.topRepeat(ins.A)
	zeroWidth := rf != nil && rf.Count > 0 && rf.LoopPos == t.Pos
	atMax := rf != nil && rf.Max >= 0 && rf.Count >= rf.Max

	// The continuation for the non-preferred branch.
	altPc := t.Pc + 1
	if !ins.Greedy {
		altPc = ins.Target
	}
	prefDesc := "body"
	if !ins.Greedy {
		prefDesc = "stop"
	}

	// Zero-width repetition must terminate: take the stop branch without
	// leaving another alternative for this loop.
	if zeroWidth {
		stopPc := ins.Target
		r.pruneLoopChoices(ins.A, "zero-width repetition forced to stop")
		r.log(Event{Kind: EvRepeat, Pc: t.Pc, NID: ins.NID, Pos: t.Pos,
			Detail: fmt.Sprintf("loop %d zero-width -> stop pc %d", ins.A, stopPc)})
		t.Pc = stopPc
		return
	}
	if atMax {
		stopPc := ins.Target
		r.pruneLoopChoices(ins.A, "upper bound reached")
		r.log(Event{Kind: EvRepeat, Pc: t.Pc, NID: ins.NID, Pos: t.Pos,
			Detail: fmt.Sprintf("loop %d at max %d -> stop pc %d", ins.A, rf.Max, stopPc)})
		t.Pc = stopPc
		return
	}

	r.states++
	if r.states > r.Budgets.MaxStates {
		r.indeterminate("state", "alternative states")
		return
	}
	caps, ok := r.snapshotCaps(t.Caps)
	if !ok {
		r.indeterminate("snapshot", "split capture snapshot")
		return
	}
	t.push(Frame{
		Kind:   FrameChoice,
		Pc:     altPc,
		Pos:    t.Pos,
		LoopID: ins.A,
		Caps:   caps,
	})
	nextPc := t.Pc + 1
	if !ins.Greedy {
		nextPc = ins.Target
	}
	r.log(Event{Kind: EvSplit, Pc: t.Pc, NID: ins.NID, Pos: t.Pos,
		Detail: fmt.Sprintf("loop %d prefer %s; alternative saved at pc %d", ins.A, prefDesc, altPc)})
	t.Pc = nextPc
	if rf != nil {
		rf.LoopPos = t.Pos
	}
}

func (r *Run) doGroupBegin(ins Instr) {
	t := r.Thread
	old := t.Caps[ins.A]
	caps := t.Caps.Clone()
	r.snaps++
	caps[ins.A] = CapSlot{Begin: t.Pos, End: -1}
	r.log(Event{Kind: EvCapture, Pc: t.Pc, NID: ins.NID, Pos: t.Pos,
		Detail: fmt.Sprintf("group %s open at %d", r.capName(ins.A), t.Pos),
		Caps:   caps, CapDiff: r.capDiff(t.Caps, caps, ins.NID)})
	t.Caps = caps
	_ = old
	t.Pc++
}

func (r *Run) doGroupEnd(ins Instr) {
	t := r.Thread
	caps := t.Caps.Clone()
	r.snaps++
	caps[ins.A] = CapSlot{Begin: caps[ins.A].Begin, End: t.Pos}
	r.log(Event{Kind: EvCapture, Pc: t.Pc, NID: ins.NID, Pos: t.Pos,
		Detail: fmt.Sprintf("group %s close at %d", r.capName(ins.A), t.Pos),
		Caps:   caps, CapDiff: r.capDiff(t.Caps, caps, ins.NID)})
	t.Caps = caps
	t.Pc++
}

func (r *Run) doBackref(ins Instr) bool {
	t := r.Thread
	slot := t.Caps[ins.A]
	if slot.Begin < 0 || slot.End < 0 {
		// An unset group matches the empty string.
		r.logStep(ins, fmt.Sprintf("backref group %d unset -> empty", ins.A))
		t.Pc++
		return true
	}
	length := slot.End - slot.Begin
	if t.Pos+length > r.Input.lenRunes() {
		return false
	}
	for i := 0; i < length; i++ {
		a := r.Input.Runes[slot.Begin+i]
		b := r.Input.Runes[t.Pos+i]
		if !runeEq(a, b, false) {
			return false
		}
	}
	r.logStep(ins, fmt.Sprintf("backref group %d matched %d runes", ins.A, length))
	t.Pos += length
	t.Pc++
	return true
}

func (r *Run) doMatch(ins Instr) {
	t := r.Thread
	caps := t.Caps.Clone()
	r.snaps++
	caps[0] = CapSlot{Begin: caps[0].Begin, End: t.Pos}
	t.Caps = caps
	bLo, bHi := r.Input.ByteRange(caps[0].Begin, caps[0].End)
	m := &Match{
		RuneLo: caps[0].Begin,
		RuneHi: caps[0].End,
		ByteLo: bLo,
		ByteHi: bHi,
		Text:   r.Input.Raw[bLo:bHi],
		Caps:   caps,
	}
	r.Status = StatusMatch
	r.Match = m
	r.StepsUsed, r.StatesUsed, r.SnapsUsed = r.steps, r.states, r.snaps
	r.log(Event{Kind: EvMatch, Pc: t.Pc, NID: ins.NID, Pos: t.Pos,
		Detail: fmt.Sprintf("match runes [%d,%d) bytes [%d,%d) %q", m.RuneLo, m.RuneHi, m.ByteLo, m.ByteHi, m.Text),
		Caps:   caps})
}
