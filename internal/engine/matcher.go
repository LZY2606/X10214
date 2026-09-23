package engine

import (
	"unicode/utf8"

	"capturetrace/internal/syntax"
	"capturetrace/internal/ucd"
)

// Matcher decides whether one symbol belongs to a character class.
type Matcher struct {
	negated bool
	ranges  []runeRange
	shorts  []shortToken
	props   []propToken
}

type runeRange struct{ lo, hi rune }

type shortToken struct {
	kind byte // d w s
	neg  bool
}

type propToken struct {
	name string
	neg  bool
}

func compileMatcher(a *syntax.Atom) (*Matcher, error) {
	m := &Matcher{negated: a.Negated}
	for _, mem := range a.Members {
		switch {
		case mem.IsRange:
			m.ranges = append(m.ranges, runeRange{mem.Lo, mem.Hi})
		case mem.IsShort:
			m.shorts = append(m.shorts, shortToken{mem.Short, mem.NegProp})
		case mem.IsProp:
			if _, ok := ucd.LookupProperty(mem.Prop); !ok {
				return nil, &syntax.Error{Pos: a.Pos, Msg: "unknown Unicode property " + mem.Prop}
			}
			m.props = append(m.props, propToken{mem.Prop, mem.NegProp})
		default:
			m.ranges = append(m.ranges, runeRange{mem.Single, mem.Single})
		}
	}
	return m, nil
}

func compileProperty(a *syntax.Atom) (*Matcher, error) {
	if _, ok := ucd.LookupProperty(a.Prop); !ok {
		return nil, &syntax.Error{Pos: a.Pos, Msg: "unknown Unicode property " + a.Prop}
	}
	m := &Matcher{props: []propToken{{a.Prop, false}}, negated: a.Negated}
	return m, nil
}

// memberMatch reports membership in the positive member set. Invalid bytes
// never belong to any positive set; negated shorthands/properties match them.
func (m *Matcher) memberMatch(s Sym) bool {
	if s.Invalid {
		for _, t := range m.shorts {
			if t.neg {
				return true
			}
		}
		for _, t := range m.props {
			if t.neg {
				return true
			}
		}
		return false
	}
	r := s.R
	for _, iv := range m.ranges {
		if r >= iv.lo && r <= iv.hi {
			return true
		}
	}
	for _, t := range m.shorts {
		var base bool
		switch t.kind {
		case 'd':
			base = ucd.IsDecimalDigit(r)
		case 'w':
			base = ucd.IsWordChar(r)
		case 's':
			base = ucd.IsWhiteSpace(r)
		}
		if t.neg {
			base = !base
		}
		if base {
			return true
		}
	}
	for _, t := range m.props {
		hit := propHit(t.name, r)
		if t.neg {
			hit = !hit
		}
		if hit {
			return true
		}
	}
	return false
}

func propHit(name string, r rune) bool {
	rs, ok := ucd.LookupProperty(name)
	if !ok {
		return false
	}
	return rs.Has(r)
}

// Match applies the (possibly negated) class.
func (m *Matcher) Match(s Sym) bool {
	hit := m.memberMatch(s)
	if m.negated {
		return !hit
	}
	return hit
}

var _ = utf8.RuneError
