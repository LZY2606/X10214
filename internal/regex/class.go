package regex

import (
	"unicode"
	"unicode/utf8"
)

// BadRune is the synthetic rune used for an invalid UTF-8 byte. The executor
// works on a sequence of "units": valid runes decoded normally and exactly one
// BadRune per malformed byte, so byte positions and code-point positions stay
// simultaneously addressable.
const BadRune = utf8.RuneError

func isWord(r rune) bool {
	return r == '_' ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') ||
		r >= utf8.RuneSelf
}

func isSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x85, 0xA0:
		return true
	}
	return false
}

// elemMatch reports whether a single unit satisfies one class element.
func elemMatch(e ClassElem, r rune) bool {
	switch e.Named {
	case "d":
		return r >= '0' && r <= '9'
	case "D":
		return !(r >= '0' && r <= '9')
	case "w":
		return isWord(r)
	case "W":
		return !isWord(r)
	case "s":
		return isSpace(r)
	case "S":
		return !isSpace(r)
	case "p":
		return propMatch(e.Prop, r)
	}
	return r >= e.Lo && r <= e.Hi
}

// foldCandidates enumerates r and all of its simple case-fold equivalents.
func foldCandidates(r rune) []rune {
	out := []rune{r}
	if r == BadRune {
		return out
	}
	f := unicode.SimpleFold(r)
	for f != r {
		out = append(out, f)
		f = unicode.SimpleFold(f)
	}
	return out
}

// Match tests a unit against the class, honoring case-insensitive matching by
// considering the whole fold-equivalence class of the input unit.
func (c *CharClass) Match(r rune, ci bool) bool {
	cands := []rune{r}
	if ci {
		cands = foldCandidates(r)
	}
	hit := false
outer:
	for _, cand := range cands {
		for i := range c.Elems {
			if elemMatch(c.Elems[i], cand) {
				hit = true
				break outer
			}
		}
	}
	if c.Negate {
		// A literal range inside a negated class also folds; \W etc. already
		// encode their own complements, so only ranges participate here.
		return !hit
	}
	return hit
}

// propMatch evaluates \p{...}: general categories, selected scripts, Any/ASCII.
func propMatch(prop string, r rune) bool {
	switch prop {
	case "Any":
		return true
	case "ASCII", "ascii":
		return r < utf8.RuneSelf
	case "Latin":
		return unicode.Is(unicode.Latin, r)
	case "Greek":
		return unicode.Is(unicode.Greek, r)
	case "Han":
		return unicode.Is(unicode.Han, r)
	case "Hiragana":
		return unicode.Is(unicode.Hiragana, r)
	case "Katakana":
		return unicode.Is(unicode.Katakana, r)
	case "Common":
		return unicode.Is(unicode.Common, r)
	}
	if t, ok := catTable[prop]; ok {
		return unicode.Is(t, r)
	}
	if ranges, ok := unicode.Categories[prop]; ok {
		return unicode.Is(ranges, r)
	}
	if ranges, ok := unicode.Scripts[prop]; ok {
		return unicode.Is(ranges, r)
	}
	return false
}

var catTable = map[string]*unicode.RangeTable{
	"C": unicode.C, "Cc": unicode.Cc, "Cf": unicode.Cf, "Co": unicode.Co, "Cs": unicode.Cs, "Cn": unicode.Cn,
	"L": unicode.L, "Ll": unicode.Ll, "Lm": unicode.Lm, "Lo": unicode.Lo, "Lt": unicode.Lt, "Lu": unicode.Lu,
	"M": unicode.M, "Mc": unicode.Mc, "Me": unicode.Me, "Mn": unicode.Mn,
	"N": unicode.N, "Nd": unicode.Nd, "Nl": unicode.Nl, "No": unicode.No,
	"P": unicode.P, "Pc": unicode.Pc, "Pd": unicode.Pd, "Pe": unicode.Pe, "Pf": unicode.Pf, "Pi": unicode.Pi, "Po": unicode.Po, "Ps": unicode.Ps,
	"S": unicode.S, "Sc": unicode.Sc, "Sk": unicode.Sk, "Sm": unicode.Sm, "So": unicode.So,
	"Z": unicode.Z, "Zl": unicode.Zl, "Zp": unicode.Zp, "Zs": unicode.Zs,
}
