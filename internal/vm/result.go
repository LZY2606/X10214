package vm

import "capturetrace/internal/syntax"

// Paused reports whether the machine stopped at a breakpoint rather than a
// terminal verdict or budget suspension.
func (m *Machine) Paused() bool { return m.paused }

// PausedNode returns the node id at which a breakpoint paused, or 0.
func (m *Machine) PausedNode() int {
	if !m.paused {
		return 0
	}
	l := m.cur()
	if len(l.T) > 0 && l.T[len(l.T)-1].Kind == TermNode {
		return l.T[len(l.T)-1].NodeID
	}
	return 0
}

// Continue clears the breakpoint latch and runs.
func (m *Machine) Continue(stepCap int) Status {
	m.paused = false
	m.pausedAt = 0
	return m.Run(stepCap)
}

// SetBreakpoint adds a node breakpoint on a live machine.
func (m *Machine) SetBreakpoint(id int, on bool) {
	if on {
		m.breakpoints[id] = true
	} else {
		delete(m.breakpoints, id)
	}
}

// Result builds the public outcome snapshot.
func (m *Machine) Result() *Result {
	r := &Result{
		Status:   m.status,
		Match:    m.status == StatusMatched,
		Use:      m.Use(),
		Versions: syntax.VersionPins(),
	}
	if m.paused {
		r.Status = StatusRunning
	}
	if r.Match {
		r.Start, r.End = m.matchStart, m.matchEnd
		r.RuneStart = m.Input.ByteToRune(r.Start)
		r.RuneEnd = m.Input.ByteToRune(r.End)
		r.MatchedText = m.Input.Raw[r.Start:r.End]
		r.Groups = append([]Group(nil), m.finalSnap.Groups...)
	}
	if m.status == StatusIndeterminate {
		r.Frontier = m.Frontier()
	}
	return r
}

// Frontier serializes the suspended machine state for resume.
func (m *Machine) Frontier() *Frontier {
	levels := make([]Level, len(m.levels))
	for i, l := range m.levels {
		levels[i] = *l
	}
	return &Frontier{
		Status: m.status,
		Reason: m.reason,
		Levels: levels,
		Use:    m.Use(),
	}
}

// PrunedCount reports how many branches were cut by atomic/possessive
// constructs (informational; also reflected in branch_pruned events).
func (m *Machine) PrunedCount() int { return m.prunedCount }
