package engine

// matchAnchor evaluates a zero-width assertion at rune position pos.
func (e *Engine) matchAnchor(kind, flags string, pos int) bool {
	in := e.in
	switch kind {
	case "start": // \A
		return pos == 0
	case "end": // \z
		return pos == len(in.Slots)
	case "bol": // ^
		if pos == 0 {
			return true
		}
		if flagOn(flags, 'm') && pos > 0 && in.Slots[pos-1].R == '\n' {
			return true
		}
		return false
	case "eol": // $
		if pos == len(in.Slots) {
			return true
		}
		if flagOn(flags, 'm') && pos < len(in.Slots) && in.Slots[pos].R == '\n' {
			return true
		}
		return false
	case "wb": // \b
		return e.wordAt(pos-1) != e.wordAt(pos)
	case "nwb": // \B
		return e.wordAt(pos-1) == e.wordAt(pos)
	}
	return false
}

func (e *Engine) wordAt(pos int) bool {
	if pos < 0 || pos >= len(e.in.Slots) {
		return false
	}
	return isASCIIWord(e.in.Slots[pos].R)
}
