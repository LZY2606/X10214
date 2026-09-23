package engine

import (
	"fmt"
)

func (e *engine) matchBackref(n *BackrefNode, pos Pos, caps []*Span, k continuation) outcome {
	if n.Index >= len(caps) || caps[n.Index] == nil {
		e.emit("fail", n, pos, n.Index, n.Name, "unset backreference", nil)
		return outcomeFailed
	}
	target := caps[n.Index].Text
	if pos.Byte+len(target) > len(e.in.Bytes) || string(e.in.Bytes[pos.Byte:pos.Byte+len(target)]) != target {
		return outcomeFailed
	}
	end := pos
	for range target {
		end = e.in.advance(end)
	}
	return e.guard(k(end, caps))
}

func (e *engine) matchLookaround(n *LookaroundNode, pos Pos, caps []*Span, k continuation) outcome {
	if !n.Behind {
		return e.matchLookahead(n, pos, caps, k)
	}
	return e.matchLookbehind(n, pos, caps, k)
}

func (e *engine) matchLookahead(n *LookaroundNode, pos Pos, caps []*Span, k continuation) outcome {
	saved := cloneCaps(caps)
	succeeded := false
	var st outcome
	if n.Negated {
		st = e.match(n.Child, pos, caps, func(Pos, []*Span) outcome { succeeded = true; return outcomeFailed })
	} else {
		cut := false
		st = e.match(n.Child, pos, caps, func(end Pos, c []*Span) outcome {
			succeeded = true
			cut = true
			_ = end
			return outcomeCut
		})
		if st == outcomeCut && cut {
			st = outcomeFailed
		}
	}
	if st == outcomePaused || st == outcomeAborted {
		return st
	}
	if n.Negated && succeeded || !n.Negated && !succeeded {
		caps = saved
		return outcomeFailed
	}
	caps = saved
	label := "lookahead assertion discarded inner captures"
	if n.Negated {
		label = "negative lookahead discarded inner captures"
	}
	e.emit("zero-width", n, pos, 0, "", label, nil)
	return e.guard(k(pos, caps))
}

func (e *engine) matchLookbehind(n *LookaroundNode, pos Pos, caps []*Span, k continuation) outcome {
	limit := n.maxLookbehind(e.budget.MaxLookbehind)
	startScan := pos.Byte - limit
	if limit < 0 || startScan < 0 {
		startScan = 0
	}
	found := false
	for startByte := pos.Byte; startByte >= startScan; startByte-- {
		if startByte > pos.Byte {
			continue
		}
		start := e.in.PosAt(startByte)
		if start.Rune < 0 || startByte > pos.Byte {
			continue
		}
		saved := cloneCaps(caps)
		cut := false
		st := e.match(n.Child, start, caps, func(end Pos, c []*Span) outcome {
			if end.Byte != pos.Byte {
				return outcomeFailed
			}
			found = true
			cut = true
			return outcomeCut
		})
		if st == outcomePaused || st == outcomeAborted {
			return st
		}
		if st == outcomeCut && cut {
			break
		}
		caps = saved
		if startByte == 0 {
			break
		}
	}
	if n.Negated && found || !n.Negated && !found {
		return outcomeFailed
	}
	e.emit("zero-width", n, pos, 0, "", fmt.Sprintf("lookbehind assertion (budget bytes %d) discarded inner captures", e.budget.MaxLookbehind), nil)
	return e.guard(k(pos, caps))
}
