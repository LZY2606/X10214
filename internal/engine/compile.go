package engine

import "fmt"

// Op enumerates the machine instructions. Each op's semantics is implemented
// in vm.go and this is the only level at which matching happens: the host
// regexp package is never used.
type Op uint8

const (
	OpChar Op = iota + 1 // A/B: rune + ci flag
	OpAnyChar            // A: dotAll
	OpClass              // A: class id
	OpAnchor             // A: anchor id, B: multiline
	OpRef                // A: group index, B: ci
	OpSave               // A: capture slot
	OpStart              // A=greedy?1:0, B=alt pc, C=ast node id
	OpJmp                // A: target pc
	OpMatch              // end of a fullmatch body
	OpLook               // A: negative, B: behind, C: body start, D:assertEnd pc
	OpAssertEnd          // A: negative
	OpAdvance            // search: skip current start, try next rune
	OpFail               // explicit terminal failure
)

func (o Op) String() string {
	names := [...]string{"", "char", "any", "class", "anchor", "ref", "save",
		"start", "jmp", "match", "look", "assertEnd", "advance", "fail"}
	if int(o) < len(names) {
		return names[o]
	}
	return "?"
}

// Inst is one machine instruction.
type Inst struct {
	Op Op `json:"op"`
	A  int `json:"a,omitempty"`
	B  int `json:"b,omitempty"`
	C  int `json:"c,omitempty"`
	D  int `json:"d,omitempty"`
	// R carries the literal rune for OpChar.
	R rune `json:"r,omitempty"`
	// ClassID points into Program.Classes for OpClass.
	ClassID int `json:"classId,omitempty"`
	// Label is shown by the trace UI (e.g. branch name, capture group).
	Label string `json:"label,omitempty"`
}

// Program is the immutable compiled artifact recorded with every result.
type Program struct {
	Insts    []Inst       `json:"insts"`
	Classes  []*CharClass `json:"classes"`
	Groups   []GroupInfo  `json:"groups"`
	Names    map[string]int `json:"names"`
	NumSlots int          `json:"numSlots"`
	Start    int          `json:"startPc"`
	Search   bool         `json:"search,omitempty"`
	Source   string       `json:"source"`
}

type compiler struct {
	prog       *Program
	boundLimit int
	err        error
}

// Compile turns a parsed pattern into an executable program.
func Compile(pat *Pattern, search bool) (*Program, error) {
	c := &compiler{
		prog: &Program{
			Classes:    []*CharClass{},
			Groups:     pat.Groups,
			Names:      pat.Names,
			NumSlots:   2 * (len(pat.Groups)),
		Search:     search,
		Source:     pat.Source,
	},
		boundLimit: 100000,
	}
	// validate captures/names
	for _, g := range pat.Groups {
		if g.Name != "" {
			if pat.Names[g.Name] == -1 {
				return nil, &ParseError{Msg: fmt.Sprintf("duplicate group name %q", g.Name)}
			}
		}
	}
	if search {
		// searchStart: Start(greedy, advancePc); body...; Match; advancePc: Advance
		advPc := 0 // patched
		c.emit(Inst{Op: OpStart, A: 1, B: 0, Label: "search-start"}, -1)
		c.prog.Start = 0
		c.compileNode(pat.Root)
		c.emit(Inst{Op: OpMatch}, -1)
		advPc = c.emit(Inst{Op: OpAdvance}, -1)
		c.prog.Insts[0].B = advPc
	} else {
		c.prog.Start = 0
		c.compileNode(pat.Root)
		c.emit(Inst{Op: OpMatch}, -1)
	}
	if c.err != nil {
		return nil, c.err
	}
	if len(c.prog.Insts) > 500000 {
		return nil, &ParseError{Msg: "compiled program exceeds instruction budget (500000)"}
	}
	// Resolve backreferences to group indices (forward refs already valid).
	return c.prog, nil
}

func (c *compiler) emit(inst Inst, nodeID int) int {
	c.prog.Insts = append(c.prog.Insts, inst)
	return len(c.prog.Insts) - 1
}

func (c *compiler) addClass(cc *CharClass) int {
	c.prog.Classes = append(c.prog.Classes, cc)
	return len(c.prog.Classes) - 1
}

func (c *compiler) compileNode(n Node) {
	if c.err != nil {
		return
	}
	switch t := n.(type) {
	case *Concat:
		for _, ch := range t.Nodes {
			c.compileNode(ch)
		}
	case *Char:
		c.emit(Inst{Op: OpChar, R: t.R, A: b2i(t.CI)}, nodeIDOf(n))
	case *AnyChar:
		c.emit(Inst{Op: OpAnyChar, A: b2i(t.DotAll)}, nodeIDOf(n))
	case *CharClass:
		id := c.addClass(t)
		c.emit(Inst{Op: OpClass, ClassID: id, A: id}, nodeIDOf(n))
	case *Anchor:
		c.emit(Inst{Op: OpAnchor, A: anchorCode(t.Kind), B: b2i(t.Multiline)}, nodeIDOf(n))
	case *Ref:
		if t.Index <= 0 || t.Index >= len(c.prog.Groups) {
			c.err = &ParseError{Msg: fmt.Sprintf("backreference to undefined group %d", t.Index)}
			return
		}
		c.emit(Inst{Op: OpRef, A: t.Index, B: b2i(t.CI)}, nodeIDOf(n))
	case *Group:
		if t.Index == 0 {
			c.compileNode(t.Child)
			return
		}
		open := c.emit(Inst{Op: OpSave, A: 2*t.Index, Label: fmt.Sprintf("open %d", t.Index)}, nodeIDOf(n))
		_ = open
		c.compileNode(t.Child)
		c.emit(Inst{Op: OpSave, A: 2*t.Index+1, Label: fmt.Sprintf("close %d", t.Index)}, nodeIDOf(n))
	case *Alt:
		c.compileAlt(t)
	case *Repeat:
		c.compileRepeat(t)
	case *Lookaround:
		c.compileLook(t)
	default:
		c.err = &ParseError{Msg: "unknown AST node during compile"}
	}
}

func (c *compiler) compileAlt(t *Alt) {
	if len(t.Branches) == 0 {
		return
	}
	jumps := []int{}
	for i, b := range t.Branches {
		if i < len(t.Branches)-1 {
			altPc := 0 // patch later
			sp := c.emit(Inst{Op: OpStart, A: 0, B: 0, Label: fmt.Sprintf("alt branch %d", i+1)}, nodeIDOf(t))
			altPc = sp
			c.compileNode(b)
			jp := c.emit(Inst{Op: OpJmp, A: 0, Label: "alt end"}, nodeIDOf(t))
			jumps = append(jumps, jp)
			c.prog.Insts[altPc].B = len(c.prog.Insts) // alt continues at next branch
		} else {
			c.compileNode(b)
		}
	}
	end := len(c.prog.Insts)
	for _, jp := range jumps {
		c.prog.Insts[jp].A = end
	}
}

func (c *compiler) compileRepeat(t *Repeat) {
	if t.Max >= 0 {
		// Bounded: unroll exact min, then optional copies up to max.
		for i := 0; i < t.Min; i++ {
			c.compileNode(t.Child)
		}
		optional := t.Max - t.Min
		if optional > c.boundLimit {
			c.err = &ParseError{Msg: "bounded repetition exceeds compile budget"}
			return
		}
		jumps := []int{}
		for i := 0; i < optional; i++ {
			sp := c.emit(Inst{Op: OpStart, A: b2i(!t.Lazy), B: 0,
				Label: fmt.Sprintf("repeat opt %d", i+1)}, nodeIDOf(t))
			c.compileNode(t.Child)
			jp := c.emit(Inst{Op: OpJmp, A: 0}, nodeIDOf(t))
			jumps = append(jumps, jp)
			c.prog.Insts[sp].B = len(c.prog.Insts)
		}
		end := len(c.prog.Insts)
		for _, jp := range jumps {
			c.prog.Insts[jp].A = end
		}
		return
	}
	// Unbounded: min copies then a loop.
	for i := 0; i < t.Min; i++ {
		c.compileNode(t.Child)
	}
	head := len(c.prog.Insts)
	sp := c.emit(Inst{Op: OpStart, A: b2i(!t.Lazy), B: 0,
		Label: boolLabel(t.Lazy, "lazy-loop", "greedy-loop")}, nodeIDOf(t))
	c.compileNode(t.Child)
	c.emit(Inst{Op: OpJmp, A: head}, nodeIDOf(t))
	c.prog.Insts[sp].B = len(c.prog.Insts)
}

func boolLabel(lazy bool, lz, gr string) string {
	if lazy {
		return lz
	}
	return gr
}

func (c *compiler) compileLook(t *Lookaround) {
	bodyStart := len(c.prog.Insts) + 1
	maxLen := -1
	if t.Behind {
		maxLen = maxLookbehindLen(t.Child)
	}
	c.emit(Inst{Op: OpLook, A: b2i(t.Negative), B: b2i(t.Behind), C: bodyStart, D: 0, Label: lookLabel(t)}, nodeIDOf(t))
	id := len(c.prog.Insts) - 1
	c.compileNode(t.Child)
	endPc := c.emit(Inst{Op: OpAssertEnd, A: b2i(t.Negative)}, nodeIDOf(t))
	c.prog.Insts[id].D = endPc
	if t.Behind {
		c.prog.Insts[id].D = endPc
		c.prog.Insts[id].C = bodyStart
		c.prog.Insts[id].ClassID = maxLen
	}
}

func lookLabel(t *Lookaround) string {
	switch {
	case t.Negative && t.Behind:
		return "negative-lookbehind"
	case t.Negative:
		return "negative-lookahead"
	case t.Behind:
		return "positive-lookbehind"
	default:
		return "positive-lookahead"
	}
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func anchorCode(k AnchorKind) int {
	switch k {
	case AnchorStartLine:
		return 1
	case AnchorEndLine:
		return 2
	case AnchorStartText:
		return 3
	case AnchorEndText:
		return 4
	case AnchorEndTextNL:
		return 5
	case AnchorWord:
		return 6
	case AnchorNotWord:
		return 7
	}
	return 0
}

// nodeIDOf is a placeholder hook: source spans in the AST already identify
// nodes for the UI, so instructions don't need a separate node id.
func nodeIDOf(n Node) int {
	s, _ := n.Span()
	return s
}
