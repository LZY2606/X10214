package engine

import (
	"encoding/json"
	"fmt"
	"unicode"
)

func (e *engine) check(kind string, n Node, pos Pos, caps []*Span) outcome {
	e.usage.Steps++
	if e.controller != nil {
		cmd, stop := e.controller.BeforeStep(kind, n, pos, cloneCaps(caps), e.usage, e.budget, e.snapshotFrontier())
		if stop {
			e.emit("pause", n, pos, 0, "", cmd.Action, nil)
			e.stopped = true
			return outcomePaused
		}
		if cmd.Action == "abort" {
			e.emit("abort", n, pos, 0, "", "requested abort", nil)
			e.stopped = true
			return outcomeAborted
		}
		e.stepCommand = cmd.Action
	}
	if e.usage.Steps > e.budget.MaxSteps {
		e.pruned++
		e.emit("budget-step", n, pos, 0, "", "step budget reached", nil)
		return outcomeAborted
	}
	return outcomeFailed
}

func (e *engine) registerState(n Node, pos Pos) outcome {
	e.usage.States++
	if e.usage.States > e.budget.MaxStates {
		e.pruned++
		e.emit("budget-state", n, pos, 0, "", "state budget reached", nil)
		return outcomeAborted
	}
	return outcomeFailed
}

func (e *engine) snapshot(cost int) bool {
	e.usage.Snapshots += cost
	if e.usage.Snapshots > e.budget.MaxSnapshots {
		e.pruned++
		return false
	}
	return true
}

func (e *engine) match(n Node, pos Pos, caps []*Span, k continuation) outcome {
	if e.stopped {
		return outcomePaused
	}
	if st := e.check("enter", n, pos, caps); st != outcomeFailed {
		return st
	}
	if st := e.registerState(n, pos); st != outcomeFailed {
		return st
	}
	e.emit("enter", n, pos, 0, "", "active state", nil)
	var st outcome
	switch x := n.(type) {
	case *LiteralNode:
		st = e.matchLiteral(x, pos, caps, k)
	case *DotNode:
		st = e.matchDot(pos, caps, k)
	case *CharClassNode:
		st = e.matchClass(x, pos, caps, k)
	case *AnchorNode:
		st = e.matchAnchor(x, pos, caps, k)
	case *GroupNode:
		st = e.matchGroup(x, pos, caps, k)
	case *ConcatNode:
		st = e.matchConcat(x, 0, pos, caps, k)
	case *AltNode:
		st = e.matchAlt(x, pos, caps, k, false)
	case *RepeatNode:
		st = e.matchRepeat(x, pos, caps, k, false)
	case *BackrefNode:
		st = e.matchBackref(x, pos, caps, k)
	case *LookaroundNode:
		st = e.matchLookaround(x, pos, caps, k)
	default:
		st = outcomeAborted
	}
	if st == outcomeFailed {
		e.backtracks++
		e.emit("fail", n, pos, 0, "", "this path returned to caller", nil)
	}
	return st
}

func (e *engine) matchLiteral(n *LiteralNode, pos Pos, caps []*Span, k continuation) outcome {
	r, _, ok := e.in.Elem(pos)
	if ok != n.Invalid || r != n.Rune {
		return outcomeFailed
	}
	next := e.in.advance(pos)
	e.emit("consume", n, next, 0, "", "literal accepted", nil)
	return e.guard(k(next, caps))
}

func (e *engine) matchDot(pos Pos, caps []*Span, k continuation) outcome {
	if pos.Byte >= len(e.in.Bytes) {
		return outcomeFailed
	}
	next := e.in.advance(pos)
	return e.guard(k(next, caps))
}

func (e *engine) matchClass(n *CharClassNode, pos Pos, caps []*Span, k continuation) outcome {
	r, _, invalid := e.in.Elem(pos)
	if pos.Byte >= len(e.in.Bytes) {
		return outcomeFailed
	}
	if classMatches(n, r, invalid) != true {
		return outcomeFailed
	}
	return e.guard(k(e.in.advance(pos), caps))
}

func (e *engine) matchAnchor(n *AnchorNode, pos Pos, caps []*Span, k continuation) outcome {
	ok := false
	switch n.Kind {
	case "start-text":
		ok = pos.Byte == 0
	case "end-text":
		ok = pos.Byte == len(e.in.Bytes)
	case "word-boundary":
		ok = e.wordAt(pos.Byte-1) != e.wordAt(pos.Byte)
	case "not-word-boundary":
		ok = e.wordAt(pos.Byte-1) == e.wordAt(pos.Byte)
	}
	if !ok {
		return outcomeFailed
	}
	e.emit("zero-width", n, pos, 0, "", n.Kind, nil)
	return e.guard(k(pos, caps))
}

func (e *engine) wordAt(bytePos int) bool {
	if bytePos < 0 || bytePos >= len(e.in.Bytes) {
		return false
	}
	r, _, _ := decodeRuneAt(e.in.Bytes, bytePos)
	return r == '_' || unicode.In(r, unicode.Letter, unicode.Number)
}

func (e *engine) matchGroup(n *GroupNode, pos Pos, caps []*Span, k continuation) outcome {
	if n.Atomic {
		cut := false
		st := e.match(n.Child, pos, caps, func(end Pos, c []*Span) outcome {
			if !e.snapshot(1) {
				e.pruned++
				return outcomeAborted
			}
			e.pushFrontier(n, end, "atomic group continuation is protected")
			st := k(end, c)
			e.popFrontier()
			if st == outcomeFailed {
				cut = true
				return outcomeCut
			}
			return st
		})
		if st == outcomeCut && cut {
			return outcomeFailed
		}
		return st
	}
	if !n.Capture {
		return e.match(n.Child, pos, caps, k)
	}
	var old *Span
	if n.Index < len(caps) {
		old = cloneSpan(caps[n.Index])
	}
	st := e.match(n.Child, pos, caps, func(end Pos, c []*Span) outcome {
		if n.Index >= len(c) {
			return outcomeAborted
		}
		c[n.Index] = &Span{Start: pos, End: end, Text: e.in.slice(pos, end)}
		data, _ := json.Marshal(c[n.Index])
		e.emit("capture", n, pos, n.Index, n.Name, "capture opened/updated", data)
		if !e.snapshot(1) {
			return outcomeAborted
		}
		e.pushFrontier(n, end, fmt.Sprintf("undo capture %s", captureKey(n.Index, n.Name)))
		st := k(end, c)
		e.popFrontier()
		if st == outcomeFailed {
			e.emit("restore", n, end, n.Index, n.Name, "capture rolled back", nil)
			c[n.Index] = old
		}
		return st
	})
	return st
}

func captureKey(index int, name string) string {
	if name != "" {
		return name
	}
	return fmt.Sprintf("%d", index)
}

func (e *engine) matchConcat(n *ConcatNode, idx int, pos Pos, caps []*Span, k continuation) outcome {
	if idx == len(n.Items) {
		return k(pos, caps)
	}
	return e.match(n.Items[idx], pos, caps, func(end Pos, c []*Span) outcome {
		return e.matchConcat(n, idx+1, end, c, k)
	})
}

func (e *engine) matchAlt(n *AltNode, pos Pos, caps []*Span, k continuation, cutBranch bool) outcome {
	for i := range n.Branches {
		saved := cloneCaps(caps)
		if !e.snapshot(len(n.Branches) - i) {
			e.pruned++
			return outcomeAborted
		}
		e.pushFrontier(n, pos, fmt.Sprintf("alternation branch %d documented order", i+1))
		st := e.match(n.Branches[i], pos, caps, k)
		e.popFrontier()
		if st == outcomeCut {
			if cutBranch {
				return outcomeCut
			}
			return outcomeFailed
		}
		if st != outcomeFailed {
			return st
		}
		caps = saved
		e.pruned++
		e.emit("prune", n.Branches[i], pos, 0, "", fmt.Sprintf("branch %d rejected and restored", i+1), nil)
	}
	return outcomeFailed
}

func (e *engine) guard(st outcome) outcome {
	if st == outcomeCut {
		return outcomeFailed
	}
	return st
}

func (e *engine) pushFrontier(n Node, pos Pos, detail string) {
	e.frontier = append(e.frontier, &frontierFrame{n, pos, detail})
}
func (e *engine) popFrontier() {
	if len(e.frontier) > 0 {
		e.frontier = e.frontier[:len(e.frontier)-1]
	}
}
func (e *engine) snapshotFrontier() []FrontierPoint {
	out := make([]FrontierPoint, 0, len(e.frontier))
	for _, f := range e.frontier {
		out = append(out, FrontierPoint{NodeID: f.node.GetID(), Type: f.node.nodeType(), Detail: f.detail, Byte: f.pos.Byte, Rune: f.pos.Rune})
	}
	return out
}

func cloneCaps(in []*Span) []*Span {
	out := make([]*Span, len(in))
	for i := range in {
		out[i] = cloneSpan(in[i])
	}
	return out
}
func cloneSpan(s *Span) *Span {
	if s == nil {
		return nil
	}
	c := *s
	return &c
}

func (e *engine) emit(kind string, n Node, pos Pos, group int, name, detail string, data json.RawMessage) {
	nodeType := ""
	id := ""
	if n != nil {
		nodeType = n.nodeType()
		id = n.GetID()
	}
	pp := pos
	e.events = append(e.events, Event{Seq: len(e.events) + 1, Kind: kind, NodeID: id, Type: nodeType, Pos: &pp, Group: group, Name: name, Detail: detail, Data: data})
}
