package syntax

import (
	"strconv"
	"strings"
)

// group :=
//
//	'(' alternation ')'
//	'(?:' alternation ')'
//	'(?P<name>' | '(?<name>' alternation ')'
//	'(?' flags ('-' flags)? ':' alternation ')'
//	'(?' flags ')'
//	'(?=' | '(?!' | '(?<=' | '(?<!' alternation ')'
//	'(?>' alternation ')'
func (p *parser) parseGroup() (*Node, error) {
	from := p.pos
	p.pos++ // '('

	if p.peek() != '?' {
		return p.parseCapturingGroup(from, "", false)
	}
	p.pos++ // '?'

	switch p.peek() {
	case ':':
		p.pos++
		return p.parseWrapped(from, KindGroup, false)
	case '=':
		p.pos++
		return p.parseLook(from, true, true)
	case '!':
		p.pos++
		return p.parseLook(from, true, false)
	case '>':
		p.pos++
		return p.parseAtomic(from)
	case '<':
		// (?<name>  capture | (?<= / (?<! lookbehind
		p.pos++
		if p.peek() == '=' {
			p.pos++
			return p.parseLook(from, false, true)
		}
		if p.peek() == '!' {
			p.pos++
			return p.parseLook(from, false, false)
		}
		name, err := p.readName('>')
		if err != nil {
			return nil, err
		}
		return p.parseCapturingGroup(from, name, true)
	case 'P':
		if p.pos+1 < len(p.src) && p.src[p.pos+1] == '<' {
			p.pos += 2
			name, err := p.readName('>')
			if err != nil {
				return nil, err
			}
			return p.parseCapturingGroup(from, name, true)
		}
		return nil, p.errf("bad group after (?P")
	default:
		// inline flags: (?i), (?i:..), (?i-s:..)
		return p.parseFlagGroup(from)
	}
}

func (p *parser) readName(close byte) (string, error) {
	begin := p.pos
	for p.pos < len(p.src) && p.src[p.pos] != close {
		p.pos++
	}
	if p.pos >= len(p.src) {
		return "", p.errf("unterminated group name")
	}
	name := p.src[begin:p.pos]
	if name == "" {
		return "", p.errf("empty group name")
	}
	p.pos++ // close
	return name, nil
}

// captureCount tracks indices during the single parsing pass.
// Stored on parser via a field added lazily.
func (p *parser) nextCaptureIndex() int {
	p.captureCount++
	return p.captureCount
}

func (p *parser) parseCapturingGroup(from int, name string, named bool) (*Node, error) {
	_ = named
	n := p.newNode(KindGroup)
	n.Capture = true
	n.Index = p.nextCaptureIndex()
	n.Name = name
	inner, err := p.parseAlternation()
	if err != nil {
		return nil, err
	}
	if inner != nil {
		n.Children = []*Node{inner}
	} else {
		n.Children = []*Node{p.newNode(KindEmpty)}
	}
	if err := p.expectClose(from); err != nil {
		return nil, err
	}
	n.From, n.To, n.Src = from, p.pos, p.src[from:p.pos]
	return n, nil
}

func (p *parser) parseWrapped(from int, kind NodeKind, capture bool) (*Node, error) {
	n := p.newNode(kind)
	n.Capture = capture
	inner, err := p.parseAlternation()
	if err != nil {
		return nil, err
	}
	if inner != nil {
		n.Children = []*Node{inner}
	} else {
		n.Children = []*Node{p.newNode(KindEmpty)}
	}
	if err := p.expectClose(from); err != nil {
		return nil, err
	}
	n.From, n.To, n.Src = from, p.pos, p.src[from:p.pos]
	return n, nil
}

func (p *parser) parseAtomic(from int) (*Node, error) {
	n := p.newNode(KindAtomic)
	inner, err := p.parseAlternation()
	if err != nil {
		return nil, err
	}
	if inner != nil {
		n.Children = []*Node{inner}
	} else {
		n.Children = []*Node{p.newNode(KindEmpty)}
	}
	if err := p.expectClose(from); err != nil {
		return nil, err
	}
	n.From, n.To, n.Src = from, p.pos, p.src[from:p.pos]
	return n, nil
}

func (p *parser) parseLook(from int, ahead, pos bool) (*Node, error) {
	n := p.newNode(KindLook)
	n.Ahead = ahead
	n.Pos = pos
	inner, err := p.parseAlternation()
	if err != nil {
		return nil, err
	}
	if inner == nil {
		inner = p.newNode(KindEmpty)
	}
	n.Children = []*Node{inner}
	if err := p.expectClose(from); err != nil {
		return nil, err
	}
	if !ahead {
		bound, ok := fixedWidth(inner)
		if !ok {
			return nil, &ParseError{Msg: "lookbehind requires fixed-width subpattern", Offset: from}
		}
		n.Bound = bound
	} else {
		n.Bound = -1
	}
	n.From, n.To, n.Src = from, p.pos, p.src[from:p.pos]
	return n, nil
}

func (p *parser) parseFlagGroup(from int) (*Node, error) {
	begin := p.pos
	var set, unset []byte
	onUnset := false
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == '-' {
			onUnset = true
			p.pos++
			continue
		}
		if c == ':' || c == ')' {
			break
		}
		if c != 'i' && c != 's' && c != 'm' {
			return nil, p.errf("bad inline flag %q", c)
		}
		if onUnset {
			unset = append(unset, c)
		} else {
			set = append(set, c)
		}
		p.pos++
	}
	if p.pos == begin {
		return nil, p.errf("empty flag group")
	}
	if p.peek() == ')' {
		// flag-only group: applies to the remainder of the enclosing group
		p.pos++
		base := p.flags()
		p.pushFlags(base, string(set), string(unset))
		inner, err := p.parseAlternation()
		if err != nil {
			return nil, err
		}
		p.popFlags()
		if inner == nil {
			inner = p.newNode(KindEmpty)
		}
		n := p.newNode(KindGroup)
		n.Children = []*Node{inner}
		n.From, n.To, n.Src = from, p.pos, p.src[from:p.pos]
		return n, nil
	}
	// (?flags:...)
	p.pos++ // ':'
	base := p.flags()
	p.pushFlags(base, string(set), string(unset))
	n := p.newNode(KindGroup)
	inner, err := p.parseAlternation()
	if err != nil {
		return nil, err
	}
	p.popFlags()
	if err := p.expectClose(from); err != nil {
		return nil, err
	}
	if inner != nil {
		n.Children = []*Node{inner}
	} else {
		n.Children = []*Node{p.newNode(KindEmpty)}
	}
	n.From, n.To, n.Src = from, p.pos, p.src[from:p.pos]
	return n, nil
}

func (p *parser) expectClose(from int) error {
	if p.peek() != ')' {
		return p.errf("unclosed group opened at byte %d", from)
	}
	p.pos++
	return nil
}

var _ = strconv.Itoa
var _ = strings.IndexByte
