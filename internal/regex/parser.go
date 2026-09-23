package regex

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

type ParseError struct {
	Offset int    `json:"offset"`
	Msg    string `json:"msg"`
}

func (e *ParseError) Error() string { return fmt.Sprintf("pattern offset %d: %s", e.Offset, e.Msg) }

type parser struct {
	pattern string
	pos     int
	groups  []Group
	names   map[string]int
}

func Parse(pattern string) (*Root, error) {
	p := &parser{pattern: pattern, names: map[string]int{}}
	expr, err := p.parseAlternation()
	if err != nil {
		return nil, err
	}
	if p.pos != len(pattern) {
		return nil, p.error("unexpected input")
	}
	root := &Root{Expr: expr, Groups: p.groups}
	root.StartByte, root.EndByte = 0, len(pattern)
	NumberNodes(root)
	return root, nil
}

func (p *parser) error(msg string) *ParseError { return &ParseError{Offset: p.pos, Msg: msg} }
func (p *parser) eof() bool                    { return p.pos >= len(p.pattern) }
func (p *parser) peek() byte {
	if p.eof() {
		return 0
	}
	return p.pattern[p.pos]
}
func (p *parser) eat(s string) bool {
	if strings.HasPrefix(p.pattern[p.pos:], s) {
		p.pos += len(s)
		return true
	}
	return false
}

func (p *parser) parseAlternation() (Node, error) {
	start := p.pos
	opts := []Node{}
	for {
		item, err := p.parseConcat()
		if err != nil {
			return nil, err
		}
		opts = append(opts, item)
		if p.peek() != '|' {
			break
		}
		p.pos++
	}
	if len(opts) == 1 {
		return opts[0], nil
	}
	return &Alternation{baseNode: baseNode{StartByte: start, EndByte: p.pos}, Options: opts}, nil
}

func (p *parser) parseConcat() (Node, error) {
	start := p.pos
	items := []Node{}
	for !p.eof() {
		c := p.peek()
		if c == '|' || c == ')' {
			break
		}
		atom, err := p.parseAtom()
		if err != nil {
			return nil, err
		}
		atom, err = p.parseQuantifier(atom)
		if err != nil {
			return nil, err
		}
		items = append(items, atom)
	}
	if len(items) == 1 {
		return items[0], nil
	}
	return &Concat{baseNode: baseNode{StartByte: start, EndByte: p.pos}, Items: items}, nil
}

func (p *parser) parseQuantifier(expr Node) (Node, error) {
	if p.eof() || strings.IndexByte("?*+{", p.peek()) < 0 {
		return expr, nil
	}
	qstart := p.pos
	min, max, hasMax, ok, err := p.quantBounds()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, p.error("quantifier has no atom")
	}
	greedy := true
	if p.peek() == '?' {
		greedy = false
		p.pos++
	} else if p.peek() == '+' {
		p.pos++
	}
	if hasMax && min > max {
		return nil, p.error("quantifier minimum exceeds maximum")
	}
	if _, assert := expr.(*GroupExpr); assert {
		g := expr.(*GroupExpr)
		if strings.HasPrefix(g.Kind, "look") {
			return nil, p.error("lookaround cannot be repeated")
		}
	}
	return &Repeat{baseNode: baseNode{StartByte: qstart, EndByte: p.pos}, Expr: expr, Min: min, Max: max, HasMax: hasMax, Greedy: greedy}, nil
}

func (p *parser) quantBounds() (int, int, bool, bool, error) {
	c := p.peek()
	switch c {
	case '?':
		p.pos++
		return 0, 1, true, true, nil
	case '*':
		p.pos++
		return 0, 0, false, true, nil
	case '+':
		p.pos++
		return 1, 0, false, true, nil
	case '{':
		save := p.pos
		p.pos++
		a := p.pos
		for !p.eof() && p.peek() >= '0' && p.peek() <= '9' {
			p.pos++
		}
		if a == p.pos {
			p.pos = save
			return 0, 0, false, false, nil
		}
		minV, err := strconv.Atoi(p.pattern[a:p.pos])
		if err != nil {
			p.pos = save
			return 0, 0, false, false, nil
		}
		maxV, hasMax := minV, true
		if p.peek() == ',' {
			p.pos++
			hasMax = false
			b := p.pos
			for !p.eof() && p.peek() >= '0' && p.peek() <= '9' {
				p.pos++
			}
			if b != p.pos {
				maxV, err = strconv.Atoi(p.pattern[b:p.pos])
				if err != nil {
					return 0, 0, false, false, err
				}
				hasMax = true
			}
		}
		if p.peek() != '}' {
			p.pos = save
			return 0, 0, false, false, nil
		}
		p.pos++
		return minV, maxV, hasMax, true, nil
	}
	return 0, 0, false, false, nil
}

func (p *parser) parseAtom() (Node, error) {
	start := p.pos
	c := p.peek()
	switch c {
	case '(':
		return p.parseGroup()
	case '[':
		return p.parseClass()
	case '.':
		p.pos++
		return &AnyChar{baseNode: baseNode{StartByte: start, EndByte: p.pos}}, nil
	case '^':
		p.pos++
		return &Anchor{baseNode: baseNode{StartByte: start, EndByte: p.pos}, Kind: "start"}, nil
	case '$':
		p.pos++
		return &Anchor{baseNode: baseNode{StartByte: start, EndByte: p.pos}, Kind: "end"}, nil
	case '\\':
		return p.parseEscapeAtom()
	case '*', '+', '?', '}':
		return nil, p.error("quantifier has no atom")
	case ')':
		return nil, p.error("unmatched )")
	case '|':
		return nil, p.error("empty alternative")
	default:
		r, size := decodePatternRune(p.pattern, p.pos)
		p.pos += size
		return &Literal{baseNode: baseNode{StartByte: start, EndByte: p.pos}, Rune: r}, nil
	}
}

func decodePatternRune(s string, pos int) (rune, int) {
	r, size := utf8.DecodeRuneInString(s[pos:])
	if r == utf8.RuneError && size <= 1 {
		return invalidPatternByte, 1
	}
	return r, size
}

const invalidPatternByte rune = -1

func (p *parser) parseGroup() (Node, error) {
	start := p.pos
	p.pos++
	kind := GroupNonCapture
	name := ""
	id := 0
	if p.eat("?:") {
		kind = GroupNonCapture
	} else if p.eat("?=") {
		kind = LookaheadPositive
	} else if p.eat("?!") {
		kind = LookaheadNegative
	} else if p.eat("?<=") {
		kind = LookbehindPositive
	} else if p.eat("?<!") {
		kind = LookbehindNegative
	} else if p.eat("?<") {
		ns := p.pos
		for !p.eof() && p.peek() != '>' {
			p.pos++
		}
		if p.peek() != '>' {
			return nil, p.error("unterminated named group")
		}
		name = p.pattern[ns:p.pos]
		if !validGroupName(name) {
			return nil, p.error("invalid group name")
		}
		if _, exists := p.names[name]; exists {
			return nil, p.error("duplicate group name")
		}
		p.pos++
		kind = GroupCapture
	} else {
		kind = GroupCapture
	}
	if kind == GroupCapture {
		id = len(p.groups) + 1
		if name != "" {
			p.names[name] = id
		}
		p.groups = append(p.groups, Group{ID: id, Name: name, Kind: kind})
	}
	if p.eof() {
		return nil, p.error("unterminated group")
	}
	expr, err := p.parseAlternation()
	if err != nil {
		return nil, err
	}
	if p.peek() != ')' {
		return nil, p.error("unterminated group")
	}
	p.pos++
	return &GroupExpr{baseNode: baseNode{StartByte: start, EndByte: p.pos}, GroupID: id, Name: name, Kind: kind, Expr: expr}, nil
}

func validGroupName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func (p *parser) parseEscapeAtom() (Node, error) {
	start := p.pos
	p.pos++
	if p.eof() {
		return nil, p.error("trailing backslash")
	}
	c := p.peek()
	switch {
	case c == 'x' || c == 'u':
		p.pos++
		r, err := p.parseHexEscape(4)
		if err != nil {
			return nil, err
		}
		return &Literal{baseNode: baseNode{StartByte: start, EndByte: p.pos}, Rune: r}, nil
	case c >= '1' && c <= '9':
		ns := p.pos
		for !p.eof() && p.peek() >= '0' && p.peek() <= '9' {
			p.pos++
		}
		id, _ := strconv.Atoi(p.pattern[ns:p.pos])
		if id > len(p.groups) {
			return nil, p.error("backreference to undefined group")
		}
		return &Backref{baseNode: baseNode{StartByte: start, EndByte: p.pos}, GroupID: id}, nil
	case c == 'k':
		p.pos++
		if p.peek() != '<' {
			return nil, p.error("named backreference expects <name>")
		}
		p.pos++
		ns := p.pos
		for !p.eof() && p.peek() != '>' {
			p.pos++
		}
		if p.peek() != '>' {
			return nil, p.error("unterminated named backreference")
		}
		name := p.pattern[ns:p.pos]
		id, ok := p.names[name]
		if !ok {
			return nil, p.error("backreference to undefined named group")
		}
		p.pos++
		return &Backref{baseNode: baseNode{StartByte: start, EndByte: p.pos}, GroupID: id, Name: name}, nil
	case c == 'A':
		p.pos++
		return &Anchor{baseNode: baseNode{StartByte: start, EndByte: p.pos}, Kind: "absolute_start"}, nil
	case c == 'z':
		p.pos++
		return &Anchor{baseNode: baseNode{StartByte: start, EndByte: p.pos}, Kind: "absolute_end"}, nil
	case c == 'b':
		p.pos++
		return &Anchor{baseNode: baseNode{StartByte: start, EndByte: p.pos}, Kind: "word_boundary"}, nil
	case c == 'B':
		p.pos++
		return &Anchor{baseNode: baseNode{StartByte: start, EndByte: p.pos}, Kind: "not_word_boundary"}, nil
	case c == 'p' || c == 'P':
		neg := c == 'P'
		p.pos++
		if p.peek() != '{' {
			return nil, p.error("Unicode property expects {name}")
		}
		ns := p.pos
		p.pos++
		for !p.eof() && p.peek() != '}' {
			p.pos++
		}
		if p.peek() != '}' {
			return nil, p.error("unterminated Unicode property")
		}
		prop := p.pattern[ns+1 : p.pos]
		p.pos++
		if !isKnownProperty(prop) {
			return nil, p.error("unknown Unicode property")
		}
		return &CharClass{baseNode: baseNode{StartByte: start, EndByte: p.pos}, Negated: neg, Ranges: []ClassRange{{Property: prop, Negated: neg}}}, nil
	default:
		r, cls, simple := simpleEscape(c)
		p.pos++
		if simple {
			if cls {
				return &CharClass{baseNode: baseNode{StartByte: start, EndByte: p.pos}, Ranges: []ClassRange{{Property: string(c)}}}, nil
			}
			return &Literal{baseNode: baseNode{StartByte: start, EndByte: p.pos}, Rune: r}, nil
		}
		return &Literal{baseNode: baseNode{StartByte: start, EndByte: p.pos}, Rune: rune(c)}, nil
	}
}

func (p *parser) parseClass() (Node, error) {
	start := p.pos
	p.pos++
	negated := false
	if p.peek() == '^' {
		negated = true
		p.pos++
	}
	ranges := []ClassRange{}
	first := true
	for !p.eof() && (p.peek() != ']' || first) {
		first = false
		r, err := p.parseClassUnit()
		if err != nil {
			return nil, err
		}
		if p.peek() == '-' && p.pos+1 < len(p.pattern) && p.pattern[p.pos+1] != ']' {
			p.pos++
			r2, err := p.parseClassUnit()
			if err != nil {
				return nil, err
			}
			if r < 0 || r2 < 0 || r > r2 {
				return nil, p.error("invalid character range")
			}
			ranges = append(ranges, ClassRange{Lo: r, Hi: r2})
		} else {
			ranges = append(ranges, classRange(r))
		}
	}
	if p.peek() != ']' {
		return nil, p.error("unterminated character class")
	}
	p.pos++
	return &CharClass{baseNode: baseNode{StartByte: start, EndByte: p.pos}, Negated: negated, Ranges: ranges}, nil
}

func classRange(r rune) ClassRange { return ClassRange{Lo: r, Hi: r} }

func (p *parser) parseClassUnit() (rune, error) {
	if p.peek() == '\\' {
		p.pos++
		if p.eof() {
			return 0, p.error("trailing backslash")
		}
		c := p.peek()
		if c == 'x' || c == 'u' {
			p.pos++
			return p.parseHexEscape(4)
		}
		r, _, _ := simpleEscape(c)
		p.pos++
		return r, nil
	}
	r, size := decodePatternRune(p.pattern, p.pos)
	p.pos += size
	return r, nil
}
