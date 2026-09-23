package syntax

import "strconv"

// parseQuantifier 为原子附加量词；量词不可作用于锚点与零宽重复以外的固定结构
// 按“捕获轨迹室”文档：允许作用于任何原子（含锚点会报错，锚点重复语义无意义）。
func (p *parser) parseQuantifier(atom *Node) (*Node, error) {
	if p.pos >= len(p.src) {
		return atom, nil
	}
	start := p.pos
	c := p.src[p.pos]
	min, max := 0, 0
	have := false
	switch c {
	case '*':
		min, max, have = 0, -1, true
		p.pos++
	case '+':
		min, max, have = 1, -1, true
		p.pos++
	case '?':
		min, max, have = 0, 1, true
		p.pos++
	case '{':
		lo, hi, ok, ln, err := p.scanBrace()
		if err != nil {
			return nil, err
		}
		if ok {
			min, max, have = lo, hi, true
			p.pos += ln
		}
	}
	if !have {
		return atom, nil
	}
	switch atom.Kind {
	case KAnchor, KLookaround:
		// 锚点/lookaround 重复与单次等价但易误用；仍允许（PCRE 允许）。
	}
	greedy := true
	if p.pos < len(p.src) && p.src[p.pos] == '?' {
		greedy = false
		p.pos++
	} else if p.pos < len(p.src) && p.src[p.pos] == '+' {
		// 所有量词统一使用可回溯语义。
		p.pos++
	}
	if max >= 0 && min > max {
		return nil, p.err(start, "量词下界超过上界")
	}
	n := p.mk(KRepeat, atom.SrcPos, 0)
	n.Child = atom
	n.Min = min
	n.Max = max
	n.Greedy = greedy
	return n, nil
}

// scanBrace 尝试解析 {n} {n,} {n,m}，失败返回 ok=false（按字面 '{' 处理）。
func (p *parser) scanBrace() (min, max int, ok bool, ln int, err error) {
	q := p.pos
	j := q + 1
	lo := 0
	digits := 0
	for j < len(p.src) && p.src[j] >= '0' && p.src[j] <= '9' {
		lo = lo*10 + int(p.src[j]-'0')
		j++
		digits++
	}
	if digits == 0 {
		return 0, 0, false, 0, nil
	}
	if j < len(p.src) && p.src[j] == '}' {
		return lo, lo, true, j - q + 1, nil
	}
	if j >= len(p.src) || p.src[j] != ',' {
		return 0, 0, false, 0, nil
	}
	j++
	if j < len(p.src) && p.src[j] == '}' {
		return lo, -1, true, j - q + 1, nil
	}
	hi := 0
	digits = 0
	for j < len(p.src) && p.src[j] >= '0' && p.src[j] <= '9' {
		hi = hi*10 + int(p.src[j]-'0')
		j++
		digits++
	}
	if digits == 0 || j >= len(p.src) || p.src[j] != '}' {
		return 0, 0, false, 0, nil
	}
	if lo > hi {
		return 0, 0, false, 0, p.err(q, "量词下界超过上界")
	}
	_ = strconv.Itoa(hi)
	return lo, hi, true, j - q + 1, nil
}
