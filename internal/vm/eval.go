package vm

import (
	"fmt"
	"unicode"

	"capturetrace/internal/syntax"
)

func (m *Machine) evalTerm(t Term) {
	switch t.Kind {
	case TermAtomicCommit:
		m.commitBarrier(t)
		return
	case TermGroupEnd:
		m.closeGroup(t)
		return
	case TermLoop:
		m.evalLoopCont(t)
		return
	}

	n := m.node(t.NodeID)
	m.emit(EvNodeEnter, n.ID, nodeLabel(n))
	switch n.Kind {
	case syntax.KindEmpty:
		return // success with no movement
	case syntax.KindChar:
		m.evalChar(n)
	case syntax.KindAny:
		m.evalAny(n)
	case syntax.KindClass:
		m.evalClass(n)
	case syntax.KindConcat:
		m.evalConcat(n)
	case syntax.KindAlt:
		m.evalAlt(n)
	case syntax.KindGroup:
		m.evalGroup(n)
	case syntax.KindRepeat:
		m.evalRepeat(n)
	case syntax.KindAssertStart, syntax.KindAssertEnd:
		m.evalAnchor(n)
	case syntax.KindWordBound:
		m.evalWordBound(n)
	case syntax.KindBackref:
		m.evalBackref(n)
	case syntax.KindLook:
		m.evalLook(n)
	case syntax.KindAtomic:
		m.evalAtomic(n)
	}
}

func nodeLabel(n *syntax.Node) string {
	switch n.Kind {
	case syntax.KindChar:
		if n.Atom.RawByte >= 0 {
			return fmt.Sprintf("char byte=0x%02x", n.Atom.RawByte)
		}
		return fmt.Sprintf("char %q", string(n.Atom.R))
	case syntax.KindAny:
		return "any ."
	case syntax.KindClass:
		return "class " + n.Src
	}
	return string(n.Kind)
}

// readAtom returns the atom at the current position and its byte width,
// or ok=false at end of input.
func (m *Machine) readAtom() (syntax.Atom, int, bool) {
	l := m.cur()
	if l.Pos >= m.Input.Bytes() {
		return syntax.Atom{}, 0, false
	}
	ri := m.Input.ByteToRune(l.Pos)
	if m.Input.InvalidByte[ri] {
		return syntax.InvalidByte(m.Input.Raw[l.Pos]), 1, true
	}
	return syntax.Atom{R: m.Input.RuneAt[ri], RawByte: -1}, m.Input.RuneBytes[ri], true
}

func (m *Machine) evalChar(n *syntax.Node) {
	l := m.cur()
	a, w, ok := m.readAtom()
	if !ok {
		m.emit(EvCharFail, n.ID, "end of input")
		m.doFail()
		return
	}
	want := n.Atom
	match := atomsEqual(a, want, n.CaseFold)
	m.emit(EvCharTest, n.ID, fmt.Sprintf("input=%q want=%q casefold=%v", atomText(a), atomText(want), n.CaseFold))
	if !match {
		m.emit(EvCharFail, n.ID, "mismatch")
		m.doFail()
		return
	}
	l.Pos += w
	m.emit(EvCharMatch, n.ID, fmt.Sprintf("consume %q", atomText(a)))
}

func atomsEqual(a, b syntax.Atom, caseFold bool) bool {
	if a.RawByte >= 0 || b.RawByte >= 0 {
		return a.RawByte == b.RawByte
	}
	if a.R == b.R {
		return true
	}
	if !caseFold {
		return false
	}
	for f := unicode.SimpleFold(b.R); f != b.R; f = unicode.SimpleFold(f) {
		if f == a.R {
			return true
		}
	}
	return false
}

func atomText(a syntax.Atom) string {
	if a.RawByte >= 0 {
		return fmt.Sprintf("\\x%02x", a.RawByte)
	}
	return string(a.R)
}

func (m *Machine) evalAny(n *syntax.Node) {
	l := m.cur()
	a, w, ok := m.readAtom()
	if !ok {
		m.emit(EvCharFail, n.ID, "end of input")
		m.doFail()
		return
	}
	if a.RawByte < 0 && a.R == '\n' && !n.DotAll {
		m.emit(EvCharFail, n.ID, "dot does not match newline without (?s)")
		m.doFail()
		return
	}
	l.Pos += w
	m.emit(EvCharMatch, n.ID, fmt.Sprintf("dot consumes %q", atomText(a)))
}

func (m *Machine) evalClass(n *syntax.Node) {
	l := m.cur()
	a, w, ok := m.readAtom()
	if !ok {
		m.emit(EvCharFail, n.ID, "end of input")
		m.doFail()
		return
	}
	if !syntax.MatchClass(n, a) {
		m.emit(EvCharFail, n.ID, fmt.Sprintf("%q not in class %s", atomText(a), n.Src))
		m.doFail()
		return
	}
	l.Pos += w
	m.emit(EvCharMatch, n.ID, fmt.Sprintf("class %s consumes %q", n.Src, atomText(a)))
}

func (m *Machine) evalConcat(n *syntax.Node) {
	l := m.cur()
	for i := len(n.Children) - 1; i >= 0; i-- {
		l.pushT(Term{Kind: TermNode, NodeID: n.Children[i].ID})
	}
}

func (m *Machine) evalAlt(n *syntax.Node) {
	l := m.cur()
	first := n.Children[0]
	m.emit(EvBranchTry, first.ID, fmt.Sprintf("alternation %d branch 0 (documented order)", n.ID))
	for b := len(n.Children) - 1; b >= 1; b-- {
		l.A = append(l.A, Alt{
			Kind:   AltBranch,
			NodeID: n.ID,
			Branch: b,
			Height: len(l.T),
			Pos:    l.Pos,
			Snap:   l.Snap,
		})
	}
	l.pushT(Term{Kind: TermNode, NodeID: first.ID})
}
