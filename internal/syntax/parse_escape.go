package syntax

import (
	"unicode/utf8"
)

func (p *parser) parseEscape(fl Flags) (*Node, Flags, error) {
	start := p.pos
	p.pos++ // backslash
	if p.pos >= len(p.src) {
		return nil, fl, p.err(start, "结尾的反斜杠")
	}
	c := p.src[p.pos]
	switch c {
	case 'A':
		p.pos++
		return p.anchor(start, ABegin), fl, nil
	case 'z':
		p.pos++
		return p.anchor(start, AEnd), fl, nil
	case 'Z':
		p.pos++
		return p.anchor(start, AEndLine), fl, nil
	case 'b':
		p.pos++
		return p.anchor(start, AWordBoundary), fl, nil
	case 'B':
		p.pos++
		return p.anchor(start, ANonWordBound), fl, nil
	case 'd', 'D', 'w', 'W', 's', 'S':
		p.pos++
		cn := &ClassNode{Classes: []byte{c}}
		n := p.mk(KClass, start, 2)
		n.Class = cn
		return n, fl, nil
	case 'p', 'P':
		neg := c == 'P'
		p.pos++
		prop, ln, err := p.readUnicodeName(neg)
		if err != nil {
			return nil, fl, err
		}
		cn := &ClassNode{Unicode: []UnicodeProp{prop}}
		n := p.mk(KClass, start, p.pos-start)
		_ = ln
		return n, fl, nil
	case 'k':
		p.pos++
		name, err := p.readKName()
		if err != nil {
			return nil, fl, err
		}
		n := p.mk(KBackRef, start, p.pos-start)
		n.RefName = name
		return n, fl, nil
	case '0' <= c && c <= '9':
		idx, ln := p.readDecimal()
		p.pos += ln
		if ln == 0 {
			// \0 NUL
			p.pos++
			return p.charNode(start, 0, fl), fl, nil
		}
		if idx > p.groups {
			return nil, fl, p.err(start, "反向引用 #%d 指向不存在的组（当前只有 %d 个组）", idx, p.groups)
		}
		n := p.mk(KBackRef, start, p.pos-start)
		n.RefIdx = idx
		return n, fl, nil
	default:
		r, sz, err := p.readSimpleEscape(c)
		if err != nil {
			return nil, fl, err
		}
		p.pos += sz
		n := p.charNode(start, r, fl)
		n.SrcLen = p.pos - start
		return n, fl, nil
	}
}

func (p *parser) anchor(start int, a Anchor) *Node {
	n := p.mk(KAnchor, start, 2)
	n.Anchor = a
	return n
}

func (p *parser) charNode(start int, r rune, fl Flags) *Node {
	n := p.mk(KChar, start, 0)
	n.Rune = r
	n.Flags = fl
	return n
}

// readDecimal 读取反向引用编号（最多 2 位），不允许前导形成 \0 八进制。
func (p *parser) readDecimal() (int, int) {
	j := p.pos
	n := 0
	ln := 0
	for j < len(p.src) && ln < 2 && p.src[j] >= '0' && p.src[j] <= '9' {
		if ln == 0 && p.src[j] == '0' {
			return 0, 0
		}
		n = n*10 + int(p.src[j]-'0')
		j++
		ln++
	}
	return n, ln
}

func (p *parser) readKName() (string, error) {
	if p.pos >= len(p.src) {
		return "", p.err(p.pos, "\\k 缺少组名")
	}
	close := byte('>')
	switch p.src[p.pos] {
	case '<':
		close = '>'
	case '\'':
		close = '\''
	default:
		return "", p.err(p.pos, "\\k 后应为 <name> 或 'name'")
	}
	start := p.pos
	p.pos++
	nameStart := p.pos
	for p.pos < len(p.src) && p.src[p.pos] != close {
		p.pos++
	}
	if p.pos >= len(p.src) {
		return "", p.err(start, "未闭合的组名")
	}
	nm := p.src[nameStart:p.pos]
	p.pos++
	return nm, nil
}

func (p *parser) readUnicodeName(neg bool) (UnicodeProp, int, error) {
	start := p.pos - 2 // 指向 \p / \P
	if p.pos >= len(p.src) || p.src[p.pos] != '{' {
		// \pL 单字母形式
		if p.pos < len(p.src) {
			r, sz := utf8.DecodeRuneInString(p.src[p.pos:])
			p.pos += sz
			return UnicodeProp{Name: string(r), Negate: neg}, p.pos - start, nil
		}
		return UnicodeProp{}, 0, p.err(start, "\\p 缺少属性名")
	}
	p.pos++ // {
	nameStart := p.pos
	caret := false
	if p.pos < len(p.src) && p.src[p.pos] == '^' {
		caret = true
		p.pos++
		nameStart = p.pos
	}
	for p.pos < len(p.src) && p.src[p.pos] != '}' {
		p.pos++
	}
	if p.pos >= len(p.src) {
		return UnicodeProp{}, 0, p.err(start, "未闭合的 \\p{...}")
	}
	name := p.src[nameStart:p.pos]
	p.pos++ // }
	return UnicodeProp{Name: name, Negate: neg != caret}, p.pos - start, nil
}

// readSimpleEscape 处理 \n \t 等；调用前 c == p.src[p.pos]。
// 返回 rune 与“从 c 开始消费的字节数”。
func (p *parser) readSimpleEscape(c byte) (rune, int, error) {
	switch c {
	case 'n':
		return '\n', 1, nil
	case 'r':
		return '\r', 1, nil
	case 't':
		return '\t', 1, nil
	case 'f':
		return '\f', 1, nil
	case 'v':
		return '\v', 1, nil
	case 'a':
		return 0x07, 1, nil
	case '0':
		// \0, \012 八进制（最多 3 位）
		j := p.pos
		v := 0
		ln := 0
		for j+ln < len(p.src) && ln < 3 && p.src[j+ln] >= '0' && p.src[j+ln] <= '7' {
			v = v*8 + int(p.src[j+ln]-'0')
			ln++
			if ln == 1 && p.src[j] == '0' && j+1 < len(p.src) && p.src[j+1] < '0' {
				break
			}
		}
		if ln == 0 {
			ln = 1
		}
		return rune(v), ln, nil
	case 'x':
		return p.readHexEscape()
	case 'u':
		return p.readUEscape()
	default:
		// 其它反斜杠转义一律按字面量处理（如 \\, \., \-）
		r, sz := utf8.DecodeRuneInString(p.src[p.pos:])
		return r, sz, nil
	}
}

func (p *parser) readHexEscape() (rune, int, error) {
	// p.pos 指向 'x'
	q := p.pos
	j := q + 1
	if j < len(p.src) && p.src[j] == '{' {
		k := j + 1
		v := 0
		digits := 0
		for k < len(p.src) && isHex(p.src[k]) {
			v = v*16 + hexVal(p.src[k])
			k++
			digits++
		}
		if digits == 0 || k >= len(p.src) || p.src[k] != '}' || v > 0x10FFFF {
			return 0, 0, p.err(q, "非法 \\x{...}")
		}
		return rune(v), k - q + 1, nil
	}
	v := 0
	for d := 0; d < 2; d++ {
		if j+d >= len(p.src) || !isHex(p.src[j+d]) {
			return 0, 0, p.err(q, "非法 \\xhh")
		}
		v = v*16 + hexVal(p.src[j+d])
	}
	return rune(v), 3, nil
}

func (p *parser) readUEscape() (rune, int, error) {
	q := p.pos
	j := q + 1
	if j+3 >= len(p.src) {
		return 0, 0, p.err(q, "非法 \\uXXXX")
	}
	v := 0
	for d := 0; d < 4; d++ {
		if !isHex(p.src[j+d]) {
			return 0, 0, p.err(q, "非法 \\uXXXX")
		}
		v = v*16 + hexVal(p.src[j+d])
	}
	return rune(v), 5, nil
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	default:
		return int(c-'A') + 10
	}
}
