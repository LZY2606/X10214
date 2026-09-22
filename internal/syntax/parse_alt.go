package syntax

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// alternation := concat ('|' concat)*
func (p *parser) parseAlternation() (*Node, error) {
	first, err := p.parseConcat()
	if err != nil {
		return nil, err
	}
	var branches []*Node
	if first != nil {
		branches = append(branches, first)
	}
	for p.peek() == '|' {
		p.pos++
		b, err := p.parseConcat()
		if err != nil {
			return nil, err
		}
		if b == nil {
			b = p.newNode(KindEmpty)
		}
		branches = append(branches, b)
	}
	if len(branches) == 0 {
		return nil, nil
	}
	if len(branches) == 1 {
		return branches[0], nil
	}
	n := p.newNode(KindAlt)
	n.Children = branches
	return n, nil
}

// concat := quantified*
func (p *parser) parseConcat() (*Node, error) {
	var kids []*Node
	for {
		c := p.peek()
		if c == 0 || c == '|' || c == ')' {
			break
		}
		a, err := p.parseQuantified()
		if err != nil {
			return nil, err
		}
		if a != nil {
			kids = append(kids, a)
		}
	}
	if len(kids) == 0 {
		return nil, nil
	}
	if len(kids) == 1 {
		return kids[0], nil
	}
	n := p.newNode(KindConcat)
	n.Children = kids
	return n, nil
}

// quantified := atom quantifier?
func (p *parser) parseQuantified() (*Node, error) {
	atom, err := p.parseAtom()
	if err != nil {
		return nil, err
	}
	c := p.peek()
	if c != '*' && c != '+' && c != '?' && c != '{' {
		return atom, nil
	}
	if c == '{' {
		// Only treat {n,m} as a quantifier when it is well-formed
		// immediately following the atom; otherwise it is literal text.
		save := p.pos
		min, max, ok, err := p.tryBraceRepeat()
		if err != nil {
			return nil, err
		}
		if !ok {
			p.pos = save
			return atom, nil
		}
		return p.wrapRepeat(atom, min, max), nil
	}
	if isQuantifiedNode(atom) {
		return nil, p.errf("nothing to repeat")
	}
	p.pos++
	min, max := 0, -1
	switch c {
	case '+':
		min, max = 1, -1
	case '?':
		min, max = 0, 1
	}
	return p.wrapRepeat(atom, min, max), nil
}

func isQuantifiedNode(n *Node) bool {
	return n.Kind == KindRepeat
}

func (p *parser) wrapRepeat(child *Node, min, max int) *Node {
	greedy := true
	possess := false
	if p.peek() == '?' {
		p.pos++
		greedy = false
	} else if p.peek() == '+' {
		p.pos++
		possess = true
	}
	n := p.newNode(KindRepeat)
	n.Child = child
	n.Min = min
	n.Max = max
	n.Greedy = greedy
	n.Possess = possess
	return n
}

// tryBraceRepeat parses {n}, {n,}, {n,m}; returns ok=false to rewind.
func (p *parser) tryBraceRepeat() (min, max int, ok bool, err error) {
	start := p.pos
	p.pos++ // consume '{'
	digits := func() (int, bool) {
		begin := p.pos
		for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
			p.pos++
		}
		if p.pos == begin {
			return 0, false
		}
		v, _ := strconv.Atoi(p.src[begin:p.pos])
		return v, true
	}
	lo, hasLo := digits()
	if !hasLo {
		p.pos = start
		return 0, 0, false, nil
	}
	hi, hasHi := lo, true
	if p.peek() == ',' {
		p.pos++
		hasHi = false
		if v, ok := digits(); ok {
			hi, hasHi = v, true
		}
	}
	if p.peek() != '}' {
		p.pos = start
		return 0, 0, false, nil
	}
	p.pos++
	if hasHi && hi < lo {
		return 0, 0, false, p.errf("repeat range %d > %d", lo, hi)
	}
	if !hasHi {
		hi = -1
	}
	return lo, hi, true, nil
}

func (p *parser) peek() byte {
	if p.pos >= len(p.src) {
		return 0
	}
	return p.src[p.pos]
}

var _ = strings.TrimSpace
var _ = utf8.RuneLen
