package syntax

import (
	"fmt"
	"unicode/utf8"
)

// ParseError describes a syntax failure with a byte offset.
type ParseError struct {
	Msg string
	Off int
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("syntax error at byte %d: %s", e.Off, e.Msg)
}

type patAtom struct {
	r   rune
	bad bool
}

func decodeAtoms(s string) []patAtom {
	var out []patAtom
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			out = append(out, patAtom{r: utf8.RuneError, bad: true})
			i++
			continue
		}
		out = append(out, patAtom{r: r})
		i += size
	}
	return out
}

type parser struct {
	atoms []patAtom
	src   string
	pos   int
	off   []int
	flags Flags
	caps  int
	names map[string]int
}

// Parse compiles pattern text into a tagged AST.
func Parse(pattern string) (*Program, error) {
	atoms := decodeAtoms(pattern)
	offs := make([]int, len(atoms))
	b := 0
	for i, a := range atoms {
		offs[i] = b
		if a.bad {
			b++
		} else {
			b += utf8.RuneLen(a.r)
		}
	}
	p := &parser{atoms: atoms, src: pattern, off: offs, names: map[string]int{}}
	root, err := p.parseRe()
	if err != nil {
		return nil, err
	}
	if p.pos < len(atoms) {
		return nil, &ParseError{Msg: "unexpected ')'", Off: p.curOff()}
	}
	if err := resolveRefs(root, p.caps, p.names); err != nil {
		return nil, err
	}
	var nextID int
	assignIDs(root, &nextID)
	return &Program{
		Pattern: pattern, Root: root, NumCaps: p.caps, Names: p.names,
		SyntaxV: SyntaxVersion, UnicodeV: UnicodeVersion,
	}, nil
}

func (p *parser) curOff() int {
	if p.pos < len(p.off) {
		return p.off[p.pos]
	}
	return len(p.src)
}

func (p *parser) span(startAtom int) (int, int) {
	return p.off[startAtom], p.curOff()
}

func (p *parser) peek() (patAtom, bool) {
	if p.pos >= len(p.atoms) {
		return patAtom{}, false
	}
	return p.atoms[p.pos], true
}

func (p *parser) eat() patAtom {
	a := p.atoms[p.pos]
	p.pos++
	return a
}

func (p *parser) errf(off int, f string, args ...any) error {
	return &ParseError{Msg: fmt.Sprintf(f, args...), Off: off}
}

func isQuantRune(r rune) bool { return r == '*' || r == '+' || r == '?' || r == '{' }

func (p *parser) parseRe() (Node, error) {
	var branches []Node
	start := p.pos
	for {
		node, err := p.parseConcat()
		if err != nil {
			return nil, err
		}
		branches = append(branches, node)
		a, ok := p.peek()
		if ok && !a.bad && a.r == '|' {
			p.eat()
			continue
		}
		break
	}
	so, eo := p.span(start)
	if len(branches) == 1 {
		return branches[0], nil
	}
	return &Alternation{baseNode: baseNode{off: so, end: eo}, Branches: branches}, nil
}

func (p *parser) parseConcat() (Node, error) {
	var items []Node
	start := p.pos
	for {
		a, ok := p.peek()
		if !ok || (!a.bad && (a.r == '|' || a.r == ')')) {
			break
		}
		atom, err := p.parseAtom()
		if err != nil {
			return nil, err
		}
		items = append(items, atom)
	}
	so, eo := p.span(start)
	if len(items) == 0 {
		return &Empty{baseNode: baseNode{off: so, end: eo}}, nil
	}
	if len(items) == 1 {
		return items[0], nil
	}
	return &Concat{baseNode: baseNode{off: so, end: eo}, Items: items}, nil
}

func (p *parser) parseAtom() (Node, error) {
	a := p.eat()
	startAtom := p.pos - 1
	so := p.off[startAtom]
	switch {
	case a.bad:
		n := &Literal{R: utf8.RuneError, Bad: true, Flags: p.flags,
			baseNode: baseNode{off: so, end: so + 1}}
		return p.maybeQuant(n)
	case a.r == '.':
		c := &Class{Dot: true, Flags: p.flags, baseNode: baseNode{off: so, end: p.curOff()}}
		return p.maybeQuant(c)
	case a.r == '(':
		return p.parseGroup(startAtom)
	case a.r == '[':
		c, err := p.parseClass(startAtom)
		if err != nil {
			return nil, err
		}
		return p.maybeQuant(c)
	case a.r == '^':
		return &Anchor{Kind: AnchorStartLine, Flags: p.flags,
			baseNode: baseNode{off: so, end: p.curOff()}}, nil
	case a.r == '$':
		return &Anchor{Kind: AnchorEndLine, Flags: p.flags,
			baseNode: baseNode{off: so, end: p.curOff()}}, nil
	case a.r == '\\':
		return p.parseEscape(startAtom)
	case a.r == '*' || a.r == '+' || a.r == '?' || a.r == '}':
		return nil, p.errf(so, "%q with nothing to repeat", string(a.r))
	default:
		n := &Literal{R: a.r, Flags: p.flags,
			baseNode: baseNode{off: so, end: p.curOff()}}
		return p.maybeQuant(n)
	}
}

func (p *parser) maybeQuant(child Node) (Node, error) {
	a, ok := p.peek()
	if !ok || a.bad || !isQuantRune(a.r) {
		return child, nil
	}
	so := p.curOff()
	q := &Repeat{Child: child, Greedy: true, baseNode: baseNode{off: so}}
	switch a.r {
	case '*':
		p.eat()
		q.Min, q.Max = 0, -1
	case '+':
		p.eat()
		q.Min, q.Max = 1, -1
	case '?':
		p.eat()
		q.Min, q.Max = 0, 1
	case '{':
		mn, mx, bounded, err := p.parseBrace(so)
		if err != nil {
			return nil, err
		}
		if !bounded {
			return child, nil
		}
		q.Min, q.Max = mn, mx
	}
	q.end = p.curOff()
	if b, ok := p.peek(); ok && !b.bad && (b.r == '?' || b.r == '+') {
		suffix := p.eat()
		if suffix.r == '?' {
			q.Greedy = false
			q.end = p.curOff()
		} else {
			q.end = p.curOff()
			return &Group{Body: q, Atomic: true, Flags: p.flags,
				baseNode: baseNode{off: so, end: p.curOff()}}, nil
		}
	}
	return q, nil
}

func (p *parser) parseBrace(braceOff int) (int, int, bool, error) {
	p.eat() // '{'
	readNum := func() (int, bool) {
		a, ok := p.peek()
		if !ok || a.bad || a.r < '0' || a.r > '9' {
			return 0, false
		}
		n := 0
		for {
			a, ok := p.peek()
			if !ok || a.bad || a.r < '0' || a.r > '9' {
				return n, true
			}
			n = n*10 + int(a.r-'0')
			p.eat()
		}
	}
	mn, ok1 := readNum()
	if !ok1 {
		p.rewindTo(braceOff)
		return 0, 0, false, nil
	}
	mx := mn
	a, ok := p.peek()
	if ok && !a.bad && a.r == ',' {
		p.eat()
		mx = -1
		if n2, ok := readNum(); ok {
			mx = n2
		}
		a, ok = p.peek()
	}
	if !ok || a.bad || a.r != '}' {
		p.rewindTo(braceOff)
		return 0, 0, false, nil
	}
	p.eat()
	if mx >= 0 && mx < mn {
		return 0, 0, false, p.errf(braceOff, "invalid repetition: min > max")
	}
	return mn, mx, true, nil
}

func (p *parser) rewindTo(off int) {
	for p.pos > 0 && p.off[p.pos] >= off {
		p.pos--
	}
}
