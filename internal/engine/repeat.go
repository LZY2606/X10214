package engine

import "fmt"

func (e *engine) matchRepeat(n *RepeatNode, pos Pos, caps []*Span, k continuation, possessiveWrapper bool) outcome {
	if n.Max >= 0 && n.Max < n.Min {
		e.emit("fail", n, pos, 0, "", "invalid repeat bounds", nil)
		return outcomeFailed
	}
	return e.repeatAt(n, pos, caps, k, 0, n.Min, n.Max, n.Lazy, n.Possessive && !possessiveWrapper, 0)
}

func (e *engine) repeatAt(n *RepeatNode, start Pos, caps []*Span, k continuation, count, min, max int, lazy, possessive bool, lastWidth int) outcome {
	atMin := count >= min
	if atMin && lastWidth == 0 {
		return e.finishRepeat(n, start, caps, k, possessive)
	}
	canMore := max < 0 || count < max
	if !lazy {
		if canMore {
			saved := cloneCaps(caps)
			if !e.snapshot(1) {
				e.pruned++
				return outcomeAborted
			}
			e.pushFrontier(n, start, fmt.Sprintf("greedy repeat %d: stop here on later backtrack", count))
			st := e.match(n.Child, start, caps, func(end Pos, c []*Span) outcome {
				width := end.Byte - start.Byte
				return e.repeatAt(n, end, c, k, count+1, min, max, lazy, possessive, width)
			})
			e.popFrontier()
			if st == outcomeSuccess || st == outcomePaused || st == outcomeAborted || st == outcomeCut {
				return st
			}
			caps = saved
			if atMin {
				e.emit("prune", n, start, 0, "", "greedy expansion rejected; stopping and restoring captures", nil)
				return e.finishRepeat(n, start, caps, k, possessive)
			}
			return outcomeFailed
		}
		if atMin {
			return e.finishRepeat(n, start, caps, k, possessive)
		}
		return outcomeFailed
	}

	if atMin {
		if canMore {
			saved := cloneCaps(caps)
			if !e.snapshot(1) {
				e.pruned++
				return outcomeAborted
			}
			e.pushFrontier(n, start, fmt.Sprintf("lazy repeat %d: consume one more on backtrack", count))
			st := e.finishRepeat(n, start, caps, k, false)
			if st == outcomeSuccess || st == outcomePaused || st == outcomeAborted || st == outcomeCut {
				return st
			}
			e.popFrontier()
			caps = saved
			return e.match(n.Child, start, caps, func(end Pos, c []*Span) outcome {
				width := end.Byte - start.Byte
				return e.repeatAt(n, end, c, k, count+1, min, max, lazy, possessive, width)
			})
		}
		return e.finishRepeat(n, start, caps, k, false)
	}
	return e.match(n.Child, start, caps, func(end Pos, c []*Span) outcome {
		width := end.Byte - start.Byte
		return e.repeatAt(n, end, c, k, count+1, min, max, lazy, possessive, width)
	})
}

func (e *engine) finishRepeat(n *RepeatNode, pos Pos, caps []*Span, k continuation, possessive bool) outcome {
	if !possessive {
		return k(pos, caps)
	}
	st := k(pos, caps)
	if st == outcomeFailed {
		e.emit("prune", n, pos, 0, "", "possessive expansion committed; repeat frontier removed", nil)
		return outcomeCut
	}
	return st
}
