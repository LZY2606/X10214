package engine

import (
	"fmt"
	"strings"
	"unicode"

	"pairwise-gsb/internal/syntax"
)

type Op int

const (
	OpRune Op = iota
	OpClass
	OpAny
	OpSplit // try A first, defer B (documented branch order)
	OpJmp
	OpSave
	OpProgress // zero-width repeat guard
	OpBackref
	OpAssert
	OpLook
	OpMatch
)

func (op Op) String() string {
	switch op {
	case OpRune:
		return "RUNE"
	case OpClass:
		return "CLASS"
	case OpAny:
		return "ANY"
	case OpSplit:
		return "SPLIT"
	case OpJmp:
		return "JMP"
	case OpSave:
		return "SAVE"
	case OpProgress:
		return "PROGRESS"
	case OpBackref:
		return "BACKREF"
	case OpAssert:
		return "ASSERT"
	case OpLook:
		return "LOOK"
	case OpMatch:
		return "MATCH"
	}
	return "?"
}

// Class is a compiled character class predicate.
type Class struct {
	Ranges []syntax.Range
	Cats   []*unicode.RangeTable
	Preds  []string
	Negate bool
	Desc   string
}

// IsWordRune defines the \w alphabet used by classes and \b.
func IsWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func (c *Class) Match(r rune) bool {
	ok := false
	for _, rg := range c.Ranges {
		if r >= rg.Lo && r <= rg.Hi {
			ok = true
			break
		}
	}
	if !ok {
		for _, t := range c.Cats {
			if unicode.Is(t, r) {
				ok = true
				break
			}
		}
	}
	if !ok {
		for _, p := range c.Preds {
			neg := strings.HasPrefix(p, "!")
			name := strings.TrimPrefix(p, "!")
			var hit bool
			switch name {
			case "digit":
				hit = unicode.IsDigit(r)
			case "word":
				hit = IsWordRune(r)
			case "space":
				hit = unicode.IsSpace(r)
			}
			if neg {
				hit = !hit
			}
			if hit {
				ok = true
				break
			}
		}
	}
	if c.Negate {
		return !ok
	}
	return ok
}

// Inst is one compiled instruction.
type Inst struct {
	Op     Op
	R      rune
	Class  *Class
	A, B   int // Split targets / Jmp target
	Slot   int // Save slot / Progress mark slot
	Group  int // Backref group index
	Anchor syntax.AnchorKind
	Sub    int // Look subprogram index
	Look   syntax.LookKind
}

// Program is a compiled pattern.
type Program struct {
	Insts    []Inst
	NumSlots int // capture slots: 2*(groups+1)
	NumMarks int // zero-width guard marks
	Groups   []string
	Subs     []*Program
}

type compiler struct {
	insts []Inst
	subs  []*Program
	marks int
	info  *syntax.Info
}

// Compile lowers an AST into a Program.
func Compile(root *syntax.Node, info *syntax.Info) (*Program, error) {
	c := &compiler{info: info}
	c.emit(Inst{Op: OpSave, Slot: 0})
	if err := c.node(root); err != nil {
		return nil, err
	}
	c.emit(Inst{Op: OpSave, Slot: 1})
	c.emit(Inst{Op: OpMatch})
	groups := make([]string, info.Groups+1)
	for name, idx := range info.Names {
		if idx >= 0 && idx < len(groups) {
			groups[idx] = name
		}
	}
	return &Program{
		Insts:    c.insts,
		NumSlots: 2 * (info.Groups + 1),
		NumMarks: c.marks,
		Groups:   groups,
		Subs:     c.subs,
	}, nil
}

func (c *compiler) emit(in Inst) int {
	c.insts = append(c.insts, in)
	return len(c.insts) - 1
}

func (c *compiler) node(n *syntax.Node) error {
	switch n.Kind {
	case syntax.Empty:
	case syntax.Literal:
		for _, r := range n.Lit {
			c.emit(Inst{Op: OpRune, R: r})
		}
	case syntax.AnyChar:
		c.emit(Inst{Op: OpAny})
	case syntax.CharClass:
		cl, err := buildClass(n)
		if err != nil {
			return err
		}
		c.emit(Inst{Op: OpClass, Class: cl})
	case syntax.Concat:
		for _, ch := range n.Children {
			if err := c.node(ch); err != nil {
				return err
			}
		}
	case syntax.Alternate:
		return c.alt(n)
	case syntax.Group:
		c.emit(Inst{Op: OpSave, Slot: 2 * n.Index})
		if err := c.node(n.Children[0]); err != nil {
			return err
		}
		c.emit(Inst{Op: OpSave, Slot: 2*n.Index + 1})
	case syntax.Backref:
		idx := n.Index
		if n.Name != "" {
			v, ok := c.info.Names[n.Name]
			if !ok {
				return fmt.Errorf("compile: unknown group name %q", n.Name)
			}
			idx = v
		}
		if idx <= 0 || idx > c.info.Groups {
			return fmt.Errorf("compile: backreference to unknown group %d", idx)
		}
		c.emit(Inst{Op: OpBackref, Group: idx})
	case syntax.Anchor:
		c.emit(Inst{Op: OpAssert, Anchor: n.Anchor})
	case syntax.Lookaround:
		sub := &compiler{info: c.info}
		sub.emit(Inst{Op: OpSave, Slot: 0})
		if err := sub.node(n.Children[0]); err != nil {
			return err
		}
		sub.emit(Inst{Op: OpSave, Slot: 1})
		sub.emit(Inst{Op: OpMatch})
		idx := len(c.subs)
		c.subs = append(c.subs, &Program{
			Insts:    sub.insts,
			NumSlots: 2 * (c.info.Groups + 1),
			NumMarks: sub.marks,
			Subs:     sub.subs,
		})
		c.emit(Inst{Op: OpLook, Sub: idx, Look: n.Look})
	case syntax.Repeat:
		return c.repeat(n)
	default:
		return fmt.Errorf("compile: unsupported node %v", n.Kind)
	}
	return nil
}

// alt compiles alternation. Branch preference is left-to-right, the
// documented order; every later transformation must preserve it.
func (c *compiler) alt(n *syntax.Node) error {
	var jmps []int
	for i, br := range n.Children {
		if i < len(n.Children)-1 {
			sp := c.emit(Inst{Op: OpSplit})
			c.insts[sp].A = len(c.insts)
			if err := c.node(br); err != nil {
				return err
			}
			jmps = append(jmps, c.emit(Inst{Op: OpJmp}))
			c.insts[sp].B = len(c.insts)
		} else {
			if err := c.node(br); err != nil {
				return err
			}
		}
	}
	end := len(c.insts)
	for _, j := range jmps {
		c.insts[j].A = end
	}
	return nil
}

func (c *compiler) repeat(n *syntax.Node) error {
	child := n.Children[0]
	if n.Min > syntax.MaxRepeat || (n.Max >= 0 && n.Max > syntax.MaxRepeat) {
		return fmt.Errorf("compile: repeat bound too large")
	}
	for i := 0; i < n.Min; i++ {
		if err := c.node(child); err != nil {
			return err
		}
	}
	if n.Max == -1 {
		return c.star(child, n.Lazy)
	}
	return c.options(child, n.Max-n.Min, n.Lazy)
}

// star emits a guarded unbounded loop. The PROGRESS instruction
// terminates iterations that match the empty string.
func (c *compiler) star(child *syntax.Node, lazy bool) error {
	mark := c.marks
	c.marks++
	sp := c.emit(Inst{Op: OpSplit})
	body := len(c.insts)
	if err := c.node(child); err != nil {
		return err
	}
	c.emit(Inst{Op: OpProgress, Slot: mark})
	c.emit(Inst{Op: OpJmp, A: sp})
	exit := len(c.insts)
	if lazy {
		c.insts[sp].A, c.insts[sp].B = exit, body
	} else {
		c.insts[sp].A, c.insts[sp].B = body, exit
	}
	return nil
}

func (c *compiler) options(child *syntax.Node, count int, lazy bool) error {
	if count == 0 {
		return nil
	}
	sp := c.emit(Inst{Op: OpSplit})
	body := len(c.insts)
	if err := c.node(child); err != nil {
		return err
	}
	if err := c.options(child, count-1, lazy); err != nil {
		return err
	}
	exit := len(c.insts)
	if lazy {
		c.insts[sp].A, c.insts[sp].B = exit, body
	} else {
		c.insts[sp].A, c.insts[sp].B = body, exit
	}
	return nil
}

func buildClass(n *syntax.Node) (*Class, error) {
	cl := &Class{
		Ranges: n.Ranges,
		Negate: n.Negate,
		Desc:   describeClass(n),
	}
	for _, name := range n.Cats {
		neg := strings.HasPrefix(name, "!")
		base := strings.TrimPrefix(name, "!")
		t := unicode.Categories[base]
		if t == nil {
			return nil, fmt.Errorf("compile: unknown unicode category %q", base)
		}
		if neg {
			cl.Preds = append(cl.Preds, "!cat:"+base)
		} else {
			cl.Cats = append(cl.Cats, t)
		}
	}
	cl.Preds = append(cl.Preds, n.Preds...)
	return cl, nil
}

func describeClass(n *syntax.Node) string {
	var b strings.Builder
	b.WriteByte('[')
	if n.Negate {
		b.WriteByte('^')
	}
	for _, r := range n.Ranges {
		if r.Lo == r.Hi {
			fmt.Fprintf(&b, "%q", string(r.Lo))
		} else {
			fmt.Fprintf(&b, "%q-%q", string(r.Lo), string(r.Hi))
		}
	}
	for _, c := range n.Cats {
		fmt.Fprintf(&b, "\\p{%s}", c)
	}
	for _, p := range n.Preds {
		fmt.Fprintf(&b, "<%s>", p)
	}
	b.WriteByte(']')
	return b.String()
}

// Optimize applies jump threading: JMP/SPLIT targets that point at JMP
// instructions are redirected to the final destination. It never
// reorders SPLIT alternatives, so documented branch preference and the
// observable trace (branch decisions, captures, failures) are preserved.
func Optimize(p *Program) *Program {
	resolve := func(pc int) int {
		seen := map[int]bool{}
		for pc >= 0 && pc < len(p.Insts) && p.Insts[pc].Op == OpJmp && !seen[pc] {
			seen[pc] = true
			pc = p.Insts[pc].A
		}
		return pc
	}
	np := *p
	insts := make([]Inst, len(p.Insts))
	copy(insts, p.Insts)
	for i := range insts {
		switch insts[i].Op {
		case OpJmp:
			insts[i].A = resolve(insts[i].A)
		case OpSplit:
			insts[i].A = resolve(insts[i].A)
			insts[i].B = resolve(insts[i].B)
		}
	}
	np.Insts = insts
	if len(p.Subs) > 0 {
		np.Subs = make([]*Program, len(p.Subs))
		for i, s := range p.Subs {
			np.Subs[i] = Optimize(s)
		}
	}
	return &np
}

// Disasm renders the program for display.
func (p *Program) Disasm() []string {
	out := make([]string, len(p.Insts))
	for i, in := range p.Insts {
		out[i] = fmt.Sprintf("%3d  %s", i, disasmInst(in))
	}
	return out
}

func disasmInst(in Inst) string {
	switch in.Op {
	case OpRune:
		return fmt.Sprintf("RUNE %q", string(in.R))
	case OpClass:
		return "CLASS " + in.Class.Desc
	case OpAny:
		return "ANY"
	case OpSplit:
		return fmt.Sprintf("SPLIT try=%d defer=%d", in.A, in.B)
	case OpJmp:
		return fmt.Sprintf("JMP %d", in.A)
	case OpSave:
		return fmt.Sprintf("SAVE slot=%d", in.Slot)
	case OpProgress:
		return fmt.Sprintf("PROGRESS mark=%d", in.Slot)
	case OpBackref:
		return fmt.Sprintf("BACKREF g%d", in.Group)
	case OpAssert:
		return "ASSERT " + in.Anchor.String()
	case OpLook:
		return fmt.Sprintf("LOOK %ssub=%d", in.Look.String(), in.Sub)
	case OpMatch:
		return "MATCH"
	}
	return "?"
}
