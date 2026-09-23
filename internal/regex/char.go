package regex

import (
	"unicode"
)

// propRange maps a property name/alias to a membership predicate.
func propRange(name string) (func(rune) bool, bool) {
	tab, ok := propertyTable[name]
	return tab, ok
}

var propertyTable = map[string]func(rune) bool{
	"L":       unicode.IsLetter,
	"Letter":  unicode.IsLetter,
	"N":       unicode.IsNumber,
	"Number":  unicode.IsNumber,
	"P":       unicode.IsPunct,
	"Punct":   unicode.IsPunct,
	"S":       unicode.IsSymbol,
	"Symbol":  unicode.IsSymbol,
	"Z":       unicode.IsSpace,
	"Space":   unicode.IsSpace,
	"C":       unicode.IsControl,
	"Control": unicode.IsControl,
	"M":       unicode.IsMark,
	"Mark":    unicode.IsMark,
}

func isPerl(kind byte, r rune) bool {
	switch kind {
	case 'd':
		return unicode.IsDigit(r)
	case 'w':
		return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
	case 's':
		return unicode.IsSpace(r)
	}
	return false
}

// elemMatch reports whether r satisfies one class element.
func elemMatch(e ClassElem, r rune) bool {
	var base bool
	switch {
	case e.Perl != 0:
		base = isPerl(e.Perl, r)
	case e.Prop != "":
		f, ok := propRange(e.Prop)
		if !ok {
			return false
		}
		base = f(r)
	default:
		base = r >= e.Lo && r <= e.Hi
	}
	if e.NegRange {
		return !base
	}
	return base
}

// foldEqual reports case-insensitive equality using Unicode simple folds.
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
