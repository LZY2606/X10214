package engine

import (
	"unicode"
	"unicode/utf8"

	"traceroom/syntax"
)

func foldRune(r rune) rune { return unicode.ToLower(r) }

func runeEq(a, b rune, caseInsensitive bool) bool {
	if a == b {
		return true
	}
	return caseInsensitive && foldRune(a) == foldRune(b)
}

// matchClass reports whether r satisfies the (possibly negated) class.
func matchClass(cls *syntax.Class, r rune) bool {
	ok := false
	for _, t := range cls.Terms {
		var hit bool
		if t.Prop != "" {
			hit = matchProp(t.Prop, r)
		} else {
			hit = r >= t.Lo && r <= t.Hi
		}
		if t.Neg {
			hit = !hit
		}
		if hit {
			ok = true
			break
		}
	}
	if cls.Negated {
		return !ok
	}
	return ok
}

// matchProp implements shorthand and POSIX properties against the Go Unicode
// tables pinned to the engine Unicode data version.
func matchProp(prop string, r rune) bool {
	switch prop {
	case "d":
		return r >= '0' && r <= '9'
	case "w":
		if r < 0x80 {
			return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') ||
				(r >= 'A' && r <= 'Z') || r == '_'
		}
		return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
	case "s":
		switch r {
		case ' ', '\t', '\n', '\r', '\f', '\v', 0x85, 0xA0:
			return true
		}
		return unicode.Is(unicode.Zs, r)
	case "h":
		return r == '\t' || r == ' ' || unicode.Is(unicode.Zs, r)
	case "Vspace":
		switch r {
		case '\n', '\r', '\v', '\f', 0x85, 0x2028, 0x2029:
			return true
		}
		return false
	case "alnum":
		return unicode.IsLetter(r) || unicode.IsDigit(r)
	case "alpha":
		return unicode.IsLetter(r)
	case "xdigit":
		return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
	case "upper":
		return unicode.IsUpper(r)
	case "lower":
		return unicode.IsLower(r)
	case "cntrl":
		return unicode.IsControl(r)
	case "graph":
		return unicode.IsGraphic(r) && !unicode.IsSpace(r) && !unicode.IsControl(r)
	case "print":
		return unicode.IsPrint(r)
	case "punct":
		return unicode.IsPunct(r)
	}
	return false
}

func isWordByte(r rune) bool {
	if r < 0x80 {
		return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') || r == '_'
	}
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

// matchAnchor evaluates a zero-width assertion at byte position pos.
func matchAnchor(kind syntax.AnchorKind, in *Input, pos int, flags uint8) bool {
	n := len(in.Bytes)
	switch kind {
	case syntax.AnchorStart:
		return pos == 0
	case syntax.AnchorEnd:
		return pos == n
	case syntax.AnchorEndHalf:
		if pos == n {
			return true
		}
		return pos == n-1 && in.Bytes[pos] == '\n'
	case syntax.AnchorLineStart:
		if pos == 0 {
			return true
		}
		return flags&syntax.FlagM != 0 && in.Bytes[pos-1] == '\n'
	case syntax.AnchorLineEnd:
		if pos == n {
			return true
		}
		if in.Bytes[pos] != '\n' {
			return false
		}
		if flags&syntax.FlagM != 0 {
			return true
		}
		return pos == n-1
	case syntax.AnchorWordBoundary, syntax.AnchorNotWordBoundary:
		var before, after bool
		if pos > 0 {
			r, _ := utf8DecodeBefore(in.Bytes, pos)
			before = isWordByte(r)
		}
		if pos < n {
			r, _ := in.DecodeRune(pos)
			after = isWordByte(r)
		}
		b := before != after
		if kind == syntax.AnchorNotWordBoundary {
			return !b
		}
		return b
	}
	return false
}

// utf8DecodeBefore decodes the rune ending exactly at byte position pos.
func utf8DecodeBefore(b []byte, pos int) (rune, int) {
	if pos <= 0 {
		return utf8.RuneError, 0
	}
	// Rune start bytes: 0xxxxxxx, 110xxxxx, 1110xxxx, 11110xxx.
	start := pos - 1
	for start > 0 && start > pos-4 && b[start]&0xC0 == 0x80 {
		start--
	}
	r, sz := utf8.DecodeRune(b[start:])
	if start+sz != pos {
		return utf8.RuneError, 1
	}
	return r, sz
}
