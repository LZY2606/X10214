package regex

import "sort"

// matcher decides whether one input element satisfies a class or char test.
type matcher interface {
	matches(code uint32) bool
}

type charMatcher struct {
	code rune
	fold bool
}

func (m charMatcher) matches(code uint32) bool {
	if code >= ElemInvalidBase {
		return false
	}
	r := rune(code)
	if r == m.code {
		return true
	}
	if m.fold {
		return foldEqual(r, m.code)
	}
	return false
}

type dotMatcher struct{ newline bool }

func (m dotMatcher) matches(code uint32) bool {
	if code >= ElemInvalidBase {
		return true // invalid byte is an element; dot consumes it
	}
	r := rune(code)
	if !m.newline && r == '\n' {
		return false
	}
	return true
}

// rangeTerm / propTerm are precompiled class terms.
type classMatcher struct {
	negated bool
	ranges  []rng
	props   []propSpec
	fold    bool
}

type rng struct{ lo, hi rune }

type propSpec struct {
	name string
	neg  bool
}

func buildClassMatcher(cl *Class, fl Flags) matcher {
	cm := &classMatcher{negated: cl.Negated, fold: fl.I}
	for _, t := range cl.Terms {
		switch t.Kind {
		case TermRange:
			cm.ranges = append(cm.ranges, rng{t.Lo, t.Hi})
		case TermProp:
			name := t.Prop
			neg := false
			if len(name) > 0 && name[0] == '^' {
				neg = true
				name = name[1:]
			}
			cm.props = append(cm.props, propSpec{name: name, neg: neg})
		}
	}
	sort.Slice(cm.ranges, func(i, j int) bool { return cm.ranges[i].lo < cm.ranges[j].lo })
	return cm
}

func (m *classMatcher) matches(code uint32) bool {
	hit := false
	if code < ElemInvalidBase {
		r := rune(code)
		i := sort.Search(len(m.ranges), func(i int) bool { return m.ranges[i].lo > r })
		if i > 0 && m.ranges[i-1].hi >= r {
			hit = true
		}
		if !hit {
			for _, pr := range m.props {
				ok := propMatches(pr.name, r)
				if pr.neg {
					ok = !ok
				}
				if ok {
					hit = true
					break
				}
			}
		}
		if !hit && m.fold {
			for c := simpleFold(r); c != r; c = simpleFold(c) {
				if m.literalHit(c) {
					hit = true
					break
				}
			}
		}
	}
	return hit != m.negated
}

func (m *classMatcher) literalHit(r rune) bool {
	i := sort.Search(len(m.ranges), func(i int) bool { return m.ranges[i].lo > r })
	if i > 0 && m.ranges[i-1].hi >= r {
		return true
	}
	for _, pr := range m.props {
		ok := propMatches(pr.name, r)
		if pr.neg {
			ok = !ok
		}
		if ok {
			return true
		}
	}
	return false
}

func propMatches(name string, r rune) bool {
	if name == "any" {
		return true
	}
	a := uniProps[name]
	if a == nil {
		return false
	}
	i := sort.Search(len(a), func(i int) bool { return a[i].Lo > r })
	return i > 0 && a[i-1].Hi >= r
}

// foldEqual reports whether two runes share a simple-case-fold class.
func foldEqual(a, b rune) bool {
	if a == b {
		return true
	}
	for r := simpleFold(a); r != a; r = simpleFold(r) {
		if r == b {
			return true
		}
	}
	return false
}

// simpleFold returns the next rune in the deterministic fold cycle.
func simpleFold(r rune) rune {
	if n, ok := foldNext[r]; ok {
		return n
	}
	return r
}

// isWordRune implements \w / word boundaries per flag set.
func isWordRune(code uint32, u bool) bool {
	if code >= ElemInvalidBase {
		return false
	}
	r := rune(code)
	if r == '_' {
		return true
	}
	if u {
		return propMatches("l", r) || propMatches("n", r)
	}
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
}

func isNewline(code uint32) bool {
	if code >= ElemInvalidBase {
		return false
	}
	switch rune(code) {
	case '\n', '\r', 0x85, 0x2028, 0x2029:
		return true
	}
	return false
}
