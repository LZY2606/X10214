package regex

import "fmt"

// Op is a VM opcode.
type Op int

const (
	OpChar Op = iota // match one specific rune
	OpClass          // match one rune in class
	OpAny            // match any rune except '\n'
	OpString         // match a folded run of literal runes (optimization)
	OpSplit          // try A first, defer B (priority order = documented order)
	OpJmp
	OpSave // write current byte offset into slot
	OpBackref
	OpLook // enter lookaround subprogram
	OpBol  // ^ anchor
	OpEol  // $ anchor
	OpMatch
)

func (op Op) String() string {
	switch op {
	case OpChar:
		return "Char"
	case OpClass:
		return "Class"
	case OpAny:
		return "Any"
	case OpString:
		return "String"
	case OpSplit:
		return "Split"
	case OpJmp:
		return "Jmp"
	case OpSave:
		return "Save"
	case OpBackref:
		return "Backref"
	case OpLook:
		return "Look"
	case OpBol:
		return "Bol"
	case OpEol:
		return "Eol"
	case OpMatch:
		return "Match"
	}
	return "?"
}

// Inst is one compiled instruction.
type Inst struct {
	Op     Op
	Rune   rune      // OpChar
	Runes  []rune    // OpString
	Ranges [][2]rune // OpClass
	Neg    bool      // OpClass
	A, B   int       // OpSplit branches / OpJmp target
	Slot   int       // OpSave
	Group  int       // OpBackref
	Sub    int       // OpLook: index into Program.Looks
	Ahead  bool      // OpLook direction
	Pos    bool      // OpLook polarity
}

// Program is a compiled pattern.
type Program struct {
	Code      []Inst
	Looks     [][]Inst
	NumGroups int
	Names     map[string]int
	Optimized bool
}

// maxUnroll bounds {m,n} expansion so pathological patterns fail at compile time.
const maxUnroll = 256

// Compile lowers an AST into a Program. When optimize is true, runs of
// adjacent literal runes fold into OpString instructions; this never crosses
// Split/Save boundaries, so branch priority order is preserved.
func Compile(p *Pattern, optimize bool) (*Program, error) {
	c := &compiler{prog: &Program{NumGroups: p.NumGroups, Names: p.GroupNames, Optimized: optimize}, optimize: optimize}
	c.emit(Inst{Op: OpSave, Slot: 0})
	if err := c.compile(p.Root); err != nil {
		return nil, err
	}
	c.emit(Inst{Op: OpSave, Slot: 1})
	c.emit(Inst{Op: OpMatch})
	return c.prog, nil
}

type compiler struct {
	prog     *Program
	optimize bool
}

func (c *compiler) emit(in Inst) int {
	c.prog.Code = append(c.prog.Code, in)
	return len(c.prog.Code) - 1
}

func (c *compiler) compile(n *Node) error {
	switch n.Kind {
	case KindEmpty:
		return nil
	case KindLiteral:
		c.emit(Inst{Op: OpChar, Rune: n.Rune})
	case KindAny:
		c.emit(Inst{Op: OpAny})
	case KindClass:
		c.emit(Inst{Op: OpClass, Ranges: n.Ranges, Neg: n.Negated})
	case KindAnchorStart:
		c.emit(Inst{Op: OpBol})
	case KindAnchorEnd:
		c.emit(Inst{Op: OpEol})
	case KindConcat:
		if c.optimize {
			return c.compileConcatOpt(n)
		}
		for _, k := range n.Kids {
			if err := c.compile(k); err != nil {
				return err
			}
		}
	case KindAlt:
		// Left alternative has priority: Split body, rest.
		if len(n.Kids) == 0 {
			return nil
		}
		return c.compileAlt(n.Kids)
	case KindGroup:
		if n.GroupIdx > 0 {
			c.emit(Inst{Op: OpSave, Slot: 2 * n.GroupIdx})
			if err := c.compile(n.Kids[0]); err != nil {
				return err
			}
			c.emit(Inst{Op: OpSave, Slot: 2*n.GroupIdx + 1})
			return nil
		}
		return c.compile(n.Kids[0])
	case KindRepeat:
		return c.compileRepeat(n)
	case KindBackref:
		idx := n.RefIdx
		if n.RefName != "" {
			g, ok := c.prog.Names[n.RefName]
			if !ok {
				return fmt.Errorf("compile: unknown group name %q", n.RefName)
			}
			idx = g
		}
		if idx <= 0 || idx > c.prog.NumGroups {
			return fmt.Errorf("compile: backreference to unknown group %d", idx)
		}
		c.emit(Inst{Op: OpBackref, Group: idx})
	case KindLook:
		sub := &compiler{prog: &Program{NumGroups: c.prog.NumGroups, Names: c.prog.Names}, optimize: c.optimize}
		if err := sub.compile(n.Kids[0]); err != nil {
			return err
		}
		sub.emit(Inst{Op: OpMatch})
		c.prog.Looks = append(c.prog.Looks, sub.prog.Code)
		c.emit(Inst{Op: OpLook, Sub: len(c.prog.Looks) - 1, Ahead: n.Ahead, Pos: n.Positive})
	}
	return nil
}

// compileConcatOpt folds adjacent literal runes into OpString. Only Literal
// siblings merge, so capture/split structure — and therefore documented
// branch order — is untouched.
func (c *compiler) compileConcatOpt(n *Node) error {
	var pending []rune
	flush := func() {
		if len(pending) == 0 {
			return
		}
		if len(pending) == 1 {
			c.emit(Inst{Op: OpChar, Rune: pending[0]})
		} else {
			c.emit(Inst{Op: OpString, Runes: append([]rune(nil), pending...)})
		}
		pending = pending[:0]
	}
	for _, k := range n.Kids {
		if k.Kind == KindLiteral {
			pending = append(pending, k.Rune)
			continue
		}
		flush()
		if err := c.compile(k); err != nil {
			return err
		}
	}
	flush()
	return nil
}

func (c *compiler) compileAlt(kids []*Node) error {
	if len(kids) == 1 {
		return c.compile(kids[0])
	}
	split := c.emit(Inst{Op: OpSplit})
	if err := c.compile(kids[0]); err != nil {
		return err
	}
	jmp := c.emit(Inst{Op: OpJmp})
	rest := len(c.prog.Code)
	if err := c.compileAlt(kids[1:]); err != nil {
		return err
	}
	end := len(c.prog.Code)
	c.prog.Code[split].A = split + 1
	c.prog.Code[split].B = rest
	c.prog.Code[jmp].A = end
	return nil
}

func (c *compiler) compileRepeat(n *Node) error {
	child := n.Kids[0]
	if n.Min > maxUnroll || (n.Max >= 0 && n.Max > maxUnroll) {
		return fmt.Errorf("compile: quantifier {%d,%d} exceeds unroll limit %d", n.Min, n.Max, maxUnroll)
	}
	for i := 0; i < n.Min; i++ {
		if err := c.compile(child); err != nil {
			return err
		}
	}
	if n.Max < 0 {
		// Star loop over the remaining copies.
		split := c.emit(Inst{Op: OpSplit})
		body := len(c.prog.Code)
		if err := c.compile(child); err != nil {
			return err
		}
		c.emit(Inst{Op: OpJmp, A: split})
		end := len(c.prog.Code)
		if n.Lazy {
			c.prog.Code[split].A = end
			c.prog.Code[split].B = body
		} else {
			c.prog.Code[split].A = body
			c.prog.Code[split].B = end
		}
		return nil
	}
	// Bounded tail: (max-min) nested optional copies sharing one exit.
	var splits []int
	for i := n.Min; i < n.Max; i++ {
		s := c.emit(Inst{Op: OpSplit})
		splits = append(splits, s)
		body := len(c.prog.Code)
		if err := c.compile(child); err != nil {
			return err
		}
		if n.Lazy {
			c.prog.Code[s].A = -1 // patched to end
			c.prog.Code[s].B = body
		} else {
			c.prog.Code[s].A = body
			c.prog.Code[s].B = -1
		}
	}
	end := len(c.prog.Code)
	for _, s := range splits {
		if n.Lazy {
			c.prog.Code[s].A = end
		} else {
			c.prog.Code[s].B = end
		}
	}
	return nil
}
