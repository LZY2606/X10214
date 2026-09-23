package syntax

import "fmt"

// ParseError 携带模式字节位置，供页面定位。
type ParseError struct {
	Pos int
	Msg string
}

func (e *ParseError) Error() string { return fmt.Sprintf("pattern:%d: %s", e.Pos, e.Msg) }

type parser struct {
	nodeSeq int
	src     string
	pos     int
	groups  int
	names   map[string]int
	flags   Flags // 模式级标志
}

// Parse 将正则模式解析为 AST，返回根节点与捕获组数量。
func Parse(pattern string) (*Node, int, error) {
	p := &parser{src: pattern, names: map[string]int{}}
	root, _, err := p.parseAlt(0, p.flags)
	if err != nil {
		return nil, 0, err
	}
	if p.pos < len(p.src) {
		return nil, 0, p.err(p.pos, "未期望的字符 %q", p.src[p.pos])
	}
	if root == nil {
		root = p.mk(KEmpty, 0, 0)
	}
	return root, p.groups, nil
}

func (p *parser) err(pos int, format string, args ...any) error {
	return &ParseError{Pos: pos, Msg: fmt.Sprintf(format, args...)}
}

func (p *parser) mk(kind NodeKind, pos, ln int) *Node {
	id := p.nodeSeq
	p.nodeSeq++
	return &Node{ID: NodeID(id), Kind: kind, Greedy: true, SrcPos: pos, SrcLen: ln}
}

// parseAlt 解析 alternation，按文档次序保留分支。
// 返回的 flags 为“裸标志组”对后续生效后的状态（RE2 风格，后续分支同样继承）。
func (p *parser) parseAlt(depth int, flags Flags) (*Node, Flags, error) {
	if depth > 1000 {
		return nil, flags, p.err(p.pos, "嵌套过深")
	}
	var branches []*Node
	fl := flags
	for {
		child, nfl, err := p.parseConcat(depth, fl)
		if err != nil {
			return nil, fl, err
		}
		fl = nfl
		if child == nil {
			child = p.mk(KEmpty, p.pos, 0)
		}
		branches = append(branches, child)
		if p.pos >= len(p.src) || p.src[p.pos] != '|' {
			break
		}
		p.pos++
	}
	if len(branches) == 1 {
		return branches[0], fl, nil
	}
	n := p.mk(KAlt, branches[0].SrcPos, 0)
	n.Children = branches
	return n, fl, nil
}

func (p *parser) parseConcat(depth int, flags Flags) (*Node, Flags, error) {
	var items []*Node
	start := p.pos
	fl := flags
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == '|' || c == ')' {
			break
		}
		atom, nfl, err := p.parseAtom(depth, fl)
		if err != nil {
			return nil, fl, err
		}
		fl = nfl
		if atom == nil {
			break
		}
		atom, err = p.parseQuantifier(atom)
		if err != nil {
			return nil, fl, err
		}
		items = append(items, atom)
	}
	switch len(items) {
	case 0:
		return nil, fl, nil
	case 1:
		return items[0], fl, nil
	}
	n := p.mk(KConcat, start, p.pos-start)
	n.Children = items
	return n, fl, nil
}
