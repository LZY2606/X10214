package engine

import (
	"unicode"
)

func flagOn(flags, f byte) bool {
	for i := 0; i < len(flags); i++ {
		if flags[i] == f {
			return true
		}
	}
	return false
}

// matchChar tests a slot rune against one compiled single-slot matcher.
func matchChar(spec *CharSpec, flags string, slot Slot) bool {
	if spec.IsClass {
		return matchTerms(spec.Terms, spec.Neg, flags, slot)
	}
	if flagOn(flags, 'i') {
		return foldEqual(spec.R, slot.R)
	}
	return spec.R == slot.R
}

func foldEqual(a, b rune) bool {
	if a == b {
		return true
	}
	for r := unicode.SimpleFold(a); r != a; r = unicode.SimpleFold(r) {
		if r == b {
			return true
		}
	}
	return false
}

func matchTerms(terms []ClassTerm, neg bool, flags string, slot Slot) bool {
	matched := false
	for _, t := range terms {
		if termMatches(t, flags, slot) {
			matched = true
			break
		}
	}
	if neg {
		return !matched
	}
	return matched
}

func termMatches(t ClassTerm, flags string, slot Slot) bool {
	r := slot.R
	switch t.Kind {
	case "rune":
		if flagOn(flags, 'i') {
			return foldEqual(t.R1, r)
		}
		return t.R1 == r
	case "range":
		if flagOn(flags, 'i') {
			if foldEqualRange(t.R1, t.R2, r) {
				return true
			}
		}
		return r >= t.R1 && r <= t.R2
	case "perl":
		return perlMatch(byte(t.R1), r, slot.Invalid)
	case "cat":
		neg := t.Neg
		rt := lookupRangeTable(t.Name)
		if rt == nil {
			return false
		}
		ok := unicode.In(r, rt)
		if neg {
			return !ok
		}
		return ok
	case "posix":
		return posixMatch(t.Name, r)
	}
	return false
}

func foldEqualRange(lo, hi, r rune) bool {
	// fold family of r may land inside range
	for x := unicode.SimpleFold(r); x != r; x = unicode.SimpleFold(x) {
		if x >= lo && x <= hi {
			return true
		}
	}
	return r >= lo && r <= hi
}

func perlMatch(c byte, r rune, invalid bool) bool {
	switch c {
	case 'd':
		return r >= '0' && r <= '9'
	case 'D':
		return !(r >= '0' && r <= '9')
	case 'w':
		return isASCIIWord(r)
	case 'W':
		return !isASCIIWord(r)
	case 's':
		return isASCIISpace(r) || (r == 0x85 || r == 0xA0 || unicode.Is(unicode.Zs, r))
	case 'S':
		return !(isASCIISpace(r) || r == 0x85 || r == 0xA0 || unicode.Is(unicode.Zs, r))
	}
	_ = invalid
	return false
}

func posixMatch(name string, r rune) bool {
	switch name {
	case "alpha":
		return unicode.IsLetter(r)
	case "digit":
		return unicode.IsDigit(r)
	case "alnum":
		return unicode.IsLetter(r) || unicode.IsDigit(r)
	case "space":
		return unicode.IsSpace(r)
	case "upper":
		return unicode.IsUpper(r)
	case "lower":
		return unicode.IsLower(r)
	case "punct":
		return unicode.IsPunct(r)
	case "xdigit":
		return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
	case "blank":
		return r == ' ' || r == '\t'
	case "cntrl":
		return unicode.IsControl(r)
	case "print", "graph":
		return unicode.IsPrint(r)
	}
	return false
}
