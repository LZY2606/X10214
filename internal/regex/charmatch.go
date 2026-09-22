package regex

import (
	"unicode"
	"unicode/utf8"
)

// classMatcher evaluates a compiled character class against code points.
type classMatcher struct {
	ranges  []RuneRange
	negated bool
	ci      bool
}

func compileClass(n *Node) classMatcher {
	return classMatcher{ranges: n.Ranges, negated: n.Negated, ci: n.CI}
}

// match reports whether r belongs to the class. invalid marks r as RuneError
// produced by an invalid byte (width 1), distinct from the real U+FFFD.
func (c classMatcher) match(r rune, invalid bool) bool {
	hit := false
	for _, rr := range c.ranges {
		if rr.Kind != "" {
			if matchBuiltin(rr, r, invalid) {
				hit = true
				break
			}
			continue
		}
		if r >= rr.Lo && r <= rr.Hi {
			hit = true
			break
		}
		if c.ci && foldInRange(rr, r) {
			hit = true
			break
		}
	}
	if c.negated {
		hit = !hit
	}
	return hit
}

func foldInRange(rr RuneRange, r rune) bool {
	folded := unicode.SimpleFold(r)
	for folded != r {
		if folded >= rr.Lo && folded <= rr.Hi {
			return true
		}
		folded = unicode.SimpleFold(folded)
	}
	return false
}

func matchBuiltin(rr RuneRange, r rune, invalid bool) bool {
	var ok bool
	switch {
	case rr.Kind == "digit":
		ok = unicode.IsDigit(r)
	case rr.Kind == "word":
		ok = wordRune(r)
	case rr.Kind == "space":
		ok = unicode.IsSpace(r)
	case len(rr.Kind) > 8 && rr.Kind[:8] == "unicode:":
		ok = invalid == false && matchProp(rr.Kind[8:], r)
	}
	if rr.Neg {
		ok = !ok
	}
	return ok
}

func wordRune(r rune) bool {
	if r == '_' {
		return true
	}
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r)
}

// matchProp maps the normalized property name (lowercase, no separators) onto
// the Go standard library Unicode tables which are pinned via UnicodeDataVersion.
func matchProp(name string, r rune) bool {
	if table, ok := unicodeCategories[name]; ok {
		return table(r)
	}
	if script, ok := unicodeScripts[name]; ok {
		return unicode.Is(script, r)
	}
	if t, ok := unicode.Categories["L"]; ok && name == "anyletter" {
		return unicode.Is(t, r)
	}
	return false
}

// validProperty checks property-name existence at compile time.
func validProperty(name string) bool {
	if _, ok := unicodeCategories[name]; ok {
		return true
	}
	if _, ok := unicodeScripts[name]; ok {
		return true
	}
	return false
}

func decodeAt(input []byte, bytePos int) (r rune, width int, invalid bool) {
	if bytePos >= len(input) {
		return utf8.RuneError, 0, false
	}
	r, w := utf8.DecodeRune(input[bytePos:])
	if r == utf8.RuneError && w == 1 {
		return utf8.RuneError, 1, true
	}
	return r, w, false
}

// caseFoldEqual compares two input substrings by Unicode simple case folding.
func caseFoldEqual(a []byte, b []byte) bool {
	for len(a) > 0 || len(b) > 0 {
		ra, wa, ia := decodeAt(a, 0)
		rb, wb, ib := decodeAt(b, 0)
		if ia || ib {
			if !ia && ib == false {
			}
			if wa != wb || (len(a) > 0 && len(b) > 0 && a[0] != b[0]) {
				return false
			}
			a, b = a[wa:], b[wb:]
			continue
		}
		if !foldEqual(ra, rb) {
			return false
		}
		a, b = a[wa:], b[wb:]
	}
	return len(a) == 0 && len(b) == 0
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
