package vm

import "fmt"

func (r *Run) topRepeat(loopID int) *Frame {
	t := r.Thread
	for i := len(t.Stack) - 1; i >= 0; i-- {
		if t.Stack[i].Kind == FrameRepeat && t.Stack[i].LoopID == loopID {
			return &t.Stack[i]
		}
	}
	return nil
}

// pruneLoopChoices removes loop-tagged choice frames above the repeat frame;
// these become dead branches when a zero-width repeat forces termination.
func (r *Run) pruneLoopChoices(loopID int, why string) {
	t := r.Thread
	top := len(t.Stack)
	rfIdx := -1
	for i := len(t.Stack) - 1; i >= 0; i-- {
		if t.Stack[i].Kind == FrameRepeat && t.Stack[i].LoopID == loopID {
			rfIdx = i
			break
		}
	}
	kept := t.Stack[:0]
	for i := 0; i < top; i++ {
		f := t.Stack[i]
		if i > rfIdx && f.Kind == FrameChoice && f.LoopID == loopID {
			r.log(Event{Kind: EvPrune, Pc: f.Pc, Pos: f.Pos,
				Detail: fmt.Sprintf("prune loop %d alternative: %s", loopID, why)})
			continue
		}
		kept = append(kept, f)
	}
	t.Stack = kept
}

func (r *Run) doRepeatBegin(ins Instr) {
	t := r.Thread
	t.push(Frame{Kind: FrameRepeat, LoopID: ins.A, Count: 0, LoopPos: t.Pos,
		Min: ins.B, Max: ins.C})
	r.log(Event{Kind: EvRepeat, Pc: t.Pc, NID: ins.NID, Pos: t.Pos,
		Detail: fmt.Sprintf("loop %d begin min=%d max=%d greedy=%v at %d",
			ins.A, ins.B, ins.C, ins.Greedy, t.Pos)})
	t.Pc++
}

func (r *Run) doRepeatPost(ins Instr) {
	t := r.Thread
	rf := r.topRepeat(ins.A)
	if rf != nil {
		rf.Count++
	}
	r.log(Event{Kind: EvRepeat, Pc: t.Pc, NID: ins.NID, Pos: t.Pos,
		Detail: fmt.Sprintf("loop %d completed iteration count=%d pos=%d", ins.A, rf.Count, t.Pos)})
	t.Pc++
}

// doRepeatAfter enforces the minimum and tears down loop state.
func (r *Run) doRepeatAfter(ins Instr) bool {
	t := r.Thread
	rf := r.topRepeat(ins.A)
	count := 0
	if rf != nil {
		count = rf.Count
	}
	if count < ins.B {
		r.log(Event{Kind: EvRepeat, Pc: t.Pc, NID: ins.NID, Pos: t.Pos,
			Detail: fmt.Sprintf("loop %d stop with count %d < min %d", ins.A, count, ins.B)})
		return false
	}
	// Drop the repeat counter frame and any of its tagged choice frames
	// that are still above it (fully-explored stop alternatives).
	rfIdx := -1
	for i := len(t.Stack) - 1; i >= 0; i-- {
		if t.Stack[i].Kind == FrameRepeat && t.Stack[i].LoopID == ins.A {
			rfIdx = i
			break
		}
	}
	kept := t.Stack[:0]
	for i, f := range t.Stack {
		if i == rfIdx {
			continue
		}
		if rfIdx >= 0 && i > rfIdx && f.Kind == FrameChoice && f.LoopID == ins.A {
			r.log(Event{Kind: EvPrune, Pc: f.Pc, Pos: f.Pos,
				Detail: fmt.Sprintf("prune exhausted loop %d alternative", ins.A)})
			continue
		}
		kept = append(kept, f)
	}
	t.Stack = kept
	r.log(Event{Kind: EvRepeat, Pc: t.Pc, NID: ins.NID, Pos: t.Pos,
		Detail: fmt.Sprintf("loop %d finished count=%d", ins.A, count)})
	t.Pc++
	return true
}
