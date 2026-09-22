package vm

import (
	"fmt"

	"capturetrace/internal/syntax"
)

func (m *Machine) evalAnchor(n *syntax.Node) {
	l := m.cur()
	ok := false
	switch n.Kind {
	case syntax.KindAssertStart:
		if l.Look != nil {
			ok = l.Pos == l.Look.Start // inside lookbehind, anchor to sub start
		} else {
			ok = l.Pos == 0
		}
	case syntax.KindAssertEnd:
		ok = l.Pos == m.Input.Bytes()
		if !ok && n.AtFinalNL {
			ok = l.Pos == m.Input.Bytes()-1 && m.Input.Raw[l.Pos] == '\n'
		}
	}
	label := "start"
	if n.Kind == syntax.KindAssertEnd {
		label = "end"
	}
	m.emit(EvAnchorTest, n.ID, fmt.Sprintf("anchor %s at byte %d", label, l.Pos))
	if ok {
		m.emit(EvAnchorMatch, n.ID, "anchor holds")
		return
	}
	m.emit(EvAnchorFail, n.ID, "anchor fails")
	m.doFail()
}

func (m *Machine) evalWordBound(n *syntax.Node) {
	l := m.cur()
	prev := m.wordCharAt(l.Pos - 1)
	cur := m.wordCharAt(l.Pos)
	bound := prev != cur
	ok := bound == n.WBPos
	m.emit(EvAnchorTest, n.ID, fmt.Sprintf("word boundary at %d (prev-word=%v cur-word=%v want-bound=%v)", l.Pos, prev, cur, n.WBPos))
	if ok {
		m.emit(EvAnchorMatch, n.ID, "word condition holds")
		return
	}
	m.emit(EvAnchorFail, n.ID, "word condition fails")
	m.doFail()
}

func (m *Machine) wordCharAt(bytePos int) bool {
	if bytePos < 0 || bytePos >= m.Input.Bytes() {
		return false
	}
	ri := m.Input.ByteToRune(bytePos)
	if m.Input.InvalidByte[ri] {
		return false
	}
	r := m.Input.RuneAt[ri]
	return syntax.EvalClass([]*syntax.Range{{Class: "w"}}, false, false, syntax.Atom{R: r, RawByte: -1})
}
