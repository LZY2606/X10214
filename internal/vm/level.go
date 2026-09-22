package vm

import (
	"fmt"

	"capturetrace/internal/syntax"
)

// levelEmpty handles a level whose success stack is exhausted.
func (m *Machine) levelEmpty() {
	l := m.cur()
	if len(l.A) == 0 {
		if l.Completion == nil {
			// root level: no alternatives anywhere
			m.finishNoMatch()
			return
		}
		m.completeLook(false)
		return
	}
	m.doFail()
}

// doFail pops the innermost alternative and takes it.
func (m *Machine) doFail() {
	l := m.cur()
	if len(l.A) == 0 {
		m.levelEmpty()
		return
	}
	a := l.A[len(l.A)-1]
	l.A = l.A[:len(l.A)-1]

	switch a.Kind {
	case AltBranch:
		m.restore(a)
		n := m.node(a.NodeID)
		branch := n.Children[a.Branch]
		m.emit(EvBranchTry, branch.ID, fmt.Sprintf("alternation %d branch %d (documented order)", a.NodeID, a.Branch))
		l.pushT(Term{Kind: TermNode, NodeID: branch.ID})
		m.pushLaterBranches(n, a.Branch)

	case AltLoopStop:
		m.restore(a)
		m.takeLoopStop(a)

	case AltLoopRetry:
		m.restore(a)
		m.takeLoopRetry(a)

	case AltMandatory:
		m.restore(a)
		m.emit(EvBacktrack, a.NodeID, "required repetition not satisfied; propagating failure")
		m.doFail()

	case AltGroupUndo:
		m.takeGroupUndo(a)

	case AltBarrier:
		// failure crosses an atomic/possessive barrier: handlers below it
		// belong to the outer expression and remain reachable.
		m.restore(a)
		m.doFail()

	case AltNextStart:
		m.takeNextStart(a)

	case AltLookDone:
		// sub-level exhausted after previous start attempts; use payload
		m.finishLookFromAlt(a)
	}
}

func (m *Machine) takeGroupUndo(a Alt) {
	l := m.cur()
	if a.Height >= 0 && a.Height <= len(l.T) {
		l.T = append([]Term(nil), l.T[:a.Height]...)
	}
	l.Pos = a.Pos
	idx := a.Group
	base := a.Snap
	if a.PrevSnap != nil {
		base = *a.PrevSnap
	}
	prev := base.Get(idx)
	restored, ok := m.snaps.make(base, idx-1, prev)
	if !ok {
		m.suspend("capture snapshot budget exhausted while undoing group", EvBudgetExceeded)
		return
	}
	l.Snap = restored
	m.emit(EvCaptureRollback, a.NodeID, fmt.Sprintf("group %d failed; restored prior capture (set=%v)", idx, prev.Set)).Group = idx
	m.doFail()
}

func (m *Machine) restore(a Alt) {
	l := m.cur()
	if a.Height >= 0 && a.Height <= len(l.T) {
		l.T = append([]Term(nil), l.T[:a.Height]...)
	}
	l.Pos = a.Pos
	if len(a.Snap.Groups) > 0 {
		l.Snap = a.Snap
	}
}

func (m *Machine) pushLaterBranches(n *syntax.Node, chosen int) {
	l := m.cur()
	for b := chosen + 1; b < len(n.Children); b++ {
		l.A = append(l.A, Alt{
			Kind:   AltBranch,
			NodeID: n.ID,
			Branch: b,
			Height: len(l.T),
			Pos:    l.Pos,
			Snap:   l.Snap,
		})
	}
}

func (m *Machine) finishNoMatch() {
	m.status = StatusNoMatch
	m.reason = "all start positions and branches exhausted"
	m.emit(EvNoMatch, 0, m.reason)
}

func (m *Machine) takeNextStart(a Alt) {
	l := m.cur()
	next := a.NextStart
	if next > m.Input.Bytes() {
		m.finishNoMatch()
		return
	}
	l.Pos = next
	l.Start = next
	l.Snap = a.Snap
	l.A = []Alt{{Kind: AltNextStart, Pos: next, Snap: a.Snap, NextStart: next + 1}}
	l.T = []Term{{Kind: TermNode, NodeID: m.Root.ID}}
	m.emit(EvStartAdvance, 0, fmt.Sprintf("retry search at byte %d (rune %d)", next, m.Input.ByteToRune(next)))
}
