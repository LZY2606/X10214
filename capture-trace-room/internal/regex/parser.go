package regex

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

type parser struct {
	text       string
	pos        int
	groupCount int
	names      map[string]int
	idCounter  int
}

func (p *parser) errorf(format string, args ...any) error {
	return fmt.Errorf("regex parse error at byte %d: %s", p.pos, fmt.Sprintf(format, args...))
}

func (p *parser) newNode(k NodeKind, pos int) *Node {
	// IDs are assigned lazily through nextID on compilation-independent order.
	return &Node{Kind: k, Pos: pos}
}

func (p *parser) nextID() int {
	p.idCounter++
	return p.idCounter
}

// skipSpace ignores whitespace and # comments when free-spacing (x) is active.
func (p *parser) skipSpace(f *Flags) {
	if !f.X {
		return
	}
	for p.pos < len(p.text) {
		r, sz := utf8.DecodeRuneInString(p.text[p.pos:])
		if r == '\\' || (r != ' ' && r != '\t' && r != '\n' && r != '\r' && r != '\f' && r != '#') {
			return
		}
		if r == '#' {
			for p.pos < len(p.text) && p.text[p.pos] != '\n' {
				p.pos++
			}
			continue
		}
		p.pos += sz
	}
}

// parseExpr := alt ('|' alt)*
func (p *parser) parseExpr(f *Flags) (*Node, error) {
	start := p.pos
	branches := []*Node{}
	for {
		node, err := p.parseConcat(f)
		if err != nil {
			return nil, err
		}
		branches = append(branches, node)
		p.skipSpace(f)
		if p.pos >= len(p.text) || p.text[p.pos] != '|' {
			break
		}
		p.pos++
	}
	if len(branches) == 1 {
		return branches[0], nil
	}
	return &Node{Kind: NAlt, Pos: start, Children: branches}, nil
}

// parseConcat consumes quantified atoms until | ) or end.
func (p *parser) parseConcat(f *Flags) (*Node, error) {
	start := p.pos
	items := []*Node{}
	for {
		p.skipSpace(f)
		if p.pos >= len(p.text) {
			break
		}
		c := p.text[p.pos]
		if c == '|' || c == ')' {
			break
		}
		atom, err := p.parseAtom(f)
		if err != nil {
			return nil, err
		}
		quant, err := p.parseQuantifier(f, atom)
		if err != nil {
			return nil, err
		}
		items = append(items, quant)
	}
	switch len(items) {
	case 0:
		return &Node{Kind: NEmpty, Pos: start}, nil
	case 1:
		return items[0], nil
	default:
		return &Node{Kind: NConcat, Pos: start, Children: items}, nil
	}
}

// parseQuantifier wraps atom with a following ?, *, +, {n,m} (and laziness/possessiveness suffix).
func (p *parser) parseQuantifier(f *Flags, atom *Node) (*Node, error) {
	p.skipSpace(f)
	if p.pos >= len(p.text) {
		return atom, nil
	}
	var min, max int
	var hasQ bool
	pos := p.pos
	switch p.text[p.pos] {
	case '?':
		min, max, hasQ = 0, 1, true
		p.pos++
	case '*':
		min, max, hasQ = 0, -1, true
		p.pos++
	case '+':
		min, max, hasQ = 1, -1, true
		p.pos++
	case '{':
		if save, n, m, ok := p.tryBraceRepeat(); ok {
			p.pos = save
			min, max, hasQ = n, m, true
		}
	}
	if !hasQ {
		return atom, nil
	}
	if atom.Kind == NAnchor || atom.Kind == NLookaround {
		return nil, p.errorf("quantifier not allowed on zero-width token")
	}
	greedy, possessive := true, false
	p.skipSpace(f)
	if p.pos < len(p.text) {
		switch p.text[p.pos] {
		case '?':
			greedy = false
			p.pos++
		case '+':
			possessive = true
			p.pos++
		}
	}
	return &Node{Kind: NRepeat, Pos: pos, Min: min, Max: max, Greedy: greedy, Possessive: possessive, Children: []*Node{atom}}, nil
}

func (p *parser) tryBraceRepeat() (savePos, min, max int, ok bool) {
	save := p.pos
	j := save + 1
	readDigits := func() (int, bool) {
		start := j
		for j < len(p.text) && p.text[j] >= '0' && p.text[j] <= '9' {
			j++
		}
		if j == start {
			return 0, false
		}
		n, err := strconv.Atoi(p.text[start:j])
		if err != nil {
			return 0, false
		}
		return n, true
	}
	n, gotN := readDigits()
	if !gotN {
		return save, 0, 0, false
	}
	m := n
	if j < len(p.text) && p.text[j] == ',' {
		j++
		if j < len(p.text) && p.text[j] == '}' {
			m = -1
		} else {
			m2, gotM := readDigits()
			if !gotM {
				return save, 0, 0, false
			}
			m = m2
		}
	}
	if j >= len(p.text) || p.text[j] != '}' {
		return save, 0, 0, false
	}
	j++
	return j, n, m, true
}
