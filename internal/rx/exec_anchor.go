package rx

import "unicode"

func (e *Executor) execAnchor(ins *Instr) {
	m := e.M
	ok := false
	switch ins.Anchor {
	case AnchorBOS:
		ok = m.Pos == 0
	case AnchorEOS:
		ok = m.Pos == e.Idx.Len()
	case AnchorBOL:
		if ins.Multiline {
			ok = m.Pos == 0 || e.prevRune() == '\n'
		} else {
			ok = m.Pos == 0
		}
	case AnchorEOL:
		if ins.Multiline {
			if m.Pos >= e.Idx.Len() {
				ok = true
			} else {
				r, _ := e.Idx.RuneAt(m.Pos)
				ok = r == '\n'
			}
		} else {
			ok = m.Pos == e.Idx.Len()
		}
	case AnchorWB, AnchorNWB:
		wb := e.atWordBoundary(m.Pos)
		if ins.Anchor == AnchorWB {
			ok = wb
		} else {
			ok = !wb
		}
	}
	ev := e.event(EvAnchor, ins)
	if ok {
		ev.Detail = "anchor holds"
		e.emit(ev)
		m.PC++
		return
	}
	ev.Detail = "anchor fails"
	e.emit(ev)
	e.failf(ins, "anchor fails")
}

func (e *Executor) prevRune() rune {
	if e.M.Pos <= 0 {
		return 0
	}
	r, _ := e.Idx.RuneAt(e.M.Pos - 1)
	return r
}

func (e *Executor) isWordAt(p int) bool {
	r, ok := e.Idx.RuneAt(p)
	if !ok {
		return false
	}
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

func (e *Executor) atWordBoundary(p int) bool {
	before := false
	if p > 0 {
		r, _ := e.Idx.RuneAt(p - 1)
		before = unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
	}
	after := e.isWordAt(p)
	return before != after
}

func (e *Executor) execBackref(ins *Instr) {
	m := e.M
	capv := m.Caps[ins.Group]
	ev := e.event(EvTry, ins)
	ev.Group = ins.Group
	if name := e.groupName(ins.Group); name != "" {
		ev.Name = name
	}
	if !capv.Set {
		ev.Detail = "unset group matches empty"
		e.emit(ev)
		m.PC++
		return
	}
	text := e.Idx.Slice(capv.Start, capv.End)
	ev.Text = text
	e.emit(ev)
	if !e.matchText(text, ins.Fold) {
		e.failf(ins, "backreference mismatch")
		return
	}
	m.Pos += cpLen(text)
	m.PC++
}

func cpLen(s string) int {
	return NewIdx(s).Len()
}

// matchText checks whether s matches the input at the current position,
// honoring invalid-UTF-8 byte identity.
func (e *Executor) matchText(s string, fold bool) bool {
	m := e.M
	want := NewIdx(s)
	if m.Pos+want.Len() > e.Idx.Len() {
		return false
	}
	for i := 0; i < want.Len(); i++ {
		a := want.Runes[i]
		b := e.Idx.Runes[m.Pos+i]
		if !foldEqual(fold, a, b) {
			return false
		}
	}
	return true
}
