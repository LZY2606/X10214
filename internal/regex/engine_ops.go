package regex

import "unicode/utf8"

// doSplit implements the documented priority ordering: the preferred
// continuation A is executed first; B is pushed beneath it as a fully
// independent snapshot. Greedy splits point A at "repeat", lazy ones point A
// at "exit". No optimizer may reorder A and B.
func (e *engine) doSplit(t Thread, in Instr) (Thread, execStatus) {
	if in.Op == OpSplit && in.Negative {
		// Negative lookaround: only the body is explored now. Continuation B
		// runs solely via the failure-rewrite path once the body fails; it is
		// never placed on the ordinary backtrack stack.
		start := t.BytePos
		if in.Behind {
			rp := e.runePos(t.BytePos)
			if rp < in.Width {
				t.IP = in.B
				return t, execCont
			}
			outer := t.BytePos
			start = e.byteAtRune(rp - in.Width)
			t.Looks = append(t.Looks, LookFrame{ResumeIP: in.B, StartByte: start,
				OuterByte: outer, Negative: true, Behind: true, Depth: len(t.Looks) + 1})
			t.BytePos = start
		} else {
			t.Looks = append(t.Looks, LookFrame{ResumeIP: in.B, StartByte: start,
				Negative: true, Depth: len(t.Looks) + 1})
		}
		t.IP = in.A
		e.recordSplit(&t, in.A, in.B, t.BytePos)
		return t, execCont
	}
	if e.states+1 > e.opts.Budget.MaxStates || e.snapshots+1 > e.opts.Budget.MaxSnapshots {
		e.recordAt(&t, EvPrune, "state/snapshot budget exceeded at split")
		return t, execBudget
	}
	alt := t
	alt.Guards = append([]int(nil), t.Guards...)
	alt.Looks = append([]LookFrame(nil), t.Looks...)
	alt.Slots = append([]int(nil), t.Slots...)
	alt.IP = in.B
	altByte := alt.BytePos
	e.snapshots++
	e.states++
	e.stack = append(e.stack, alt)
	t.IP = in.A
	e.recordSplit(&t, in.A, in.B, altByte)
	return t, execCont
}

func (e *engine) doSave(t *Thread, in Instr) {
	old := t.Slots[in.A]
	newV := t.BytePos
	var oldSpan *Span
	if old >= 0 {
		sp := e.spanOfSlots(t.Slots, in.A)
		oldSpan = &sp
	}
	t.Slots[in.A] = newV
	e.recordCapture(t, in, old, newV, oldSpan)
}

func (e *engine) spanOfSlots(slots []int, slot int) Span {
	group := slot/2 + 1
	lo, hi := slots[(group-1)*2], slots[(group-1)*2+1]
	if lo < 0 || hi < 0 {
		return Span{Matched: false}
	}
	return Span{ByteLo: lo, ByteHi: hi, RuneLo: e.runePos(lo), RuneHi: e.runePos(hi), Matched: lo <= hi}
}

func (e *engine) doBackref(t *Thread, in Instr) bool {
	lo := t.Slots[(in.Ref-1)*2]
	hi := t.Slots[(in.Ref-1)*2+1]
	if lo < 0 || hi < 0 {
		// An unset group participating in a backref matches the empty string
		// (PCRE/JS semantics).
		return true
	}
	want := e.input[lo:hi]
	if t.BytePos+len(want) > len(e.input) {
		e.recordAt(t, EvFail, "backref input too short")
		return false
	}
	got := e.input[t.BytePos : t.BytePos+len(want)]
	eq := caseFoldEqual(want, got)
	if !in.CI {
		eq = string(want) == string(got)
	}
	if !eq {
		e.recordAt(t, EvFail, "backref text differs")
		return false
	}
	t.BytePos += len(want)
	return true
}

// doLookEnter pushes a frame for a positive lookaround. For lookbehind it
// rewinds by in.Width code points (fixed width).
func (e *engine) doLookEnter(t *Thread, in Instr) execStatus {
	start := t.BytePos
	if in.Behind {
		rp := e.runePos(t.BytePos)
		if rp < in.Width {
			e.recordAt(t, EvFail, "fixed-width lookbehind crosses start")
			return execFailed
		}
		outer := t.BytePos
		start = e.byteAtRune(rp - in.Width)
		t.Looks = append(t.Looks, LookFrame{ResumeIP: t.IP, StartByte: start, OuterByte: outer, Behind: in.Behind, Depth: len(t.Looks) + 1})
		t.BytePos = start
		return execCont
	}
	t.Looks = append(t.Looks, LookFrame{ResumeIP: t.IP, StartByte: start, Behind: in.Behind, Depth: len(t.Looks) + 1})
	return execCont
}

// doLookEnd runs when a lookahead/behind body reached its end.
func (e *engine) doLookEnd(t *Thread, in Instr) execStatus {
	if len(t.Looks) == 0 {
		e.recordAt(t, EvFail, "lookend without frame")
		return execFailed
	}
	frame := t.Looks[len(t.Looks)-1]
	t.Looks = t.Looks[:len(t.Looks)-1]
	if frame.Negative {
		// Body matched where it was supposed to fail: assertion fails. Any
		// captures written inside the body die with this thread and cannot
		// leak, because the continuation lives only in the forked snapshot
		// which never executed the body.
		e.recordAt(t, EvFail, "negative lookaround matched; assertion fails")
		return execFailed
	}
	if frame.Behind {
		t.BytePos = frame.OuterByte
	} else {
		// Lookahead rewinds to the assertion anchor; the body consumed input
		// only to prove the suffix exists.
		t.BytePos = frame.StartByte
	}
	// Dispatch already advanced t.IP past LookEnd to the continuation.
	return execCont
}

func (e *engine) checkAnchor(in Instr, pos int, t *Thread) bool {
	switch in.Anchor {
	case "boa":
		return pos == 0
	case "eoa":
		return pos == len(e.input)
	case "bol":
		if pos == 0 {
			return true
		}
		if !in.Multiline {
			return false
		}
		return pos <= len(e.input) && e.input[pos-1] == '\n'
	case "eol":
		if pos == len(e.input) {
			return true
		}
		if in.Multiline {
			return pos < len(e.input) && e.input[pos] == '\n'
		}
		// Non-multiline: permit the single terminating newline convention.
		return pos == len(e.input)-1 && e.input[pos] == '\n'
	case "wb", "nwb":
		prev := wordAtByte(e.input, pos-1)
		next := wordAtByte(e.input, pos)
		wb := prev != next
		if in.Anchor == "wb" {
			return wb
		}
		return !wb
	}
	return false
}

func wordAtByte(input []byte, i int) bool {
	if i < 0 || i >= len(input) {
		return false
	}
	// Only evaluate at a rune boundary; continuation bytes are non-word.
	if i > 0 && !utf8.RuneStart(input[i]) {
		return false
	}
	r, _ := utf8.DecodeRune(input[i:])
	return wordRune(r)
}
