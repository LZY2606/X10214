package vm

import (
	"unicode"

	"trajex/internal/charsets"
	"trajex/internal/syntax"
)

func classMatches(cls *syntax.Class, r rune, fold bool) bool {
	inside := false
	for _, it := range cls.Items {
		switch it.Kind {
		case 'l':
			if it.R1 == r || fold && foldContains(it.R1, r) {
				inside = true
			}
		case 'r':
			if r >= it.R1 && r <= it.R2 || fold && foldRangeHit(it.R1, it.R2, r) {
				inside = true
			}
		case 'p':
			if charsets.Perl(it.Perl, it.Negated, r) {
				inside = true
			}
		case 'u':
			ok := charsets.UnicodeProperty(it.Property, r)
			if it.NegProp {
				ok = !ok
			}
			if ok {
				inside = true
			}
		}
	}
	if cls.Negated {
		// Negated classes never match newline-by-default '.' semantics; they
		// still reject invalid end-of-input (handled by caller).
		return !inside
	}
	return inside
}

func foldContains(want, r rune) bool {
	for _, f := range charsets.FoldRunes(want) {
		if f == r {
			return true
		}
	}
	return false
}

func foldRangeHit(lo, hi, r rune) bool {
	for _, f := range charsets.FoldRunes(r) {
		if f >= lo && f <= hi {
			return true
		}
	}
	return false
}

func isWordByte(r rune) bool {
	if r < 128 {
		return charsets.IsASCIIWord(r)
	}
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}
