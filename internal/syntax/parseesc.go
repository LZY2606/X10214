package syntax

import (
	"fmt"
)

// decodeOneRune 解码 src[at:] 的一个“输入字符”。
// 合法 UTF-8：返回真实 rune 与字节长度；非法字节：返回伪码点
// invalidBase + 该字节，长度为 1。这样无效 UTF-8 既可逐字节定位，
// 也能在模式中被 \x80..\xFF 精确匹配。
func decodeOneRune(src string, at int) (rune, int) {
	b := src[at]
	if b < 0x80 {
		return rune(b), 1
	}
	r, size := decodeRune(src[at:])
	if r < 0 {
		return InvalidByteRune(b), 1
	}
	return r, size
}

// 不使用 unicode/utf8 的 RuneError 语义：自行判断合法性。
func decodeRune(s string) (rune, int) {
	b0 := s[0]
	switch {
	case b0 < 0x80:
		return rune(b0), 1
	case b0 < 0xC2:
		return -1, 1
	case b0 < 0xE0:
		if len(s) < 2 || !isCont(s[1]) {
			return -1, 1
		}
		r := rune(b0&0x1F)<<6 | rune(s[1]&0x3F)
		return r, 2
	case b0 < 0xF0:
		if len(s) < 3 || !isCont(s[1]) || !isCont(s[2]) {
			return -1, 1
		}
		r := rune(b0&0x0F)<<12 | rune(s[1]&0x3F)<<6 | rune(s[2]&0x3F)
		if b0 == 0xE0 && r < 0x800 {
			return -1, 1
		}
		if r >= 0xD800 && r <= 0xDFFF {
			return -1, 1
		}
		return r, 3
	case b0 < 0xF5:
		if len(s) < 4 || !isCont(s[1]) || !isCont(s[2]) || !isCont(s[3]) {
			return -1, 1
		}
		r := rune(b0&0x07)<<18 | rune(s[1]&0x3F)<<12 | rune(s[2]&0x3F)<<6 | rune(s[3]&0x3F)
		if (b0 == 0xF0 && r < 0x10000) || r > 0x10FFFF {
			return -1, 1
		}
		return r, 4
	}
	return -1, 1
}

func isCont(b byte) bool { return b&0xC0 == 0x80 }

func (p *parser) parseEscape() (*Node, error) {
	from := p.pos
	p.pos++ // '\'
	if p.pos >= len(p.src) {
		return nil, p.errf(from, "trailing backslash")
	}
	c := p.src[p.pos]
	switch c {
	case 'd', 'D', 'w', 'W', 's', 'S':
		p.pos++
		n := p.newNode(NCharClass, from, p.pos)
		n.Ranges = namedClassRanges(c)
		return n, nil
	case 'p', 'P':
		p.pos++
		neg := c == 'P'
		name, err := p.readPropName()
		if err != nil {
			return nil, err
		}
		rs, ok := PropertyRanges(name)
		if !ok {
			return nil, p.errf(from, "unknown Unicode property %q", name)
		}
		if neg {
			rs = ComplementRanges(rs)
		}
		n := p.newNode(NCharClass, from, p.pos)
		n.Ranges = rs
		return n, nil
	case 'b', 'B':
		p.pos++
		kind := "wordBoundary"
		if c == 'B' {
			kind = "notWordBoundary"
		}
		return p.anchorNode(from, kind), nil
	case 'A':
		p.pos++
		return p.anchorNode(from, "bosNonLine"), nil
	case 'z':
		p.pos++
		return p.anchorNode(from, "eosNonLine"), nil
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return p.parseNumBackref(from)
	case 'k':
		p.pos++
		name, err := p.readKName()
		if err != nil {
			return nil, err
		}
		n := p.newNode(NBackref, from, p.pos)
		p.backrefs = append(p.backrefs, backrefRef{node: n, name: name})
		return n, nil
	}
	// 简单转义或字面转义
	r, err := p.simpleEscape(c)
	if err != nil {
		return nil, err
	}
	n := p.newNode(NCharClass, from, p.pos)
	n.Ranges = []RuneRange{{Lo: r, Hi: r}}
	n.Single = true
	return n, nil
}

func (p *parser) parseNumBackref(from int) (*Node, error) {
	d0 := p.pos
	num := 0
	for p.pos < len(p.src) {
		b := p.src[p.pos]
		if b < '0' || b > '9' {
			break
		}
		num = num*10 + int(b-'0')
		p.pos++
		if num > 1000 {
			return nil, p.errf(d0, "backreference number too large")
		}
	}
	if num == 0 {
		// \0  NUL
		n := p.newNode(NCharClass, from, p.pos)
		n.Ranges = []RuneRange{{Lo: 0, Hi: 0}}
		n.Single = true
		return n, nil
	}
	n := p.newNode(NBackref, from, p.pos)
	p.backrefs = append(p.backrefs, backrefRef{node: n, num: num})
	return n, nil
}

func (p *parser) readPropName() (string, error) {
	if p.peek() != '{' {
		return "", p.errf(p.pos, "expected '{' after \\p")
	}
	p.pos++
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] != '}' {
		p.pos++
	}
	if p.pos >= len(p.src) {
		return "", p.errf(start, "unterminated \\p{...}")
	}
	name := p.src[start:p.pos]
	p.pos++ // '}'
	if name == "" {
		return "", p.errf(start, "empty property name")
	}
	return name, nil
}

func (p *parser) readKName() (string, error) {
	if p.peek() != '<' {
		return "", p.errf(p.pos, "expected '<' after \\k")
	}
	p.pos++
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] != '>' {
		p.pos++
	}
	if p.pos >= len(p.src) {
		return "", p.errf(start, "unterminated \\k<...>")
	}
	name := p.src[start:p.pos]
	p.pos++
	if name == "" {
		return "", p.errf(start, "empty reference name")
	}
	return name, nil
}

func (p *parser) simpleEscape(c byte) (rune, error) {
	at := p.pos
	switch c {
	case 'n':
		p.pos++
		return '\n', nil
	case 'r':
		p.pos++
		return '\r', nil
	case 't':
		p.pos++
		return '\t', nil
	case 'f':
		p.pos++
		return '\f', nil
	case 'v':
		p.pos++
		return '\v', nil
	case 'a':
		p.pos++
		return 0x07, nil
	case 'x':
		return p.readHex(at, 2)
	case 'u':
		return p.readHexBraced(at)
	}
	// 其它被反斜杠转义的字符一律按其字面含义（包括 . * + ? 等）。
	p.pos++
	return rune(c), nil
}

func (p *parser) readHex(at int, maxDigits int) (rune, error) {
	p.pos++ // 越过 'x'
	var v rune
	n := 0
	for n < maxDigits && p.pos < len(p.src) {
		d, ok := hexVal(p.src[p.pos])
		if !ok {
			break
		}
		v = v<<4 | d
		p.pos++
		n++
	}
	if n == 0 {
		return 0, p.errf(at, "expected hex digits")
	}
	return v, nil
}

func (p *parser) readHexBraced(at int) (rune, error) {
	// 调用时 p.pos 指向 'u'
	if p.pos+1 < len(p.src) && p.src[p.pos+1] == '{' {
		p.pos += 2
		var v rune
		n := 0
		for p.pos < len(p.src) {
			d, ok := hexVal(p.src[p.pos])
			if !ok {
				break
			}
			v = v<<4 | d
			p.pos++
			n++
		}
		if p.pos >= len(p.src) || p.src[p.pos] != '}' || n == 0 || v > MaxRune {
			return 0, p.errf(at, "invalid \\u{...}")
		}
		p.pos++ // '}'
		return v, nil
	}
	return p.readHex(at, 4)
}

func hexVal(b byte) (rune, bool) {
	switch {
	case b >= '0' && b <= '9':
		return rune(b - '0'), true
	case b >= 'a' && b <= 'f':
		return rune(b-'a') + 10, true
	case b >= 'A' && b <= 'F':
		return rune(b-'A') + 10, true
	}
	return 0, false
}

var _ = fmt.Sprintf
