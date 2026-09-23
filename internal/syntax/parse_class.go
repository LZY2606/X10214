package syntax

import "unicode/utf8"

// parseClass 解析 [...] 字符类。
// 规则：
//   - 紧跟 '[' 后的 '^' 表示取反；
//   - 紧邻 '[' 或 '[^' 后的 ']' 为字面量；
//   - 支持 - 区间（两端均为字面量/转义单字符）；
//   - 支持 \d \D \w \W \s \S 与 \p{...}/\P{...}（Go/RE2 风格 \p{^...} 视为否定）。
func (p *parser) parseClass(start int) (*Node, error) {
	p.pos++ // [
	cn := &ClassNode{}
	if p.pos < len(p.src) && p.src[p.pos] == '^' {
		cn.Negate = true
		p.pos++
	}
	if p.pos < len(p.src) && p.src[p.pos] == ']' {
		// 字面量 ]
		cn.Ranges = append(cn.Ranges, RuneRange{']', ']'})
		p.pos++
	}
	for p.pos < len(p.src) {
		if p.src[p.pos] == ']' {
			p.pos++
			n := p.mk(KClass, start, p.pos-start)
			n.Class = cn
			return n, nil
		}
		if err := p.parseClassItem(cn); err != nil {
			return nil, err
		}
	}
	return nil, p.err(start, "未闭合的字符类")
}

func (p *parser) parseClassItem(cn *ClassNode) error {
	r, isClass, classByte, prop, err := p.readClassAtom()
	if err != nil {
		return err
	}
	if isClass {
		if classByte != 0 {
			cn.Classes = append(cn.Classes, classByte)
		} else {
			cn.Unicode = append(cn.Unicode, prop)
		}
		return nil
	}
	// 区间？
	if p.pos < len(p.src) && p.src[p.pos] == '-' &&
		p.pos+1 < len(p.src) && p.src[p.pos+1] != ']' {
		dash := p.pos
		p.pos++ // -
		r2, isClass2, _, _, err := p.readClassAtom()
		if err != nil {
			return err
		}
		if isClass2 {
			return p.err(dash, "区间端点不能是字符类简写")
		}
		if r2 < r {
			return p.err(dash, "字符区间反向 %q-%q", r, r2)
		}
		cn.Ranges = append(cn.Ranges, RuneRange{r, r2})
		return nil
	}
	cn.Ranges = append(cn.Ranges, RuneRange{r, r})
	return nil
}

// readClassAtom 返回字面量 rune 或一个内置类标记。
func (p *parser) readClassAtom() (r rune, isClass bool, classByte byte, prop UnicodeProp, err error) {
	if p.pos >= len(p.src) {
		return 0, false, 0, UnicodeProp{}, p.err(p.pos, "字符类意外结束")
	}
	c := p.src[p.pos]
	if c == '\\' {
		return p.readEscapeInClass()
	}
	r, sz := utf8.DecodeRuneInString(p.src[p.pos:])
	p.pos += sz
	return r, false, 0, UnicodeProp{}, nil
}
