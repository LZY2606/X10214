package rx

import (
	"strings"
	"unicode"
)

// MatchCharClass evaluates a bracket expression against r at code-point
// position p. p < 0 represents a virtual boundary (used defensively).
func MatchCharClass(cc *CharClass, r rune, fold bool) bool {
	matched := false
	if fold {
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			if classPositive(cc, f) {
				matched = true
				break
			}
		}
	}
	if !matched {
		matched = classPositive(cc, r)
	}
	if cc.Negate {
		return !matched
	}
	return matched
}

func classPositive(cc *CharClass, r rune) bool {
	for _, lit := range cc.Literals {
		if r == lit {
			return true
		}
	}
	for _, rg := range cc.Ranges {
		if r >= rg.Lo && r <= rg.Hi {
			return true
		}
	}
	for _, sh := range cc.POSIX {
		if matchPOSIX(sh, r) {
			return true
		}
	}
	for _, pr := range cc.Props {
		if matchProperty(pr, r) {
			return true
		}
	}
	return false
}

// matchPOSIX handles both POSIX bracket names and the \d \w \s shorthands.
func matchPOSIX(item POSIXItem, r rune) bool {
	var ok bool
	switch item.Name {
	case "d":
		ok = unicode.IsDigit(r)
	case "D":
		ok = !unicode.IsDigit(r)
	case "w":
		ok = unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
	case "W":
		ok = !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_')
	case "s":
		ok = unicode.IsSpace(r)
	case "S":
		ok = !unicode.IsSpace(r)
	case "alnum":
		ok = unicode.IsLetter(r) || unicode.IsDigit(r)
	case "alpha":
		ok = unicode.IsLetter(r)
	case "blank":
		ok = r == '\t' || r == ' '
	case "cntrl":
		ok = unicode.IsControl(r)
	case "digit":
		ok = unicode.IsDigit(r)
	case "graph":
		ok = unicode.IsOneOf(unicode.GraphicRanges, r) && r != ' '
	case "lower":
		ok = unicode.IsLower(r)
	case "print":
		ok = unicode.IsPrint(r)
	case "punct":
		ok = unicode.IsPunct(r)
	case "space":
		ok = unicode.IsSpace(r)
	case "upper":
		ok = unicode.IsUpper(r)
	case "xdigit":
		ok = isXDigit(r)
	default:
		ok = false
	}
	if item.Negate {
		return !ok
	}
	return ok
}

func isXDigit(r rune) bool {
	switch {
	case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		return true
	}
	return false
}

// matchProperty resolves \p{Name}. Supported: binary properties (Alphabetic,
// White_Space, ...), general categories (L, Ll, ...) and script names.
func matchProperty(item PropItem, r rune) bool {
	ok := propertyMatches(item.Name, r)
	if item.Negate {
		return !ok
	}
	return ok
}

func propertyMatches(name string, r rune) bool {
	if name == "Any" {
		return true
	}
	if strings.EqualFold(name, "ASCII") {
		return r >= 0 && r <= 0x7F
	}
	n := strings.ReplaceAll(name, " ", "_")
	n = strings.ReplaceAll(n, "-", "_")
	key := n
	if t, ok := unicode.Categories[key]; ok {
		return unicode.Is(t, r)
	}
	// Two-letter compact categories map onto the first-letter table.
	if len(n) == 2 {
		if t, ok := unicode.Categories[strings.ToUpper(n)]; ok {
			if unicode.Is(t, r) {
				return true
			}
		}
	}
	if t, ok := unicode.Scripts[canonScript(key)]; ok {
		return unicode.Is(t, r)
	}
	if t, ok := binaryProps[key]; ok {
		return unicode.Is(t, r)
	}
	return false
}

func canonScript(name string) string {
	// Go tables use title-cased names with underscores, e.g. Latin_Extended_A.
	parts := strings.Split(name, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
	}
	return strings.Join(parts, "_")
}
