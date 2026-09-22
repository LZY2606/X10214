package syntax

import (
	"unicode/utf8"
)

// parseClass consumes a bracketed class; p.i points at '['.
func (p *parser) parseClass() Node {
	start := p.i
	p.i++ // '['
	node := &ClassNode{Offset: start}
	if p.i < len(p.src) && (p.src[p.i] == '^') {
		node.Negate = true
		p.i++
	}
	// A leading ] is a literal member.
	if p.i < len(p.src) && p.src[p.i] == ']' {
		node.Items = append(node.Items, ClassItem{Lo: ']', Hi: ']'})
		p.i++
	}
	for p.i < len(p.src) && p.src[p.i] != ']' {
		item, ok := p.parseClassItem(node)
		if !ok {
			return nil
		}
		// Detect a range: item-rune followed by '-'.
		if p.i < len(p.src) && p.src[p.i] == '-' &&
			p.i+1 < len(p.src) && p.src[p.i+1] != ']' &&
			item.Short == 0 && item.Prop == "" && item.RawLo == 0 {
			dash := p.i
			p.i++ // consume '-'
			hi, ok2 := p.parseClassSingleton(node)
			if !ok2 {
				return nil
			}
			if hi.Short != 0 || hi.Prop != "" || hi.RawLo != 0 {
				p.fail(dash, "range endpoint must be a literal")
				return nil
			}
			if item.Lo > hi.Lo {
				p.fail(dash, "invalid range: start above end")
				return nil
			}
			item.Hi = hi.Lo
		}
		node.Items = append(node.Items, item)
	}
	if p.i >= len(p.src) {
		p.fail(start, "unterminated character class")
		return nil
	}
	p.i++ // ']'
	return node
}

func (p *parser) parseClassItem(node *ClassNode) (ClassItem, bool) {
	return p.parseClassSingleton(node)
}

func (p *parser) parseClassSingleton(node *ClassNode) (ClassItem, bool) {
	c := p.src[p.i]
	switch c {
	case '\\':
		return p.parseClassEscape()
	case '[':
		// POSIX-style [:...:] blocks are not supported; treat as error to
		// avoid silent misinterpretation.
		p.fail(p.i, "POSIX classes are not supported")
		return ClassItem{}, false
	default:
		pos := p.i
		if c < utf8.RuneSelf {
			p.i++
			return ClassItem{Lo: rune(c), Hi: rune(c)}, true
		}
		r, size := utf8.DecodeRuneInString(p.src[p.i:])
		if r == utf8.RuneError && size == 1 {
			p.i++
			return ClassItem{Lo: utf8.RuneError, Hi: utf8.RuneError, RawLo: c}, true
		}
		p.i += size
		return ClassItem{Lo: r, Hi: r}, true
	}
}

func (p *parser) parseClassEscape() (ClassItem, bool) {
	pos := p.i
	p.i++ // backslash
	if p.i >= len(p.src) {
		p.fail(pos, "trailing backslash in class")
		return ClassItem{}, false
	}
	c := p.src[p.i]
	switch c {
	case 'd', 'D', 'w', 'W', 's', 'S':
		p.i++
		return ClassItem{Short: c}, true
	case 'p', 'P':
		neg := c == 'P'
		p.i++
		name, ok := p.readPropertyName(pos)
		if !ok {
			return ClassItem{}, false
		}
		return ClassItem{Prop: name, PropNeg: neg}, true
	case 'a':
		p.i++
		return ClassItem{Lo: 7, Hi: 7}, true
	case 'f':
		p.i++
		return ClassItem{Lo: 12, Hi: 12}, true
	case 'n':
		p.i++
		return ClassItem{Lo: 10, Hi: 10}, true
	case 'r':
		p.i++
		return ClassItem{Lo: 13, Hi: 13}, true
	case 't':
		p.i++
		return ClassItem{Lo: 9, Hi: 9}, true
	case 'v':
		p.i++
		return ClassItem{Lo: 11, Hi: 11}, true
	case '0':
		p.i++
		return ClassItem{Lo: 0, Hi: 0}, true
	case 'x':
		p.i++
		r, ok := p.readHex(pos, 2)
		if !ok {
			return ClassItem{}, false
		}
		return ClassItem{Lo: r, Hi: r}, true
	case 'u':
		p.i++
		r, ok := p.readHex(pos, 4)
		if !ok {
			return ClassItem{}, false
		}
		return ClassItem{Lo: r, Hi: r}, true
	default:
		if c >= utf8.RuneSelf {
			r, size := utf8.DecodeRuneInString(p.src[p.i:])
			if r == utf8.RuneError && size == 1 {
				p.i++
				return ClassItem{Lo: utf8.RuneError, Hi: utf8.RuneError, RawLo: c}, true
			}
			p.i += size
			return ClassItem{Lo: r, Hi: r}, true
		}
		p.i++
		return ClassItem{Lo: rune(c), Hi: rune(c)}, true
	}
}
