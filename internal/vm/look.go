package vm

import "fmt"

// Lookaround semantics (documented ordering):
//   - (?=...) (?!...): body executes at the current position; zero-width.
//   - (?<=...) (?<!...): candidate consumed lengths are tried in ascending
//     order starting at 0 up to the statically bounded maximum. A negative
//     lookbehind is satisfied only after every candidate length fails; a
//     match at any length fails the assertion and the search continues at the
//     next (longer) candidate, so a *longer* negative match is never missed.
// Captures produced inside a satisfied positive assertion are kept;
// captures from a satisfied negative assertion or from failed/retried
// candidates are discarded by restoring the entry snapshot.

func (r *Run) doLookPush(ins Instr) {
	t := r.Thread
	caps, ok := r.snapshotCaps(t.Caps)
	if !ok {
		r.indeterminate("snapshot", "lookaround entry snapshot")
		return
	}
	bodyStart := t.Pc + 1
	outerPos := t.Pos
	maxLen := ins.B
	if maxLen < 0 {
		maxLen = 0
	}
	if ins.Behind && outerPos < 0 {
		r.lookCandidateImpossible(Frame{LookID: ins.A, Neg: ins.Neg, Behind: true,
			MaxLen: maxLen, Pc: bodyStart, Pos: outerPos, EndPc: ins.C, Caps: caps},
			"lookbehind before input start")
		return
	}
	t.push(Frame{
		Kind: FrameLook, LookID: ins.A, Neg: ins.Neg, Behind: ins.Behind,
		Length: 0, MaxLen: maxLen, Pc: bodyStart, Pos: outerPos,
		EndPc: ins.C, Caps: caps,
	})
	t.Pc = bodyStart
	if ins.Behind {
		t.Pos = outerPos
	}
	r.log(Event{Kind: EvLook, Pc: ins.Pc, NID: ins.NID, Pos: t.Pos,
		Detail: fmt.Sprintf("%s id %d begin length 0/%d",
			lookName(ins.Neg, ins.Behind), ins.A, maxLen)})
}

// doLookEnd is reached when a body candidate completes successfully.
func (r *Run) doLookEnd(ins Instr) {
	t := r.Thread
	idx := r.findLookIndex(ins.A)
	if idx < 0 {
		r.failThread("lookaround frame missing")
		return
	}
	lf := t.Stack[idx]
	t.Stack = t.Stack[:idx] // discard frames pushed inside the body

	if lf.Neg {
		// The body matched, so this candidate disproves the assertion at this
		// length. Lookbehind still has longer candidates to try; lookahead
		// fails immediately.
		r.log(Event{Kind: EvLook, Pc: ins.Pc, NID: ins.NID, Pos: lf.Pos,
			Detail: fmt.Sprintf("negative look id %d matched at length %d -> candidate rejected",
				ins.A, lf.Length)})
		r.advanceOrFail(lf, "negative body matched")
		return
	}
	// Positive assertion satisfied. Keep body captures; reset position.
	t.Pos = lf.Pos
	t.Pc = lf.EndPc
	r.log(Event{Kind: EvLook, Pc: ins.Pc, NID: ins.NID, Pos: t.Pos,
		Detail: fmt.Sprintf("positive look id %d satisfied at length %d (%s)",
			ins.A, lf.Length, capsBrief(t.Caps))})
}

// failLook is invoked by failThread when a body candidate runs out of
// continuations.
func (r *Run) failLook(lf Frame, reason string) bool {
	if lf.Neg {
		if lf.Behind {
			// This length disproves nothing; longer candidates may still
			// match, so the negative assertion is not yet proven.
			r.advanceOrFail(lf, reason)
			return !r.finished()
		}
		// Negative lookahead: a failing body proves the assertion.
		r.satisfyNegative(lf, reason)
		return !r.finished()
	}
	// Positive: try next candidate length (lookbehind) or fail (lookahead).
	r.advanceOrFail(lf, reason)
	return !r.finished()
}

// lookCandidateImpossible handles an assertion whose first length is invalid.
func (r *Run) lookCandidateImpossible(lf Frame, reason string) {
	if lf.Neg {
		r.satisfyNegative(lf, reason)
		return
	}
	r.failThread(reason)
}

// satisfyNegative restores the entry capture/position and skips the assertion.
func (r *Run) satisfyNegative(lf Frame, reason string) {
	t := r.Thread
	caps, ok := r.snapshotCaps(lf.Caps)
	if !ok {
		r.indeterminate("snapshot", "negative look restore")
		return
	}
	t.Caps = caps
	t.Pos = lf.Pos
	t.Pc = lf.EndPc
	r.log(Event{Kind: EvLook, Pc: lf.EndPc, Pos: t.Pos,
		Detail: fmt.Sprintf("negative %s id %d satisfied (%s)",
			lookKind(lf.Behind), lf.LookID, reason)})
}

// advanceOrFail restores entry state then either retries the next lookbehind
// candidate length (ascending) or propagates failure.
func (r *Run) advanceOrFail(lf Frame, reason string) {
	t := r.Thread
	caps, ok := r.snapshotCaps(lf.Caps)
	if !ok {
		r.indeterminate("snapshot", "look restore")
		return
	}
	t.Caps = caps
	t.Pos = lf.Pos

	if !lf.Behind {
		r.failThread(fmt.Sprintf("%s failed: %s", lookName(lf.Neg, false), reason))
		return
	}
	nextLen := lf.Length + 1
	if nextLen > lf.MaxLen || nextLen > lf.Pos {
		if lf.Neg {
			// Every candidate length failed: negative lookbehind proven.
			r.satisfyNegative(lf, "all lengths rejected")
			return
		}
		r.failThread(fmt.Sprintf("lookbehind exhausted: %s", reason))
		return
	}
	t.Pc = lf.Pc
	t.Pos = lf.Pos - nextLen
	lf.Length = nextLen
	lf.Caps = caps
	t.push(lf)
	r.log(Event{Kind: EvLook, Pc: lf.Pc, Pos: t.Pos,
		Detail: fmt.Sprintf("lookbehind id %d retry length %d (%s)",
			lf.LookID, nextLen, reason)})
}

func (r *Run) findLookIndex(id int) int {
	t := r.Thread
	for i := len(t.Stack) - 1; i >= 0; i-- {
		if t.Stack[i].Kind == FrameLook && t.Stack[i].LookID == id {
			return i
		}
	}
	return -1
}

func lookName(neg, behind bool) string {
	switch {
	case neg && behind:
		return "negative-lookbehind"
	case neg:
		return "negative-lookahead"
	case behind:
		return "positive-lookbehind"
	default:
		return "positive-lookahead"
	}
}

func lookKind(behind bool) string {
	if behind {
		return "lookbehind"
	}
	return "lookahead"
}

func capsBrief(c Captures) string {
	set := 0
	for i, s := range c {
		if i > 0 && s.Begin >= 0 {
			set++
		}
	}
	return fmt.Sprintf("%d group(s) set", set)
}
