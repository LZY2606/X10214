package engine

import (
	"unicode"
)

// foldRunes returns the full simple-case-fold closure of r.
func foldRunes(r rune) []rune {
	out := []rune{r}
	seen := map[rune]bool{r: true}
	// unicode.SimpleFold walks the fold cycle both directions.
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		if seen[f] {
			break
		}
		seen[f] = true
		out = append(out, f)
	}
	// Also walk the opposite direction: SimpleFold only moves forward in the
	// cycle, so seed from every code point found until the cycle closes.
	frontier := append([]rune(nil), out...)
	for len(frontier) > 0 {
		seed := frontier[0]
		frontier = frontier[1:]
		f := unicode.SimpleFold(seed)
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
			frontier = append(frontier, f)
		}
	}
	return out
}

// foldEqual reports whether two runes match under simple case folding.
func foldEqual(a, b rune) bool {
	if a == b {
		return true
	}
	for _, f := range foldRunes(a) {
		if f == b {
			return true
		}
	}
	return false
}

func runeInRanges(r rune, rs []RuneRange) bool {
	for _, rr := range rs {
		if r >= rr.Lo && r <= rr.Hi {
			return true
		}
	}
	return false
}

// shorthandClass implements the ASCII-centric \w \d \s definitions, matching
// Go's regexp semantics. s1..s3 select the class; neg inverts this member.
func shorthandClass(name string, r rune) bool {
	var hit bool
	switch name {
	case "__d", "__D":
		hit = r >= '0' && r <= '9'
	case "__w", "__W":
		hit = r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_'
	case "__s", "__S":
		switch r {
		case '\t', '\n', '\f', '\r', ' ', 0x0B:
			hit = true
		}
		if r >= 0x80 && unicode.In(r, unicode.Zs) {
			hit = true
		}
		if r == 0x85 || r == 0xA0 || r == 0x2028 || r == 0x2029 {
			hit = true
		}
	}
	if len(name) == 3 && (name[2] == 'D' || name[2] == 'W' || name[2] == 'S') {
		hit = !hit
	}
	return hit
}

// classMatches tests one code point against a bracket expression.
func classMatches(cc *CharClass, r rune) bool {
	hit := false
	if runeInRanges(r, cc.Ranges) {
		hit = true
	}
	for _, pr := range cc.Props {
		var m bool
		if len(pr.Name) >= 2 && pr.Name[0] == '_' {
			m = shorthandClass(pr.Name, r)
		} else if rt, ok := lookupProperty(pr.Name); ok {
			m = unicode.In(r, rt)
		}
		if pr.Negated {
			m = !m
		}
		if m {
			hit = true
		}
	}
	if cc.CI {
		for _, f := range foldRunes(r) {
			if f == r {
				continue
			}
			if runeInRanges(f, cc.Ranges) {
				hit = true
			}
			for _, pr := range cc.Props {
				var m bool
				if len(pr.Name) >= 2 && pr.Name[0] == '_' {
					m = shorthandClass(pr.Name, f)
				} else if rt, ok := lookupProperty(pr.Name); ok {
					m = unicode.In(f, rt)
				}
				if pr.Negated {
					m = !m
				}
				if m {
					hit = true
				}
			}
		}
	}
	if cc.Negated {
		return !hit
	}
	return hit
}
