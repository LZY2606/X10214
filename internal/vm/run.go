package vm

import (
	"fmt"

	"capturetrace/internal/syntax"
)

// LookParent records how to resume the enclosing level when a lookaround
// sub-level finishes.
type LookParent struct {
	LevelIndex int      `json:"level_index"`
	Term       Term     `json:"term"` // the look's term, pushed back on completion
	Pos        int      `json:"pos"`  // anchor byte position
	Snap       Snapshot `json:"snap"` // captures before the assertion
	LevelName  string   `json:"level_name"`
}

// LookDone is the A-frame that completes a lookaround sub-level.
type LookDone struct {
	NodeID   int        `json:"node_id"`
	Ahead    bool       `json:"ahead"`
	Positive bool       `json:"positive"`
	Anchor   int        `json:"anchor"`
	Start    int        `json:"start"`
	Width    int        `json:"width"`
	Parent   LookParent `json:"parent"`
}

// extra level fields kept out of the serialized frame type where possible

func (m *Machine) cur() *Level { return m.levels[len(m.levels)-1] }

func (m *Machine) node(id int) *syntax.Node {
	if id == -1 {
		return m.anchorEndNode()
	}
	return syntax.NodeByID(m.Root, id)
}

func (m *Machine) emit(k EventKind, nodeID int, detail string) *Event {
	m.seq++
	l := m.cur()
	ev := Event{
		Seq:    m.seq,
		Kind:   k,
		NodeID: nodeID,
		Level:  l.Name,
		Detail: detail,
	}
	ev.BytePos = l.Pos
	ev.RunePos = m.Input.ByteToRune(l.Pos)
	u := m.Use()
	ev.Budget = &u
	if len(l.Snap.Groups) > 0 {
		cp := make([]Group, len(l.Snap.Groups))
		copy(cp, l.Snap.Groups)
		ev.Snapshots = cp
		ev.SnapID = l.Snap.ID
	}
	m.events = append(m.events, ev)
	if len(m.events) > m.keepEvents {
		m.events = m.events[len(m.events)-m.keepEvents:]
	}
	return &m.events[len(m.events)-1]
}

func (m *machineLevelExtra) unused() {}

type machineLevelExtra struct{}

// Step performs one micro-step. It returns false when the machine is paused
// or terminal.
func (m *Machine) Step() bool {
	if m.status != StatusRunning {
		return false
	}
	m.use.Steps++
	if m.use.exhaustedSteps() {
		m.suspend("step budget exhausted", EvBudgetExceeded)
		return false
	}

	l := m.cur()

	// root success: term stack exhausted (any next-start handler is only a
	// sibling of failure, so a completed T means a real leftmost match).
	if len(l.T) == 0 {
		if l.Completion == nil {
			m.finishMatch()
			return false
		}
		// a look sub-level whose term stack drained successfully
		m.completeLook(true)
		return m.status == StatusRunning
	}

	top := l.popT()
	m.use.States++
	if m.use.exhaustedStates() {
		l.pushT(top)
		m.suspend("state budget exhausted", EvBudgetExceeded)
		return false
	}

	// breakpoint fires on entering an AST node term
	if top.Kind == TermNode && m.breakpoints[top.NodeID] {
		l.pushT(top)
		m.pausedAt = m.seq + 1
		m.paused = true
		return false
	}

	m.evalTerm(top)
	return m.status == StatusRunning && !m.paused
}

func (m *Machine) finishMatch() {
	l := m.levels[0]
	m.matchStart = l.Start
	m.matchEnd = l.Pos
	m.finalSnap = l.Snap
	m.status = StatusMatched
	m.emit(EvMatch, m.Root.ID, fmt.Sprintf("matched bytes [%d,%d) %q", m.matchStart, m.matchEnd, m.Input.Raw[m.matchStart:m.matchEnd]))
}

// rootStartAttempt reconstructs the current root start position from the
// AltNextStart frame (initial start is 0 when absent).
func (m *Machine) rootStartAttempt() int {
	l := m.levels[0]
	if len(l.A) > 0 && l.A[0].Kind == AltNextStart {
		return l.A[0].NextStart - 1
	}
	// fullmatch/match modes and post-match frames
	return 0
}

// Run executes steps until terminal, a breakpoint, or the step cap.
// stepCap > 0 limits the number of micro-steps performed in this call.
func (m *Machine) Run(stepCap int) Status {
	n := 0
	for m.status == StatusRunning && !m.paused {
		if stepCap > 0 && n >= stepCap {
			break
		}
		if !m.Step() {
			break
		}
		n++
	}
	return m.status
}

func (m *Machine) suspend(reason string, ev EventKind) {
	m.reason = reason
	m.status = StatusIndeterminate
	m.emit(ev, 0, reason)
}
