package syntax

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// parseAtom 返回原子，以及对“当前连接剩余部分”生效的标志（来自 (?i) 这类裸标志组）。
func (p *parser) parseAtom(depth int, fl Flags) (*Node, Flags, error) {
	if p.pos >= len(p.src) {
		return nil, fl, nil
	}
	start := p.pos
	c := p.src[p.pos]
	switch c {
	case '(':
		return p.parseGroup(depth, fl)
	case '[':
		n, err := p.parseClass(start)
		return n, fl, err
	case '.':
		p.pos++
		n := p.mk(KAny, start, 1)
		n.Flags = fl
		return n, fl, nil
	case '^':
		p.pos++
		n := p.mk(KAnchor, start, 1)
		if fl.M {
			n.Anchor = ABeginLine
		} else {
			n.Anchor = ABegin
		}
		return n, fl, nil
	case '$':
		p.pos++
		n := p.mk(KAnchor, start, 1)
		if fl.M {
			n.Anchor = AEndLine
		} else {
			n.Anchor = AEndOptNL
		}
		return n, fl, nil
	case '\\':
		return p.parseEscape(fl)
	case '*', '+', '?', '{', '}', ']', '|', ')':
		return nil, fl, p.err(start, "未期望的 %q", string(c))
	default:
		r, sz := utf8.DecodeRuneInString(p.src[p.pos:])
		p.pos += sz
		n := p.mk(KChar, start, sz)
		n.Rune = r
		n.Flags = fl
		return n, fl, nil
	}
}

func (p *parser) parseGroup(depth int, fl Flags) (*Node, Flags, error) {
	start := p.pos
	p.pos++ // '('
	neg := false
	behind := false
	capture := true
	name := ""
	addF, delF := Flags{}, Flags{}

	if p.pos < len(p.src) && p.src[p.pos] == '?' {
		p.pos++
		capture = false
		if p.pos >= len(p.src) {
			return nil, fl, p.err(start, "未闭合的组")
		}
		switch p.src[p.pos] {
		case ':':
			p.pos++
		case '=':
			p.pos++
			return p.parseLook(depth, fl, start, false, false)
		case '!':
			p.pos++
			return p.parseLook(depth, fl, start, true, false)
		case '<':
			p.pos++
			if p.pos < len(p.src) && p.src[p.pos] == '=' {
				p.pos++
				return p.parseLook(depth, fl, start, false, true)
			}
			if p.pos < len(p.src) && p.src[p.pos] == '!' {
				p.pos++
				return p.parseLook(depth, fl, start, true, true)
			}
			// (?<name>...) 命名捕获
			nm, err := p.scanName('>')
			if err != nil {
				return nil, fl, err
			}
			name = nm
			capture = true
		default:
			// (?flags) 或 (?flags:...)
			af, df, colon, err := p.scanFlags()
			if err != nil {
				return nil, fl, err
			}
			addF, delF = af, df
			if !colon {
				// 裸标志组：对剩余连接生效
				body, err := p.parseAlt(depth+1, applyFlags(fl, addF, delF))
				if err != nil {
					return nil, fl, err
				}
				if p.pos >= len(p.src) || p.src[p.pos] != ')' {
					return nil, fl, p.err(p.pos, "缺少 )")
				}
				p.pos++
				fn := p.mk(KFlagGroup, start, p.pos-start)
				fn.Inner = body
				fn.AddFlags, fn.DelFlags = addF, delF
				return fn, applyFlags(fl, addF, delF), nil
			}
			fl = applyFlags(fl, addF, delF)
		}
	}

	idx := 0
	if capture {
		p.groups++
		idx = p.groups
		if name != "" {
			if _, ok := p.names[name]; ok {
				return nil, fl, p.err(start, "重复的组名 %q", name)
			}
			p.names[name] = idx
		}
	}
	body, err := p.parseAlt(depth+1, fl)
	if err != nil {
		return nil, fl, err
	}
	if p.pos >= len(p.src) || p.src[p.pos] != ')' {
		return nil, fl, p.err(p.pos, "缺少 )")
	}
	p.pos++
	if body == nil {
		body = p.mk(KEmpty, p.pos, 0)
	}
	n := p.mk(KGroup, start, p.pos-start)
	n.Capture = capture
	n.GroupIdx = idx
	n.Name = name
	n.Child = body
	n.Flags = fl
	n.AddFlags, n.DelFlags = addF, delF
	_ = neg
	_ = behind
	return n, fl, nil
}

func (p *parser) parseLook(depth int, fl Flags, start int, neg, behind bool) (*Node, Flags, error) {
	body, err := p.parseAlt(depth+1, fl)
	if err != nil {
		return nil, fl, err
	}
	if p.pos >= len(p.src) || p.src[p.pos] != ')' {
		return nil, fl, p.err(p.pos, "缺少 )")
	}
	p.pos++
	if body == nil {
		body = p.mk(KEmpty, p.pos, 0)
	}
	n := p.mk(KLookaround, start, p.pos-start)
	n.Neg = neg
	n.Behind = behind
	n.Child = body
	n.Flags = fl
	return n, fl, nil
}

func (p *parser) scanName(close byte) (string, error) {
	start := p.pos
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == close {
			break
		}
		if !(c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (p.pos > start && c >= '0' && c <= '9')) {
			return "", p.err(p.pos, "组名中存在非法字符 %q", string(c))
		}
		p.pos++
	}
	if p.pos == start {
		return "", p.err(start, "组名为空")
	}
	if p.pos >= len(p.src) {
		return "", p.err(start, "未闭合的命名组")
	}
	nm := p.src[start:p.pos]
	p.pos++ // close
	return nm, nil
}

func applyFlags(base, add, del Flags) Flags {
	base.I = (base.I || add.I) && !del.I
	base.M = (base.M || add.M) && !del.M
	base.S = (base.S || add.S) && !del.S
	return base
}

func (p *parser) scanFlags() (add, del Flags, colon bool, err error) {
	deleting := false
	saw := false
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == '-' && !deleting {
			deleting = true
			p.pos++
			continue
		}
		if c == ')' {
			p.pos++
			return add, del, false, nil
		}
		if c == ':' {
			if !saw {
				return add, del, false, p.err(p.pos, "标志为空")
			}
			p.pos++
			return add, del, true, nil
		}
		saw = true
		t := &add
		if deleting {
			t = &del
		}
		switch c {
		case 'i':
			t.I = true
		case 'm':
			t.M = true
		case 's':
			t.S = true
		default:
			return add, del, false, p.err(p.pos, "不支持的标志 %q", string(c))
		}
		p.pos++
	}
	return add, del, false, p.err(p.pos, "未闭合的标志组")
}

var _ = unicode.IsLetter
var _ = strings.HasPrefix
