package regex

import (
	"fmt"
	"unicode"
)

// Predefined Perl classes as rune-range tables.
var (
	perlDigit = &Class{Ranges: [][2]rune{{'0', '9'}}}
	perlWord  = &Class{Ranges: [][2]rune{{'0', '9'}, {'A', 'Z'}, {'_', '_'}, {'a', 'z'}}}
	perlSpace = &Class{Ranges: [][2]rune{{'\t', '\n'}, {'\f', '\r'}, {' ', ' '}}}
)

func classNode(base *Class, negate bool) *Node {
	cl := &Class{Negate: negate, Ranges: append([][2]rune(nil), base.Ranges...)}
	return &Node{Kind: KClass, Class: cl}
}

// unicodeClass resolves \p{Name} against the Unicode tables shipped with the
// Go toolchain (categories and scripts). The data version is unicode.Version.
func unicodeClass(name string) (*Class, error) {
	if rt, ok := unicode.Categories[name]; ok {
		return classFromTable(rt), nil
	}
	if rt, ok := unicode.Scripts[name]; ok {
		return classFromTable(rt), nil
	}
	// Convenience aliases for single-letter category names.
	if len(name) == 1 {
		if rt, ok := unicode.Categories[name]; ok {
			return classFromTable(rt), nil
		}
	}
	return nil, fmt.Errorf("unknown Unicode class %q", name)
}

func classFromTable(rt *unicode.RangeTable) *Class {
	cl := &Class{}
	for _, r := range rt.R16 {
		lo, hi := rune(r.Lo), rune(r.Hi)
		stride := rune(r.Stride)
		for c := lo; c <= hi; c += stride {
			cl.Ranges = append(cl.Ranges, [2]rune{c, c})
		}
	}
	for _, r := range rt.R32 {
		lo, hi := rune(r.Lo), rune(r.Hi)
		stride := rune(r.Stride)
		for c := lo; c <= hi; c += stride {
			cl.Ranges = append(cl.Ranges, [2]rune{c, c})
		}
	}
	mergeRanges(cl)
	return cl
}

// mergeRanges sorts and coalesces overlapping/adjacent ranges.
func mergeRanges(cl *Class) {
	rs := cl.Ranges
	if len(rs) == 0 {
		return
	}
	sortRanges(rs)
	out := rs[:1]
	for _, r := range rs[1:] {
		last := &out[len(out)-1]
		if r[0] <= last[1]+1 {
			if r[1] > last[1] {
				last[1] = r[1]
			}
			continue
		}
		out = append(out, r)
	}
	cl.Ranges = out
}

func sortRanges(rs [][2]rune) {
	for i := 1; i < len(rs); i++ {
		for j := i; j > 0 && rs[j][0] < rs[j-1][0]; j-- {
			rs[j], rs[j-1] = rs[j-1], rs[j]
		}
	}
}

// parseClass parses a [...] character class; pos is just past '['.
func (p *parser) parseClass() (*Node, error) {
	cl := &Class{}
	if p.peek() == '^' {
		p.pos++
		cl.Negate = true
	}
	first := true
	for {
		r := p.peek()
		if r == -1 {
			return nil, p.errf("unterminated character class")
		}
		if r == ']' && !first {
			p.pos++
			break
		}
		first = false
		lo, loClass, err := p.parseClassItem()
		if err != nil {
			return nil, err
		}
		if loClass != nil {
			cl.Ranges = append(cl.Ranges, loClass.Ranges...)
			continue
		}
		// Possible range: lo '-' hi
		if p.peek() == '-' && p.pos+1 < len(p.src) && p.src[p.pos+1] != ']' {
			p.pos++
			hi, hiClass, err := p.parseClassItem()
			if err != nil {
				return nil, err
			}
			if hiClass != nil {
				return nil, p.errf("cannot use class as range endpoint")
			}
			if hi < lo {
				return nil, p.errf("invalid range %c-%c", lo, hi)
			}
			cl.Ranges = append(cl.Ranges, [2]rune{lo, hi})
			continue
		}
		cl.Ranges = append(cl.Ranges, [2]rune{lo, lo})
	}
	mergeRanges(cl)
	return &Node{Kind: KClass, Class: cl}, nil
}

// parseClassItem reads one class item: either a single rune or a nested
// class escape (\d, \p{...}).
func (p *parser) parseClassItem() (rune, *Class, error) {
	r := p.next()
	if r == -1 {
		return 0, nil, p.errf("unterminated character class")
	}
	if r != '\\' {
		return r, nil, nil
	}
	r2 := p.peek()
	if r2 == 'd' || r2 == 'D' || r2 == 'w' || r2 == 'W' || r2 == 's' || r2 == 'S' || r2 == 'p' || r2 == 'P' {
		n, err := p.parseEscape(true)
		if err != nil {
			return 0, nil, err
		}
		if n.Kind != KClass {
			return 0, nil, p.errf("escape not allowed in class")
		}
		if n.Class.Negate {
			// Materialize negation as complement over Unicode range.
			return 0, complementClass(n.Class), nil
		}
		return 0, n.Class, nil
	}
	n, err := p.parseEscape(true)
	if err != nil {
		return 0, nil, err
	}
	if n.Kind != KLiteral {
		return 0, nil, p.errf("escape not allowed as class item")
	}
	return n.Rune, nil, nil
}

// complementClass computes the complement of cl over [0, MaxRune].
func complementClass(cl *Class) *Class {
	out := &Class{}
	next := rune(0)
	for _, r := range cl.Ranges {
		if r[0] > next {
			out.Ranges = append(out.Ranges, [2]rune{next, r[0] - 1})
		}
		if r[1] >= next {
			next = r[1] + 1
		}
	}
	if next <= unicode.MaxRune {
		out.Ranges = append(out.Ranges, [2]rune{next, unicode.MaxRune})
	}
	return out
}

// classContains reports whether r is in cl (applying Negate).
func classContains(cl *Class, r rune) bool {
	in := false
	lo, hi := 0, len(cl.Ranges)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		rg := cl.Ranges[mid]
		switch {
		case r < rg[0]:
			hi = mid - 1
		case r > rg[1]:
			lo = mid + 1
		default:
			in = true
			lo, hi = 1, 0
		}
	}
	if cl.Negate {
		return !in
	}
	return in
}
