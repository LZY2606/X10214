package rx

type OpCode int

const (
	opChar OpCode = iota
	opClass
	opDot
	opSave
	opSplit
	opJmp
	opMatch
	opQEnter
	opQGuard
	opQRepeat
	opAnchor
	opBackref
	opLookStart
	opLookEnd
)

type Edge int

const (
	EdgeStart Edge = iota
	EdgeEnd
)

// Instr is one bytecode instruction. Targets are instruction indexes.
type Instr struct {
	Op      OpCode
	NodeID  int
	Target  int     // split/jmp target
	Target2 int     // quantifier body start
	Rune    rune    // opChar
	Fold    bool    // opChar/opClass/opBackref case folding
	Class   *CharClass
	DotAll  bool
	Group   int
	Edge    Edge
	QID     int
	Min     int
	Max     int // -1 unbounded
	Lazy    bool
	Anchor  AnchorKind
	Multiline bool
	Negate  bool
	Behind  bool
	ChildEnd int // lookaround: index of opLookEnd
}

// Program is a compiled pattern body (lookaround children are inlined).
type Program struct {
	Code   []Instr
	Groups []GroupInfo
	Pat    *Pattern
}

func (pr *Program) emit(i Instr) int {
	idx := len(pr.Code)
	pr.Code = append(pr.Code, i)
	return idx
}

type compiler struct {
	pr    *Program
	qseed int
}

// Compile builds the executable program from a parsed pattern.
func Compile(pat *Pattern) *Program {
	c := &compiler{pr: &Program{Groups: pat.Groups, Pat: pat}}
	c.compileNode(pat.Root)
	c.pr.emit(Instr{Op: opMatch, NodeID: pat.Root.ID})
	c.resolveJumps()
	return c.pr
}

func (c *compiler) compileNode(n *Node) {
	switch n.Kind {
	case NEmpty:
		// nothing
	case NConcat:
		for _, ch := range n.Children {
			c.compileNode(ch)
		}
	case NChar:
		c.pr.emit(Instr{Op: opChar, NodeID: n.ID, Rune: n.Rune, Fold: n.Flags.I})
	case NDot:
		c.pr.emit(Instr{Op: opDot, NodeID: n.ID, DotAll: n.Flags.S})
	case NClass:
		c.pr.emit(Instr{Op: opClass, NodeID: n.ID, Class: n.Class, Fold: n.Flags.I})
	case NProperty:
		c.pr.emit(Instr{Op: opClass, NodeID: n.ID, Fold: false,
			Class: &CharClass{Props: []PropItem{{Name: n.Name, Negate: n.Negate}}}})
	case NQuote:
		// literal bytes are always ASCII-safe to iterate by rune
		for _, r := range n.Text {
			c.pr.emit(Instr{Op: opChar, NodeID: n.ID, Rune: r, Fold: n.Flags.I})
		}
	case NAnchor:
		c.pr.emit(Instr{Op: opAnchor, NodeID: n.ID, Anchor: n.Anchor, Multiline: n.Flags.M})
	case NBackref:
		c.pr.emit(Instr{Op: opBackref, NodeID: n.ID, Group: n.RefIndex, Fold: n.Flags.I})
	case NGroup:
		if n.RefIndex > 0 {
			c.pr.emit(Instr{Op: opSave, NodeID: n.ID, Group: n.RefIndex, Edge: EdgeStart})
			c.compileNode(n.Children[0])
			c.pr.emit(Instr{Op: opSave, NodeID: n.ID, Group: n.RefIndex, Edge: EdgeEnd})
		} else {
			c.compileNode(n.Children[0])
		}
	case NAlt:
		var jumpEnds []int
		var splits []int
		for i, ch := range n.Children {
			if i < len(n.Children)-1 {
				sp := c.pr.emit(Instr{Op: opSplit, NodeID: n.ID, Target: -1, Lazy: false})
				splits = append(splits, sp)
			}
			c.compileNode(ch)
			if i < len(n.Children)-1 {
				nextStart := len(c.pr.Code) + 1 // jmp follows now, next alt after it
				c.pr.Code[splits[i]].Target = nextStart
				j := c.pr.emit(Instr{Op: opJmp, NodeID: n.ID, Target: -1})
				jumpEnds = append(jumpEnds, j)
			}
		}
		for _, j := range jumpEnds {
			c.pr.Code[j].Target = len(c.pr.Code)
		}
	case NRepeat:
		c.compileRepeat(n)
	case NLookahead, NLookbehind:
		start := c.pr.emit(Instr{Op: opLookStart, NodeID: n.ID, Negate: n.Negate,
			Behind: n.Kind == NLookbehind, ChildEnd: -1})
		c.compileNode(n.Children[0])
		end := c.pr.emit(Instr{Op: opLookEnd, NodeID: n.ID})
		c.pr.Code[start].ChildEnd = end
	}
}

func (c *compiler) compileRepeat(n *Node) {
	c.qseed++
	qid := c.qseed
	c.pr.emit(Instr{Op: opQEnter, NodeID: n.ID, QID: qid, Min: n.Min, Max: n.Max, Lazy: !n.Greedy})
	head := c.pr.emit(Instr{Op: opQGuard, NodeID: n.ID, QID: qid, Min: n.Min, Max: n.Max,
		Target2: -1, Target: -1, Lazy: !n.Greedy})
	body := len(c.pr.Code)
	c.compileNode(n.Children[0])
	c.pr.emit(Instr{Op: opQRepeat, NodeID: n.ID, QID: qid, Target: head})
	after := len(c.pr.Code)
	c.pr.Code[head].Target2 = body
	c.pr.Code[head].Target = after
}

func (c *compiler) resolveJumps() {
	// All control-flow targets are patched during emission. This hook
	// remains so an optional, semantics-preserving optimization pass can
	// be toggled (see OptimizeJumps and the trace-equivalence tests).
}
