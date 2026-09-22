package vm

import (
	"fmt"

	"capturetrace/internal/syntax"
)

func (m *Machine) evalGroup(n *syntax.Node) {
	l := m.cur()
	if !n.Capture {
		// non-capturing (or flag-only) wrapper
		l.pushT(Term{Kind: TermNode, NodeID: n.Children[0].ID})
		return
	}
	idx := n.Index
	prev := l.Snap.Get(idx)
	preSnap := l.Snap
	opened, ok := m.snaps.make(l.Snap, idx-1, Group{Set: true, Start: l.Pos, End: l.Pos})
	if !ok {
		m.suspend("capture snapshot budget exhausted while opening group", EvBudgetExceeded)
		return
	}
	l.Snap = opened
	m.emit(EvCaptureOpen, n.ID, fmt.Sprintf("group %d (%s) opens at byte %d", idx, groupLabel(n), l.Pos)).Group = idx
	l.pushT(Term{Kind: TermGroupEnd, NodeID: n.ID, GroupIndex: idx})
	l.pushT(Term{Kind: TermNode, NodeID: n.Children[0].ID})
	// if the body fails without any internal alternative succeeding,
	// restore the previous capture of this group (unconditional undo).
	l.A = append(l.A, Alt{
		Kind:   AltGroupUndo,
		NodeID: n.ID,
		Height: len(l.T),
		Pos:    l.Pos,
		Snap:   l.Snap,
	})
	// prev value is reconstructed on undo via a dedicated marker carried in Count:
	l.A[len(l.A)-1].Count = idx
	l.A[len(l.A)-1].Group = idx
	l.A[len(l.A)-1].PrevSnap = &preSnap
	_ = prev
}

func groupLabel(n *syntax.Node) string {
	if n.Name != "" {
		return n.Name
	}
	return fmt.Sprintf("#%d", n.Index)
}

func (m *Machine) closeGroup(t Term) {
	l := m.cur()
	idx := t.GroupIndex
	g := l.Snap.Get(idx)
	g.End = l.Pos
	ns, ok := m.snaps.make(l.Snap, idx-1, g)
	if !ok {
		m.suspend("capture snapshot budget exhausted while closing group", EvBudgetExceeded)
		return
	}
	l.Snap = ns
	m.emit(EvCaptureClose, t.NodeID, fmt.Sprintf("group %d closes [%d,%d)", idx, g.Start, g.End)).Group = idx
}

func (m *Machine) evalAtomic(n *syntax.Node) {
	l := m.cur()
	id := l.newBarrierID()
	l.pushT(Term{Kind: TermAtomicCommit, NodeID: n.ID})
	// barrier frame: inside alternatives are cut on success; failures that
	// pass the barrier propagate outside.
	l.A = append(l.A, Alt{Kind: AltBarrier, NodeID: n.ID, Barrier: id, Height: len(l.T), Pos: l.Pos, Snap: l.Snap})
	// store barrier id on commit term via NodeID alone? Need both. Encode id
	// in Count field.
	l.T[len(l.T)-1].Count = id
	l.pushT(Term{Kind: TermNode, NodeID: n.Children[0].ID})
}

func (m *Machine) commitBarrier(t Term) {
	l := m.cur()
	if bi := l.barrierIndex(t.Count); bi >= 0 {
		// cut every alternative that belongs to inside the atomic group
		cut := len(l.A) - bi - 1
		l.A = l.A[:bi]
		m.prunedCount += cut
		m.emit(EvBranchPruned, t.NodeID, fmt.Sprintf("atomic group commits; %d inner alternatives pruned", cut))
	}
}
