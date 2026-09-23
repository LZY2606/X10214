package regex

import (
	"fmt"
	"strconv"
	"unicode/utf8"
)

// parseEscape handles a token beginning with backslash.
func (p *parser) parseEscape() (Node, error) {
	pos := p.pos
	p.pos++ // backslash
	c := p.peek()
	if c == -1 {
		return nil, &ParseError{Offset: pos, Msg: "trailing backslash"}
	}
	switch c {
	case 'd', 'D', 'w', 'W', 's', 'S':
		p.pos++
		neg := c >= 'A' && c <= 'Z'
		perl := byte(c)
		if neg {
			perl = byte(c + 'a' - 'A')
		}
		return &AtomClass{baseNode: baseNode{pos}, Elems: []ClassElem{{Perl: perl}}, Negate: neg}, nil
	case 'b':
		p.pos++
		return &AtomBoundary{baseNode: baseNode{pos}, Kind: BoundaryWord}, nil
	case 'B':
		p.pos++
		return &AtomBoundary{baseNode: baseNode{pos}, Kind: BoundaryNotWord}, nil
	case 'x':
		p.pos++
		r, err := p.parseHex(2)
		if err != nil {
			return nil, err
		}
		return &AtomLit{baseNode: baseNode{pos}, R: r}, nil
	case 'u':
		p.pos++
		r, err := p.parseHex(4)
		if err != nil {
			return nil, err
		}
		return &AtomLit{baseNode: baseNode{pos}, R: r}, nil
	case 'U':
		p.pos++
		if p.peek() != '{' {
			return nil, &ParseError{Offset: p.pos, Msg: "expected { after \\U"}
		}
		p.pos++
		r, err := p.parseHexBraced()
		if err != nil {
			return nil, err
		}
		if p.peek() != '}' {
			return nil, &ParseError{Offset: p.pos, Msg: "expected } to close \\U"}
		}
		p.pos++
		return &AtomLit{baseNode: baseNode{pos}, R: r}, nil
	case 'X':
		p.pos++
		return &AtomGrapheme{baseNode: baseNode{pos}}, nil
	case 'p', 'P':
		neg := c == 'P'
		p.pos++
		prop, err := p.parseProp()
		if err != nil {
			return nil, err
		}
		return &AtomClass{baseNode: baseNode{pos}, Elems: []ClassElem{{Prop: prop}}, Negate: neg}, nil
	case 'k':
		p.pos++
		if p.peek() != '<' {
			return nil, &ParseError{Offset: p.pos, Msg: "expected < after \\k"}
		}
		p.pos++
		start := p.pos
		for p.peek() != '>' && p.peek() != -1 {
			p.pos++
		}
		if p.peek() != '>' {
			return nil, &ParseError{Offset: p.pos, Msg: "unterminated named reference"}
		}
		name := p.src[start:p.pos]
		p.pos++
		idx, ok := p.names[name]
		if !ok {
			// allow forward reference; resolved after parsing
			idx = -1
		}
		return &AtomBackref{baseNode: baseNode{pos}, Name: name, Index: idx}, nil
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return p.parseBackref(pos)
	case 'a', 'f', 'n', 'r', 't', 'v':
		p.pos++
		var r rune
		switch c {
		case 'a':
			r = '\a'
		case 'f':
			r = '\f'
		case 'n':
			r = '\n'
		case 'r':
			r = '\r'
		case 't':
			r = '\t'
		case 'v':
			r = '\v'
		}
		return &AtomLit{baseNode: baseNode{pos}, R: r}, nil
	}
	// Any other escaped character is a literal.
	p.pos++
	r, _ := utf8.DecodeRuneInString(p.src[p.pos-1:])
	return &AtomLit{baseNode: baseNode{pos}, R: r}, nil
}

func hexVal(c int) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

func (p *parser) parseHex(n int) (rune, error) {
	var v rune
	for i := 0; i < n; i++ {
		d, ok := hexVal(p.peek())
		if !ok {
			return 0, &ParseError{Offset: p.pos, Msg: fmt.Sprintf("bad hex escape, want %d digits", n)}
		}
		p.pos++
		v = v<<4 | rune(d)
	}
	return v, nil
}

func (p *parser) parseHexBraced() (rune, error) {
	start := p.pos
	var v rune
	any := false
	for {
		d, ok := hexVal(p.peek())
		if !ok {
			break
		}
		any = true
		p.pos++
		v = v<<4 | rune(d)
		if v > 0x10FFFF {
			return 0, &ParseError{Offset: start, Msg: "unicode escape out of range"}
		}
	}
	if !any {
		return 0, &ParseError{Offset: start, Msg: "empty hex escape"}
	}
	return v, nil
}

func (p *parser) parseProp() (string, error) {
	if p.peek() != '{' {
		c := p.peek()
		if c == -1 {
			return "", &ParseError{Offset: p.pos, Msg: "bad \\p property"}
		}
		p.pos++
		return string(rune(c)), nil
	}
	p.pos++
	start := p.pos
	for p.peek() != '}' && p.peek() != -1 {
		p.pos++
	}
	if p.peek() != '}' {
		return "", &ParseError{Offset: p.pos, Msg: "unterminated \\p property"}
	}
	name := p.src[start:p.pos]
	p.pos++
	if name == "" {
		return "", &ParseError{Offset: start, Msg: "empty unicode property"}
	}
	if _, ok := propRange(name); !ok {
		return "", &ParseError{Offset: start, Msg: "unknown unicode property " + name}
	}
	return name, nil
}

func (p *parser) parseBackref(pos int) (Node, error) {
	start := p.pos
	for isDigit(p.peek()) {
		p.pos++
	}
	idx, _ := strconv.Atoi(p.src[start:p.pos])
	if idx < 1 {
		return nil, &ParseError{Offset: pos, Msg: "backreference to invalid group 0"}
	}
	return &AtomBackref{baseNode: baseNode{pos}, Index: idx}, nil
}
