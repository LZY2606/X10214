package engine

import (
	"fmt"

	"capturetrace/internal/syntax"
)

// Op is a VM operation code.
type Op uint8

const (
	OpChar Op = iota + 1
	OpClass
	OpAny
	OpSplit
	OpJump
	OpSave
	OpMatch
	OpFail
	OpAnchor
	OpBackref
	OpLAMark // lookaround marker
)

func (o Op) String() string {
	names := map[Op]string{
		OpChar: "char", OpClass: "class", OpAny: "any", OpSplit: "split",
		OpJump: "jump", OpSave: "save", OpMatch: "match", OpFail: "fail",
		OpAnchor: "anchor", OpBackref: "backref", OpLAMark: "lookaround",
	}
	if s, ok := names[o]; ok {
		return s
	}
	return "?"
}

// Instr is one VM instruction with operands.
type Instr struct {
	Op      Op
	R       rune
	M       *Matcher
	Anchor  syntax.AnchorKind
	A, B    int
	Group   int
	Pos     int
	QuantID int
	// LAMark fields
	Body   int
	Cont   int
	Neg    bool
	Ahead  bool
	MaxW   int
	LookID int
	Greedy bool
}

// Program is the compiled pattern.
type Program struct {
	Code        []Instr
	NumGroups   int
	Groups      []syntax.Group
	Pat         *syntax.Pattern
	QuantGreedy map[int]bool
}

type compiler struct {
	code    []Instr
	pat     *syntax.Pattern
	quantID int
	lookID  int
	qg      map[int]bool
}

// Compile lowers a parsed pattern to VM bytecode.
func Compile(pat *syntax.Pattern) (*Program, error) {
	c := &compiler{pat: pat, qg: map[int]bool{}}
	if err := c.compile(pat.Root); err != nil {
		return nil, err
	}
	c.emit(Instr{Op: OpMatch, Pos: pat.Root.End})
	return &Program{Code: c.code, NumGroups: pat.NumGroups, Groups: pat.Groups, Pat: pat, QuantGreedy: c.qg}, nil
}

func (c *compiler) emit(i Instr) int {
	c.code = append(c.code, i)
	return len(c.code) - 1
}

func (c *compiler) compile(a *syntax.Atom) error {
	switch a.Kind {
	case syntax.KindConcat:
		for _, n := range a.Nodes {
			if err := c.compile(n); err != nil {
				return err
			}
		}
	case syntax.KindAlt:
		// Documented order: branches are tried strictly source-first.
		var splits, jumps, ends []int
		for i, n := range a.Nodes {
			if i < len(a.Nodes)-1 {
				splits = append(splits, c.emit(Instr{Op: OpSplit, Pos: a.Pos}))
			}
			if err := c.compile(n); err != nil {
				return err
			}
			if i < len(a.Nodes)-1 {
				jumps = append(jumps, c.emit(Instr{Op: OpJump, A: 0, Pos: a.End}))
			}
			ends = append(ends, len(c.code))
		}
		target := len(c.code)
		for i, si := range splits {
			c.code[si].A = si + 1    // body of branch i
			c.code[si].B = ends[i+1] // start of next branch
		}
		for _, j := range jumps {
			c.code[j].A = target
		}
	case syntax.KindLiteral:
		c.emit(Instr{Op: OpChar, R: a.Value, Pos: a.Pos})
	case syntax.KindAny:
		c.emit(Instr{Op: OpAny, Pos: a.Pos})
	case syntax.KindAnchor:
		c.emit(Instr{Op: OpAnchor, Anchor: a.Anchor, Pos: a.Pos})
	case syntax.KindClass:
		m, err := compileMatcher(a)
		if err != nil {
			return err
		}
		c.emit(Instr{Op: OpClass, M: m, Pos: a.Pos})
	case syntax.KindProperty:
		m, err := compileProperty(a)
		if err != nil {
			return err
		}
		c.emit(Instr{Op: OpClass, M: m, Pos: a.Pos})
	case syntax.KindBackref:
		c.emit(Instr{Op: OpBackref, A: a.Index, Pos: a.Pos})
	case syntax.KindGroup:
		if a.Capture {
			c.emit(Instr{Op: OpSave, A: 2 * a.Index, Group: a.Index, Pos: a.Pos})
			if err := c.compile(a.Target); err != nil {
				return err
			}
			c.emit(Instr{Op: OpSave, A: 2*a.Index + 1, Group: a.Index, Pos: a.End})
		} else if err := c.compile(a.Target); err != nil {
			return err
		}
	case syntax.KindQuant:
		return c.compileQuant(a)
	case syntax.KindLook:
		return c.compileLook(a)
	default:
		return fmt.Errorf("internal: cannot compile node %s", a.Kind)
	}
	return nil
}

func (c *compiler) compileQuant(a *syntax.Atom) error {
	c.quantID++
	id := c.quantID
	max := a.Max
	if a.Min > 0 {
		for i := 0; i < a.Min; i++ {
			if err := c.compile(a.Target); err != nil {
				return err
			}
		}
	}
	if max == -1 {
		si := c.emit(Instr{Op: OpSplit, QuantID: id, Pos: a.Pos, Greedy: a.Mode == syntax.Greedy})
		c.qg[id] = a.Mode == syntax.Greedy
		body := len(c.code)
		if err := c.compile(a.Target); err != nil {
			return err
		}
		c.emit(Instr{Op: OpJump, A: si, QuantID: id, Pos: a.End})
		cont := len(c.code)
		if a.Mode == syntax.Greedy {
			c.code[si].A, c.code[si].B = body, cont
		} else {
			c.code[si].A, c.code[si].B = cont, body
		}
		return nil
	}
	for i := a.Min; i < max; i++ {
		si := c.emit(Instr{Op: OpSplit, QuantID: id, Pos: a.Pos, Greedy: a.Mode == syntax.Greedy})
		c.qg[id] = a.Mode == syntax.Greedy
		body := len(c.code)
		if err := c.compile(a.Target); err != nil {
			return err
		}
		cont := len(c.code)
		if a.Mode == syntax.Greedy {
			c.code[si].A, c.code[si].B = body, cont
		} else {
			c.code[si].A, c.code[si].B = cont, body
		}
	}
	return nil
}

func (c *compiler) compileLook(a *syntax.Atom) error {
	c.lookID++
	id := c.lookID
	mi := c.emit(Instr{Op: OpLAMark, Pos: a.Pos, Neg: a.LookNeg, Ahead: a.LookAhead,
		MaxW: a.LookMax, LookID: id})
	body := len(c.code)
	if err := c.compile(a.Target); err != nil {
		return err
	}
	c.emit(Instr{Op: OpMatch, Pos: a.End, LookID: id})
	cont := len(c.code)
	c.code[mi].Body = body
	c.code[mi].Cont = cont
	return nil
}
