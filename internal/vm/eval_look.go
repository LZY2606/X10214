package vm

import (
	"fmt"

	"capturetrace/internal/syntax"
)

// evalLook is invoked after the look term has been popped from the parent T
// stack; the saved parent continuation is whatever remains on the parent T.
func (m *Machine) evalLook(n *syntax.Node) {
	parent := m.cur()
	anchor := parent.Pos

	start := anchor
	if !n.Ahead {
		// fixed-width lookbehind: first candidate ends exactly at anchor
		start = anchor - n.Bound
		if start < 0 {
			m.lookVerdict(n, false, anchor, start)
			return
		}
	}
	m.emit(EvLookEnter, n.ID, fmt.Sprintf("look%s %s anchored at byte %d, sub-start %d",
		dirLabel(n.Ahead), polLabel(n.Pos), anchor, start))
	m.spawnLook(n, anchor, start, parent)
}

func dirLabel(ahead bool) string {
	if ahead {
		return "ahead"
	}
	return "behind"
}

func polLabel(pos bool) string {
	if pos {
		return "positive"
	}
	return "negative"
}

func (m *Machine) spawnLook(n *syntax.Node, anchor, start int, parent *Level) {
	sub := &Level{
		Name:   fmt.Sprintf("look#%d@%d", n.ID, len(m.levels)),
		Parent: len(m.levels) - 1,
		Pos:    start,
		Snap:   parent.Snap.clone(),
		Look:   &LookState{NodeID: n.ID, Ahead: n.Ahead, Positive: n.Pos, Anchor: anchor, Start: start},
		Completion: &AltLook{
			Ahead:      n.Ahead,
			Positive:   n.Pos,
			Anchor:     anchor,
			Start:      start,
			Width:      n.Bound,
			ParentPos:  anchor,
			ParentSnap: parent.Snap,
			ParentTerm: Term{}, // unused: continuation stays on parent T stack
		},
	}
	sub.Snap.ID = parent.Snap.ID
	if !n.Ahead {
		// when this sub attempt fully fails (and no retries remain), the
		// completion logic renders the verdict; lookbehind has a single start
		// for fixed width, so no extra Alt frame is needed.
	}
	sub.pushT(Term{Kind: TermNode, NodeID: n.Children[0].ID})
	m.levels = append(m.levels, sub)
	m.emit(EvLookStart, n.ID, fmt.Sprintf("subpattern starts at byte %d", start))
}

// completeLook finishes the current look sub-level. matched=true means its
// subpattern reached the anchor (lookbehind) or any end position (lookahead).
func (m *Machine) completeLook(matched bool) {
	sub := m.cur()
	c := sub.Completion
	nodeID := 0
	if sub.Look != nil {
		nodeID = sub.Look.NodeID
	}
	// lookbehind must finish exactly at the anchor
	if matched && !c.Ahead && sub.Pos != c.Anchor {
		matched = false
	}
	m.levels = m.levels[:len(m.levels)-1]
	m.lookVerdictNode(nodeID, c, matched, sub.Snap)
}

func (m *Machine) lookVerdictNode(nodeID int, c *AltLook, subMatched bool, subSnap Snapshot) {
	parent := m.cur()
	assertionHolds := subMatched == c.Positive
	n := m.node(nodeID)
	if assertionHolds {
		// lookahead discards sub-captures; lookbehind commits them (PCRE)
		if !c.Ahead {
			parent.Snap = subSnap
		}
		m.emit(EvLookSuccess, nodeID, fmt.Sprintf("look%s %s holds", dirLabel(c.Ahead), polLabel(c.Positive)))
		return
	}
	m.emit(EvLookFail, nodeID, fmt.Sprintf("look%s %s fails at anchor %d", dirLabel(c.Ahead), polLabel(c.Positive), c.Anchor))
	_ = n
	parent.Pos = c.ParentPos
	parent.Snap = c.ParentSnap
	m.doFail()
}

func (m *Machine) lookVerdict(n *syntax.Node, matched bool, anchor, start int) {
	c := &AltLook{Ahead: n.Ahead, Positive: n.Pos, Anchor: anchor, Start: start, Width: n.Bound, ParentPos: anchor, ParentSnap: m.cur().Snap}
	m.emit(EvLookEnter, n.ID, fmt.Sprintf("look%s %s anchored at byte %d", dirLabel(n.Ahead), polLabel(n.Pos), anchor))
	m.lookVerdictNode(n.ID, c, matched, m.cur().Snap)
}

// finishLookFromAlt is only used if a look level has retry Alt frames
// (reserved for variable-width extensions).
func (m *Machine) finishLookFromAlt(a Alt) {
	if a.Look == nil {
		m.levelEmpty()
		return
	}
	m.completeLook(false)
}
