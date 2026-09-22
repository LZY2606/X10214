package vm

import (
	"fmt"

	"capturetrace/internal/syntax"
)

func (m *Machine) backrefGroup(n *syntax.Node) int {
	if idx, ok := m.backrefIndex[n.ID]; ok {
		return idx
	}
	idx := syntax.ResolveBackref(n, m.Root)
	m.backrefIndex[n.ID] = idx
	return idx
}

func (m *Machine) evalBackref(n *syntax.Node) {
	l := m.cur()
	idx := m.backrefGroup(n)
	g := l.Snap.Get(idx)
	if !g.Set {
		// unset backreference matches the empty string (PCRE semantics)
		m.emit(EvCharMatch, n.ID, fmt.Sprintf("backreference to unset group %d matches empty", idx))
		return
	}
	text := m.Input.Raw[g.Start:g.End]
	if l.Pos+len(text) > m.Input.Bytes() || m.Input.Raw[l.Pos:l.Pos+len(text)] != text {
		m.emit(EvCharFail, n.ID, fmt.Sprintf("backreference group %d wants %q", idx, text))
		m.doFail()
		return
	}
	l.Pos += len(text)
	m.emit(EvCharMatch, n.ID, fmt.Sprintf("backreference group %d matches %q", idx, text))
}
