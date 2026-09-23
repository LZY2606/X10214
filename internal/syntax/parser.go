package syntax

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"trajex/internal/charsets"
)

// ParseError describes a grammar v1 parse failure.
type ParseError struct {
	Offset int
	Msg    string
}

func (e *ParseError) Error() string { return fmt.Sprintf("pattern error at %d: %s", e.Offset, e.Msg) }

type parser struct {
	src   string
	pos   int
	group int
	named map[string]int
}

// Parse parses src according to grammar v1.
func Parse(src string) (*Pattern, error) {
	p := &parser{src: src, named: map[string]int{}}
	root, err := p.parseAlt()
	if err != nil {
		return nil, err
	}
	if p.pos < len(p.src) {
		return nil, &ParseError{Offset: p.pos, Msg: "unexpected " + quoteAt(p.src, p.pos)}
	}
	return &Pattern{Root: root, NumGroups: p.group, NamedGroups: p.named, Source: src}, nil
}

func quoteAt(s string, i int) string {
	if i >= len(s) {
		return "end of pattern"
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	return strconv.QuoteRune(r)
}

func (p *parser) parseAlt() (Node, error) {
	var branches []Node
	first, err := p.parseConcat()
	if err != nil {
		return nil, err
	}
	branches = append(branches, first)
	for p.peek() == '|' {
		at := p.pos
		p.adv()
		branch, err := p.parseConcat()
		if err != nil {
			return nil, err
		}
		branches = append(branches, branch)
		_ = at
	}
	if len(branches) == 1 {
		return branches[0], nil
	}
	return &Alt{At: 0, Branches: branches}, nil
}

func (p *parser) parseConcat() (Node, error) {
	at := p.pos
	var items []Node
	for {
		c := p.peek()
		if c == 0 || c == '|' || c == ')' {
			break
		}
		n, err := p.parseRepeat()
		if err != nil {
			return nil, err
		}
		items = append(items, n)
	}
	switch len(items) {
	case 0:
		return &Empty{At: at}, nil
	case 1:
		return items[0], nil
	default:
		return &Concat{At: at, Items: items}, nil
	}
}

func (p *parser) parseRepeat() (Node, error) {
	atom, err := p.parseAtom()
	if err != nil {
		return nil, err
	}
	c := p.peek()
	if c != '?' && c != '*' && c != '+' && c != '{' {
		return atom, nil
	}
	at := p.pos
	min, max, unbounded, ok, qerr := p.parseQuantifierSpec()
	if qerr != nil {
		return nil, qerr
	}
	if !ok {
		// '{' not starting a valid quantifier is a literal in grammar v1.
		return atom, nil
	}
	lazy := false
	if p.peek() == '?' {
		lazy = true
		p.adv()
	} else if p.peek() == '+' {
		return nil, &ParseError{Offset: p.pos, Msg: "possessive quantifiers are not supported in grammar v1"}
	}
	if unbounded {
		max = -1
	}
	return &Repeat{At: at, Body: atom, Min: min, Max: max, Lazy: lazy}, nil
}

// parseQuantifierSpec parses ? * + {m,n}; ok is false only when '{' does not
// start a valid quantifier (then it must be treated as a literal).
func (p *parser) parseQuantifierSpec() (min, max int, unbounded, ok bool, err error) {
	switch p.peek() {
	case '?':
		p.adv()
		return 0, 1, false, true, nil
	case '*':
		p.adv()
		return 0, 0, true, true, nil
	case '+':
		p.adv()
		return 1, 0, true, true, nil
	case '{':
		save := p.pos
		p.adv()
		n1, has1 := p.readDigits()
		n2, has2 := n1, has1
		unb := false
		if p.peek() == ',' {
			p.adv()
			if d, hd := p.readDigits(); hd {
				n2, has2 = d, true
			} else {
				unb = true
			}
		}
		if p.peek() != '}' {
			p.pos = save
			return 0, 0, false, false, nil
		}
		p.adv()
		if !has1 {
			p.pos = save
			return 0, 0, false, false, nil
		}
		if !unb && has2 && n2 < n1 {
			return 0, 0, false, false, &ParseError{Offset: save, Msg: "quantifier minimum exceeds maximum"}
		}
		return n1, n2, unb, true, nil
	}
	return 0, 0, false, false, nil
}

func (p *parser) readDigits() (int, bool) {
	start := p.pos
	n := 0
	any := false
	for c := p.peek(); c >= '0' && c <= '9'; c = p.peek() {
		n = n*10 + int(c-'0')
		any = true
		p.adv()
	}
	_ = start
	return n, any
}

func (p *parser) parseAtom() (Node, error) {
	at := p.pos
	c := p.peek()
	switch c {
	case '(':
		return p.parseGroup()
	case '[':
		return p.parseClass()
	case '.':
		p.adv()
		return &Class{At: at, Negated: true, Items: []ClassItem{{Kind: 'l', R1: '\n'}, {Kind: 'l', R1: '\r'}}}, nil
	case '^':
		p.adv()
		return &Anchor{At: at, Kind: '^'}, nil
	case '$':
		p.adv()
		return &Anchor{At: at, Kind: '$'}, nil
	case '\\':
		return p.parseEscape()
	case 0:
		return nil, &ParseError{Offset: at, Msg: "unexpected end of pattern"}
	case ')':
		return nil, &ParseError{Offset: at, Msg: "unexpected )"}
	default:
		r, _ := p.readRune()
		if rr, ok := simpleEscape(r); ok {
			r = rr
		}
		return &Lit{At: at, R: r}, nil
	}
}

func lowerByte(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 32
	}
	return c
}

func (p *parser) parseGroup() (Node, error) {
	at := p.pos
	p.adv() // '('
	if p.peek() == '?' {
		p.adv()
		switch p.peek() {
		case ':':
			p.adv()
			body, err := p.parseAlt()
			if err != nil {
				return nil, err
			}
			if err := p.expect(')'); err != nil {
				return nil, err
			}
			return &Group{At: at, Capture: false, Body: body}, nil
		case '=':
			p.adv()
			return p.finishLook(at, false, false)
		case '!':
			p.adv()
			return p.finishLook(at, false, true)
		case '<':
			p.adv()
			switch p.peek() {
			case '=':
				p.adv()
				return p.finishLook(at, true, false)
			case '!':
				p.adv()
				return p.finishLook(at, true, true)
			}
			return p.parseNamedGroup(at)
		case 'i', 'm', 's', '-':
			return p.parseFlagGroup(at)
		default:
			return nil, &ParseError{Offset: p.pos, Msg: "unsupported group syntax after ?"}
		}
	}
	p.group++
	idx := p.group
	body, err := p.parseAlt()
	if err != nil {
		return nil, err
	}
	if err := p.expect(')'); err != nil {
		return nil, err
	}
	return &Group{At: at, Capture: true, Index: idx, Body: body}, nil
}

func (p *parser) finishLook(at int, behind, negative bool) (Node, error) {
	body, err := p.parseAlt()
	if err != nil {
		return nil, err
	}
	if err := p.expect(')'); err != nil {
		return nil, err
	}
	return &Look{At: at, Behind: behind, Negative: negative, Body: body}, nil
}

func (p *parser) parseNamedGroup(at int) (Node, error) {
	start := p.pos
	for p.peek() != '>' && p.peek() != 0 {
		p.adv()
	}
	name := p.src[start:p.pos]
	if name == "" {
		return nil, &ParseError{Offset: at, Msg: "empty group name"}
	}
	if p.peek() != '>' {
		return nil, &ParseError{Offset: p.pos, Msg: "unterminated named group"}
	}
	p.adv()
	p.group++
	idx := p.group
	if prev, dup := p.named[name]; dup {
		return nil, &ParseError{Offset: at, Msg: fmt.Sprintf("duplicate group name %q (first at group %d)", name, prev)}
	}
	p.named[name] = idx
	body, err := p.parseAlt()
	if err != nil {
		return nil, err
	}
	if err := p.expect(')'); err != nil {
		return nil, err
	}
	return &Group{At: at, Capture: true, Index: idx, Name: name, Named: true, Body: body}, nil
}

func (p *parser) parseFlagGroup(at int) (Node, error) {
	// Flags are stored as a tiny DSL: letters directly after '(' enable flags,
	// letters after '-' disable them, e.g. "i", "-i", "im-s".
	flags := []byte{}
	_ = false
	sawFlag := false
	for {
		c := p.peek()
		if c == '-' {

			flags = append(flags, '-')
			p.adv()
			continue
		}
		if c == 'i' || c == 'm' || c == 's' {
			flags = append(flags, byte(c))
			sawFlag = true
			p.adv()
			continue
		}
		break
	}
	if p.peek() != ')' || !sawFlag {
		return nil, &ParseError{Offset: p.pos, Msg: "only inline flags i/m/s are supported"}
	}
	p.adv()
	body, err := p.parseAlt()
	if err != nil {
		return nil, err
	}
	if err := p.expect(')'); err != nil {
		return nil, err
	}
	return &Group{At: at, Capture: false, Flags: string(flags), Body: body}, nil
}

func (p *parser) expect(c byte) error {
	if p.peek() != c {
		return &ParseError{Offset: p.pos, Msg: "expected " + strconv.QuoteRune(rune(c))}
	}
	p.adv()
	return nil
}

func (p *parser) parseEscape() (Node, error) {
	at := p.pos
	p.adv() // backslash
	c := p.peek()
	switch c {
	case 0:
		return nil, &ParseError{Offset: at, Msg: "trailing backslash"}
	case 'd', 'D', 'w', 'W', 's', 'S':
		p.adv()
		return &Class{At: at, Items: []ClassItem{{Kind: 'p', Perl: lowerByte(c), Negated: c >= 'A' && c <= 'Z'}}}, nil
	case 'p', 'P':
		neg := c == 'P'
		p.adv()
		if p.peek() != '{' {
			return nil, &ParseError{Offset: p.pos, Msg: "expected { after \\p"}
		}
		p.adv()
		start := p.pos
		for p.peek() != '}' && p.peek() != 0 {
			p.adv()
		}
		name := strings.TrimSpace(p.src[start:p.pos])
		if p.peek() != '}' || name == "" {
			return nil, &ParseError{Offset: at, Msg: "invalid \\p property"}
		}
		p.adv()
		if !charsets.LookupUnicodeProperty(name) {
			return nil, &ParseError{Offset: at, Msg: "unknown Unicode property: " + name}
		}
		return &Class{At: at, Items: []ClassItem{{Kind: 'u', Property: name, NegProp: neg}}}, nil
	case 'b':
		p.adv()
		return &Anchor{At: at, Kind: 'b'}, nil
	case 'B':
		p.adv()
		return &Anchor{At: at, Kind: 'B'}, nil
	case 'A':
		p.adv()
		return &Anchor{At: at, Kind: 'A'}, nil
	case 'z':
		p.adv()
		return &Anchor{At: at, Kind: 'z'}, nil
	case 'Z':
		p.adv()
		return &Anchor{At: at, Kind: 'Z'}, nil
	case 'k':
		p.adv()
		if p.peek() != '<' {
			return nil, &ParseError{Offset: p.pos, Msg: "expected < after \\k"}
		}
		p.adv()
		start := p.pos
		for p.peek() != '>' && p.peek() != 0 {
			p.adv()
		}
		name := p.src[start:p.pos]
		if p.peek() != '>' || name == "" {
			return nil, &ParseError{Offset: at, Msg: "invalid named backreference"}
		}
		p.adv()
		return &Ref{At: at, Named: true, Name: name}, nil
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		var digits []byte
		for c := p.peek(); c >= '0' && c <= '9'; c = p.peek() {
			digits = append(digits, c)
			p.adv()
			if len(digits) == 3 {
				break
			}
		}
		n, _ := strconv.Atoi(string(digits))
		return &Ref{At: at, Index: n}, nil
	default:
		r, _ := p.readRune()
		return &Lit{At: at, R: r}, nil
	}
}

// simpleEscapes returns the rune denoted by a backslash escape inside classes
// and literals.
func simpleEscape(c rune) (rune, bool) {
	switch c {
	case 'n':
		return '\n', true
	case 'r':
		return '\r', true
	case 't':
		return '\t', true
	case 'f':
		return '\f', true
	case 'v':
		return '\v', true
	case 'a':
		return 0x07, true
	case 'e':
		return 0x1B, true
	}
	return c, true
}

func (p *parser) parseClass() (Node, error) {
	at := p.pos
	p.adv() // '['
	cls := &Class{At: at}
	if p.peek() == '^' {
		cls.Negated = true
		p.adv()
	}
	first := true
	for p.peek() != ']' || first {
		first = false
		if p.peek() == 0 {
			return nil, &ParseError{Offset: at, Msg: "unterminated character class"}
		}
		item, err := p.parseClassItem()
		if err != nil {
			return nil, err
		}
		// Detect "lo-9" style ranges.
		if p.peek() == '-' {
			save := p.pos
			p.adv()
			if p.peek() == ']' {
				p.pos = save
				cls.Items = append(cls.Items, item)
				cls.Items = append(cls.Items, ClassItem{Kind: 'l', R1: '-'})
				continue
			}
			end, err := p.parseClassAtom()
			if err != nil {
				return nil, err
			}
			if item.Kind != 'l' || end.Kind != 'l' {
				return nil, &ParseError{Offset: at, Msg: "range endpoints must be literals"}
			}
			if item.R1 > end.R1 {
				return nil, &ParseError{Offset: at, Msg: "invalid character range (reversed)"}
			}
			cls.Items = append(cls.Items, ClassItem{Kind: 'r', R1: item.R1, R2: end.R1})
			continue
		}
		cls.Items = append(cls.Items, item)
	}
	p.adv() // ']'
	return cls, nil
}

func (p *parser) parseClassItem() (ClassItem, error) {
	return p.parseClassAtom()
}

func (p *parser) parseClassAtom() (ClassItem, error) {
	at := p.pos
	c := p.peek()
	if c == '\\' {
		p.adv()
		esc := p.peek()
		switch esc {
		case 'd', 'D', 'w', 'W', 's', 'S':
			p.adv()
			return ClassItem{Kind: 'p', Perl: lowerByte(esc), Negated: esc >= 'A' && esc <= 'Z'}, nil
		case 'p', 'P':
			neg := esc == 'P'
			p.adv()
			if p.peek() != '{' {
				return ClassItem{}, &ParseError{Offset: at, Msg: "expected { after \\p"}
			}
			p.adv()
			start := p.pos
			for p.peek() != '}' && p.peek() != 0 {
				p.adv()
			}
			name := strings.TrimSpace(p.src[start:p.pos])
			if p.peek() != '}' || name == "" {
				return ClassItem{}, &ParseError{Offset: at, Msg: "invalid \\p property"}
			}
			p.adv()
			if !charsets.LookupUnicodeProperty(name) {
				return ClassItem{}, &ParseError{Offset: at, Msg: "unknown Unicode property: " + name}
			}
			return ClassItem{Kind: 'u', Property: name, NegProp: neg}, nil
		case 0:
			return ClassItem{}, &ParseError{Offset: at, Msg: "trailing backslash in class"}
		default:
			r, _ := p.readRune()
			if rr, ok := simpleEscape(r); ok {
				r = rr
			}
			return ClassItem{Kind: 'l', R1: r}, nil
		}
	}
	r, err := p.readRune()
	if err != nil {
		return ClassItem{}, err
	}
	_ = at
	return ClassItem{Kind: 'l', R1: r}, nil
}

// readRune consumes and returns the next rune; a bare byte that is not valid
// UTF-8 is returned as RuneError and consumed one byte at a time.
func (p *parser) readRune() (rune, error) {
	if p.pos >= len(p.src) {
		return 0, &ParseError{Offset: p.pos, Msg: "unexpected end of pattern"}
	}
	r, size := utf8.DecodeRuneInString(p.src[p.pos:])
	p.pos += size
	return r, nil
}

func (p *parser) peek() byte {
	if p.pos >= len(p.src) {
		return 0
	}
	return p.src[p.pos]
}

func (p *parser) adv() {
	if p.pos < len(p.src) {
		p.pos++
	}
}
