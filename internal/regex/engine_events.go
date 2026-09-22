package regex

func (e *engine) record(kind EventKind, bytePos, runePos, nodeID int, detail string) {
	if len(e.events) >= e.opts.Budget.MaxEvents {
		e.eventsTruncated = true
		return
	}
	e.seq++
	e.events = append(e.events, Event{Seq: e.seq, Kind: kind, BytePos: bytePos, RunePos: runePos, NodeID: nodeID, Detail: detail, Active: len(e.stack)})
}

func (e *engine) recordAt(t *Thread, kind EventKind, detail string) {
	in := Instr{}
	if t.IP >= 0 && t.IP < len(e.prog.Instr) {
		in = e.prog.Instr[t.IP]
	}
	e.record(kind, t.BytePos, e.runePos(t.BytePos), in.NodeID, detail)
}

func (e *engine) recordStep(t *Thread, in Instr) {
	if len(e.events) >= e.opts.Budget.MaxEvents {
		e.eventsTruncated = true
		return
	}
	e.seq++
	ev := Event{Seq: e.seq, Kind: EvStep, BytePos: t.BytePos, RunePos: e.runePos(t.BytePos),
		NodeID: in.NodeID, Op: in.Op, IP: t.IP, Active: len(e.stack)}
	e.events = append(e.events, ev)
}

func (e *engine) recordSplit(t *Thread, aIP, bIP, altByte int) {
	if len(e.events) >= e.opts.Budget.MaxEvents {
		e.eventsTruncated = true
		return
	}
	e.seq++
	detail := "prefer ip=" + itoa(aIP) + " ; deferred ip=" + itoa(bIP)
	e.events = append(e.events, Event{Seq: e.seq, Kind: EvBranch, BytePos: t.BytePos,
		RunePos: e.runePos(t.BytePos), NodeID: e.prog.Instr[aIP-1].NodeID, Op: OpSplit, IP: aIP,
		Detail: detail, Active: len(e.stack)})
	_ = altByte
}

func (e *engine) recordCapture(t *Thread, in Instr, old, newV int, oldSpan *Span) {
	if len(e.events) >= e.opts.Budget.MaxEvents {
		e.eventsTruncated = true
		return
	}
	e.seq++
	var ns *Span
	sp := e.spanOfSlots(t.Slots, in.A)
	if sp.Matched || sp.ByteLo >= 0 {
		ns = &sp
	}
	ev := Event{Seq: e.seq, Kind: EvCapture, BytePos: t.BytePos, RunePos: e.runePos(t.BytePos),
		NodeID: in.NodeID, Slot: in.A, OldValue: oldSpan, NewValue: ns, Active: len(e.stack)}
	ev.Captures = e.captureSnapshot(t)
	e.events = append(e.events, ev)
}

func (e *engine) captureSnapshot(t *Thread) map[string]Span {
	out := map[string]Span{}
	for g := 1; g <= e.prog.NumGroups; g++ {
		lo := t.Slots[(g-1)*2]
		hi := t.Slots[(g-1)*2+1]
		if lo < 0 || hi < 0 {
			continue
		}
		out[itoa(g)] = Span{ByteLo: lo, ByteHi: hi, RuneLo: e.runePos(lo), RuneHi: e.runePos(hi), Matched: true}
	}
	return out
}

func (e *engine) finish(status Status, reason string, frontier bool) *Result {
	r := &Result{
		Status:          status,
		SyntaxVersion:   SyntaxVersion,
		UnicodeVersion:  UnicodeDataVersion,
		Events:          e.events,
		EventsTruncated: e.eventsTruncated,
		Reason:          reason,
		Budget:          e.opts.Budget,
		BudgetUsed:      BudgetCounters{Steps: e.steps, States: e.states, Snapshots: e.snapshots},
	}
	if frontier {
		r.Frontier = &Frontier{
			Stack: e.stack, TryBytePos: e.tryByte, Steps: e.steps, States: e.states,
			Snapshots: e.snapshots, Seq: e.seq, NextRunePos: e.runeAtAttempt,
		}
	}
	if status == StatusIndeterminate {
		e.record(EvIndeterminate, e.tryByte, e.runePos(e.tryByte), 0, reason)
		r.Events = e.events
	}
	if status == StatusNoMatch {
		e.record(EvNoMatch, len(e.input), e.runePos(len(e.input)), 0, "all attempts exhausted")
		r.Events = e.events
	}
	return r
}

func (e *engine) finishMatch(t Thread) *Result {
	sp := Span{ByteLo: e.tryByte, ByteHi: t.BytePos,
		RuneLo: e.runePos(e.tryByte), RuneHi: e.runePos(t.BytePos), Matched: true}
	caps := make([]Span, e.prog.NumGroups)
	named := map[string]Span{}
	for g := 1; g <= e.prog.NumGroups; g++ {
		lo := t.Slots[(g-1)*2]
		hi := t.Slots[(g-1)*2+1]
		if lo < 0 || hi < 0 {
			caps[g-1] = Span{Matched: false}
			continue
		}
		cs := Span{ByteLo: lo, ByteHi: hi, RuneLo: e.runePos(lo), RuneHi: e.runePos(hi), Matched: true}
		caps[g-1] = cs
	}
	for name, g := range e.prog.groupNames() {
		if caps[g-1].Matched {
			named[name] = caps[g-1]
		}
	}
	e.record(EvMatch, t.BytePos, e.runePos(t.BytePos), 0, "match accepted")
	r := &Result{
		Status:          StatusMatch,
		SyntaxVersion:   SyntaxVersion,
		UnicodeVersion:  UnicodeDataVersion,
		Match:           &sp,
		Captures:        caps,
		Named:           named,
		Events:          e.events,
		EventsTruncated: e.eventsTruncated,
		Budget:          e.opts.Budget,
		BudgetUsed:      BudgetCounters{Steps: e.steps, States: e.states, Snapshots: e.snapshots},
	}
	return r
}

// groupNames returns name -> group index.
func (p *Program) groupNames() map[string]int {
	out := map[string]int{}
	walk(p.AST, func(n *Node) {
		if n.Kind == NGroup && n.Name != "" {
			out[n.Name] = n.Capture
		}
	})
	return out
}
