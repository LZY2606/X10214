package engine

import "fmt"

// Op is an IR operation executed by the explicit backtracking machine.
type Op string

const (
	OpChar    Op = "char"
	OpDot     Op = "dot"
	OpSplit   Op = "split"
	OpJmp     Op = "jmp"
	OpSave    Op = "save"
	OpAnchor  Op = "anchor"
	OpBackref Op = "backref"
	OpRPhase  Op = "rphase"
	OpRAgain  Op = "ragain"
	OpRReset  Op = "rreset"
	OpLAOpen  Op = "laopen"
	OpLABound Op = "labound"
	OpLAClose Op = "laclose"
	OpLBOpen  Op = "lbopen"
	OpLBClose Op = "lbclose"
	OpMatch   Op = "match"
)

// Inst is one compiled instruction. Target fields Arg/Arg2 hold a label
// reference ("L:xxx") before backpatching and an absolute pc afterwards.
type Inst struct {
	Op    Op          `json:"op"`
	Arg   int         `json:"arg,omitempty"`
	Arg2  int         `json:"arg2,omitempty"`
	Node  int         `json:"node"`
	Match *CharSpec   `json:"match,omitempty"`
	Kind  string      `json:"kind,omitempty"`
	Flags string      `json:"flags,omitempty"`
	Label string      `json:"label,omitempty"`
	LArg  string      `json:"-"`
	LArg2 string      `json:"-"`
	Loop  *LoopSpec   `json:"loop,omitempty"`
}

// LoopSpec carries the quantifier bounds attached to an RPhase.
type LoopSpec struct {
	ID     int  `json:"id"`
	Min    int  `json:"min"`
	Max    int  `json:"max"`
	Greedy bool `json:"greedy"`
}

// CharSpec describes a single-slot matcher.
type CharSpec struct {
	R       rune        `json:"r,omitempty"`
	IsClass bool        `json:"isClass,omitempty"`
	Neg     bool        `json:"neg,omitempty"`
	Terms   []ClassTerm `json:"terms,omitempty"`
}

// Program is compiled IR plus group metadata.
type Program struct {
	Code       []Inst          `json:"code"`
	NumGroups  int             `json:"numGroups"`
	Names      map[string]int  `json:"names"`
	GroupNames map[int]string  `json:"groupNames"`
	AST        *Node           `json:"ast"`
	Pat        string          `json:"pat"`
}

type compiler struct {
	prog  *Program
	loops int
	lc    int
}

// Compile parses and compiles a pattern.
func Compile(pattern string) (*Program, error) {
	p, err := Parse(pattern)
	if err != nil {
		return nil, err
	}
	c := &compiler{prog: &Program{NumGroups: p.NumGroups, Names: p.Names, GroupNames: p.GroupNames, AST: p.AST, Pat: pattern}}
	c.emit(Inst{Op: OpSave, Arg: 0, Node: p.AST.ID})
	c.compileNode(p.AST, "")
	c.emit(Inst{Op: OpMatch, Node: p.AST.ID})
	if err := c.backpatch(); err != nil {
		return nil, err
	}
	return c.prog, nil
}

func (c *compiler) label(prefix string) string {
	c.lc++
	return fmt.Sprintf("%s%d", prefix, c.lc)
}

func (c *compiler) emit(ins Inst) int {
	pc := len(c.prog.Code)
	c.prog.Code = append(c.prog.Code, ins)
	return pc
}

func (c *compiler) mark(label string) {
	c.prog.Code[len(c.prog.Code)-1].Label = label
}

func ref(l string) string { return "L:" + l }

func (c *compiler) backpatch() error {
	addr := map[string]int{}
	for i := range c.prog.Code {
		if l := c.prog.Code[i].Label; l != "" {
			if _, ok := addr[l]; !ok {
				addr[l] = i
			}
		}
	}
	resolve := func(l string) (int, error) {
		if l == "" {
			return 0, nil
		}
		key := l
		if len(l) > 2 && l[:2] == "L:" {
			key = l[2:]
		}
		a, ok := addr[key]
		if !ok {
			return 0, fmt.Errorf("internal: unresolved label %q", l)
		}
		return a, nil
	}
	for i := range c.prog.Code {
		ins := &c.prog.Code[i]
		if ins.LArg != "" {
			a, err := resolve(ins.LArg)
			if err != nil {
				return err
			}
			ins.Arg = a
		}
		if ins.LArg2 != "" {
			a, err := resolve(ins.LArg2)
			if err != nil {
				return err
			}
			ins.Arg2 = a
		}
	}
	return nil
}
