package vm

import (
	"fmt"

	"capturetrace/internal/syntax"
)

func (m *Machine) evalRepeat(n *syntax.Node) {
	l := m.cur()
	m.emit(EvRepeatEnter, n.ID, fmt.Sprintf("repeat min=%d max=%d greedy=%v possessive=%v", n.Min, n.Max, n.Greedy, n.Possess))
	barrierID := 0
	if n.Possess {
		barrierID = l.newBarrierID()
		l.A = append(l.A, Alt{Kind: AltBarrier, NodeID: n.ID, Barrier: barrierID, Height: len(l.T), Pos: l.Pos, Snap: l.Snap})
	}
	m.beginIteration(n, 0, barrierID)
}

// beginIteration pushes the loop continuation and one child term (or stops
// the repetition when the minimum was already satisfied and stopping wins).
func (m *Machine) beginIteration(n *syntax.Node, count, barrierID int) {
	l := m.cur()
	if n.Max >= 0 && count >= n.Max {
		// exactly max reached: must stop; prune any stop/retry frames of this loop
		m.finishLoop(n, count, barrierID)
		return
	}
	term := Term{Kind: TermLoop, NodeID: n.ID, Count: count, StartPos: l.Pos}
	if barrierID != 0 {
		term.GroupIndex = barrierID
	}
	canStop := count >= n.Min
	if canStop && !n.Greedy && !n.Possess {
		// lazy: stopping has priority, retrying is the alternative. The
		// no-progress rule is enforced when the retry is actually taken
		// (see takeLoopRetry), so a retry frame is always pushed here.
		{
			l.pushT(term)
			l.A = append(l.A, Alt{
				Kind: AltLoopRetry, NodeID: n.ID, Count: count,
				Height: len(l.T), Pos: l.Pos, Snap: l.Snap, IterPos: l.Pos,
			})
		}
	} else {
		l.pushT(term)
		if canStop && !n.Possess {
			// greedy: child iteration first; stopping is the alternative
			l.A = append(l.A, Alt{
				Kind: AltLoopStop, NodeID: n.ID, Count: count,
				Height: len(l.T), Pos: l.Pos, Snap: l.Snap, IterPos: l.Pos,
			})
		}
	}
	if !canStop {
		// below any child alternatives, a hard failure: fewer than min reps
		l.A = append(l.A, Alt{
			Kind: AltMandatory, NodeID: n.ID, Count: count,
			Height: len(l.T), Pos: l.Pos, Snap: l.Snap,
		})
	}
	l.pushT(Term{Kind: TermNode, NodeID: n.Child.ID})
}

// evalLoopCont runs when one child repetition succeeded and control returns
// to the loop. Zero-width repetitions are terminated here.
func (m *Machine) evalLoopCont(t Term) {
	n := m.node(t.NodeID)
	l := m.cur()
	count := t.Count + 1
	barrierID := t.GroupIndex

	zeroWidth := l.Pos == t.StartPos
	if zeroWidth {
		// No-progress rule: an iteration that consumed nothing settles the
		// quantifier at the current count.
		m.emit(EvRepeatZeroBlock, n.ID, fmt.Sprintf("zero-width iteration at byte %d terminated", l.Pos))
		m.popLoopDecision(n, t.Count)
		m.finishLoop(n, count, barrierID)
		return
	}
	m.beginIteration(n, count, barrierID)
}

func (m *Machine) popLoopDecision(n *syntax.Node, count int) {
	l := m.cur()
	for i := len(l.A) - 1; i >= 0; i-- {
		a := l.A[i]
		if a.NodeID == n.ID && a.Count == count &&
			(a.Kind == AltLoopStop || a.Kind == AltLoopRetry) {
			l.A = append(l.A[:i], l.A[i+1:]...)
			return
		}
	}
}

// finishLoop removes the loop term and cuts possessive barriers.
func (m *Machine) finishLoop(n *syntax.Node, count, barrierID int) {
	l := m.cur()
	if barrierID != 0 {
		if bi := l.barrierIndex(barrierID); bi >= 0 {
			cut := len(l.A) - bi - 1
			m.prunedCount += cut
			l.A = l.A[:bi]
			m.emit(EvBranchPruned, n.ID, fmt.Sprintf("possessive repeat {%d} commits; %d inner alternatives pruned", count, cut))
		}
	}
}

// takeLoopStop handles AltLoopStop: keep the current position and continue
// after the quantifier.
func (m *Machine) takeLoopStop(a Alt) {
	n := m.node(a.NodeID)
	l := m.cur()
	m.emit(EvBacktrack, n.ID, fmt.Sprintf("greedy repeat gives back after %d iterations at byte %d (captures restored)", a.Count, l.Pos))
	// The TermLoop for this count is at T[a.Height]; drop it so control
	// continues with the enclosing continuation.
	m.finishLoop(n, a.Count, 0)
}

// takeLoopRetry handles AltLoopRetry for lazy quantifiers: force one more
// child iteration.
func (m *Machine) takeLoopRetry(a Alt) {
	n := m.node(a.NodeID)
	l := m.cur()
	if l.Pos == a.IterPos {
		// forcing another repetition cannot consume anything: commit count
		m.emit(EvRepeatZeroBlock, n.ID, fmt.Sprintf("zero-width lazy expansion at byte %d blocked", l.Pos))
		m.finishLoop(n, a.Count, 0)
		return
	}
	m.emit(EvBacktrack, n.ID, fmt.Sprintf("lazy repeat expands to %d iterations", a.Count+1))
	barrierID := 0
	m.beginIteration(n, a.Count, barrierID)
}
