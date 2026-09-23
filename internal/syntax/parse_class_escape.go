package syntax

import "unicode/utf8"

// readEscapeInClass 读取字符类内的反斜杠项。
// 返回：
//   - isClass=true, classByte!=0：内置类 \d/\w/\s 等；
//   - isClass=true, classByte==0：\p{} Unicode 属性；
//   - isClass=false：字面量 rune（\b 在类内表示退格 U+0008）。
func (p *parser) readEscapeInClass() (rune, bool, byte, UnicodeProp, error) {
	start := p.pos
	p.pos++ // backslash
	if p.pos >= len(p.src) {
		return 0, false, 0, UnicodeProp{}, p.err(start, "结尾的反斜杠")
	}
	c := p.src[p.pos]
	switch c {
	case 'b':
		p.pos++
		return '\b', false, 0, UnicodeProp{}, nil
	case 'd', 'D', 'w', 'W', 's', 'S':
		p.pos++
		return 0, true, c, UnicodeProp{}, nil
	case 'p', 'P':
		neg := c == 'P'
		p.pos++
		prop, _, err := p.readUnicodeName(neg)
		if err != nil {
			return 0, false, 0, UnicodeProp{}, err
		}
		return 0, true, 0, prop, nil
	default:
		r, sz, err := p.readSimpleEscape(c)
		if err != nil {
			return 0, false, 0, UnicodeProp{}, err
		}
		p.pos += sz
		if r >= utf8.RuneSelf {
			_ = utf8.RuneLen(r)
		}
		return r, false, 0, UnicodeProp{}, nil
	}
}
