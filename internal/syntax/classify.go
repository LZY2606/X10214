package syntax

import (
	"unicode"
)

func posixSupported(name string) bool {
	switch name {
	case "alnum", "alpha", "blank", "cntrl", "digit", "graph",
		"lower", "print", "punct", "space", "upper", "xdigit":
		return true
	}
	return false
}

// ClassMatch reports whether atom satisfies one Range element.
// Invalid UTF-8 bytes only match byte-valued intervals/classes; they never
// match Unicode predicates.
func ClassMatch(rng *Range, a Atom, caseFold bool) bool {
	if rng.Class != "" {
		return classPredicate(rng.Class, a)
	}
	if !inInterval(rng, a) {
		return false
	}
	if caseFold && a.RawByte < 0 {
		return true // interval membership already; folding handled by folding the input instead
	}
	return true
}

func inInterval(rng *Range, a Atom) bool {
	if a.RawByte >= 0 {
		return rng.Lo.RawByte >= 0 && byte(rng.Lo.RawByte) <= byte(a.RawByte) &&
			byte(a.RawByte) <= byte(rng.Hi.RawByte)
	}
	if rng.Lo.RawByte >= 0 {
		return false
	}
	return rng.Lo.R <= a.R && a.R <= rng.Hi.R
}

// MatchClass evaluates a full (possibly negated) class node against an atom.
func MatchClass(n *Node, a Atom) bool {
	return EvalClass(n.Ranges, n.Negated, n.CaseFold, a)
}

// EvalClass evaluates a list of Range predicates with outer negation.
func EvalClass(ranges []*Range, negated, caseFold bool, a Atom) bool {
	hit := false
	if caseFold && a.RawByte < 0 {
		folds := foldSet(a.R)
		for _, fr := range folds {
			if anyRange(ranges, Atom{R: fr, RawByte: -1}) {
				hit = true
				break
			}
		}
	} else {
		hit = anyRange(ranges, a)
	}
	if negated {
		// negated classes never match a newline? PCRE negated classes DO match
		// newline; we keep that explicit behavior.
		return !hit
	}
	return hit
}

func anyRange(ranges []*Range, a Atom) bool {
	for _, r := range ranges {
		if ClassMatch(r, a, false) {
			return true
		}
	}
	return false
}

// foldSet returns the set of runes equivalent under simple case folding:
// the rune itself plus every SimpleFold orbit partner.
func foldSet(r rune) []rune {
	out := []rune{r}
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		out = append(out, f)
		if f == r {
			break
		}
	}
	return out
}

func classPredicate(class string, a Atom) bool {
	if a.RawByte >= 0 {
		// only ASCII-aware shortcut classes can match a raw byte
		b := byte(a.RawByte)
		switch class {
		case "d":
			return b >= '0' && b <= '9'
		case "D":
			return !asciiDigit(b)
		case "w":
			return asciiWord(b)
		case "W":
			return !asciiWord(b)
		case "s":
			return asciiSpace(b)
		case "S":
			return !asciiSpace(b)
		}
		return false
	}
	switch class {
	case "d":
		return unicode.IsDigit(a.R)
	case "D":
		return !unicode.IsDigit(a.R)
	case "w":
		return unicode.IsLetter(a.R) || unicode.IsDigit(a.R) || a.R == '_'
	case "W":
		return !(unicode.IsLetter(a.R) || unicode.IsDigit(a.R) || a.R == '_')
	case "s":
		return unicode.IsSpace(a.R)
	case "S":
		return !unicode.IsSpace(a.R)
	}
	if len(class) >= 4 && class[:2] == "[:" && class[len(class)-2:] == ":]" {
		return posixMatch(class[2:len(class)-2], a.R)
	}
	return false
}

func asciiDigit(b byte) bool { return b >= '0' && b <= '9' }
func asciiWord(b byte) bool {
	return asciiDigit(b) || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b == '_'
}
func asciiSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	}
	return false
}

func posixMatch(name string, r rune) bool {
	switch name {
	case "alnum":
		return unicode.IsLetter(r) || unicode.IsDigit(r)
	case "alpha":
		return unicode.IsLetter(r)
	case "blank":
		return r == '\t' || r == ' '
	case "cntrl":
		return unicode.IsControl(r)
	case "digit":
		return unicode.IsDigit(r)
	case "graph":
		return unicode.IsOneOf(unicode.GraphicRanges, r) && r != ' '
	case "lower":
		return unicode.IsLower(r)
	case "print":
		return unicode.IsPrint(r)
	case "punct":
		return unicode.IsPunct(r)
	case "space":
		return unicode.IsSpace(r)
	case "upper":
		return unicode.IsUpper(r)
	case "xdigit":
		return asciiHex(byte(r)) || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F'
	}
	return false
}

func asciiHex(b byte) bool { return asciiDigit(b) }
