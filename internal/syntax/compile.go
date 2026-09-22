package syntax

import "fmt"

// CompileError reports a problem found while lowering the AST.
type CompileError struct {
	Offset int
	Msg    string
}

func (e *CompileError) Error() string {
	return fmt.Sprintf("compile error at byte %d: %s", e.Offset, e.Msg)
}

type compiler struct {
	insts   []Inst
	classes []ClassNode
	quantID int
	frameID int
	names   map[string]int
	groups  int
}

// Compile lowers an AST into a Program. numGroups and names come from Parse.
func Compile(root Node, numGroups int, names map[string]int) (*Program, error) {
	c := &compiler{names: names, groups: numGroups}
	if err := c.compileNode(root, Flags{}); err != nil {
		return nil, err
	}
	c.emit(Inst{Op: OpMatch})
	return &Program{
		Insts:     c.insts,
		Start:     0,
		NumGroups: numGroups,
		Names:     names,
		Classes:   c.classes,
	}, nil
}

func (c *compiler) emit(in Inst) int {
	c.insts = append(c.insts, in)
	return len(c.insts) - 1
}

func (c *compiler) compileNode(n Node, fl Flags) error {
	switch t := n.(type) {
	case nil:
		return nil
	case *CharNode:
		c.emit(Inst{Op: OpChar, Rune: t.Rune, Raw: t.Raw, Flags: fl})
	case *StrNode:
		c.emit(Inst{Op: OpStr, Str: t.Runes, Flags: t.Flags})
	case *DotNode:
		c.emit(Inst{Op: OpDot, Flags: fl})
	case *ClassNode:
		idx := len(c.classes)
		c.classes = append(c.classes, *t)
		c.emit(Inst{Op: OpClass, Arg: idx, Flags: fl})
	case *ShorthandNode:
		c.emit(Inst{Op: OpShorthand, Arg: int(t.Kind), Flags: fl})
	case *PropNode:
		c.emit(Inst{Op: OpProp, Str: t.Name, Neg: t.Negate, Flags: fl})
	case *AnchorNode:
		c.emit(Inst{Op: OpAnchor, Arg: int(t.Kind), Flags: fl})
	case *BackrefNode:
		idx := t.Index
		if t.Name != "" {
			v, ok := c.names[t.Name]
			if !ok {
				return &CompileError{Offset: t.Offset, Msg: "unknown group name " + t.Name}
			}
			idx = v
		}
		if idx <= 0 || idx > c.groups {
			return &CompileError{Offset: t.Offset, Msg: fmt.Sprintf("backreference to unknown group %d", idx)}
		}
		c.emit(Inst{Op: OpBackref, Arg: idx, Flags: fl})
	case *GroupNode:
		nf := fl
		if t.Mask.I {
			nf.I = t.Set.I
		}
		if t.Mask.S {
			nf.S = t.Set.S
		}
		if t.Mask.M {
			nf.M = t.Set.M
		}
		if t.Capture {
			c.emit(Inst{Op: OpGroupStart, Arg: t.Index})
			if err := c.compileNode(t.Body, nf); err != nil {
				return err
			}
			c.emit(Inst{Op: OpGroupEnd, Arg: t.Index})
		} else {
			if err := c.compileNode(t.Body, nf); err != nil {
				return err
			}
		}
	case *ConcatNode:
		for _, item := range t.Items {
			if err := c.compileNode(item, fl); err != nil {
				return err
			}
		}
	case *AlternationNode:
		return c.compileAlternation(t, fl)
	case *QuantNode:
		return c.compileQuant(t, fl)
	case *LookaroundNode:
		return c.compileLookaround(t, fl)
	default:
		return &CompileError{Msg: fmt.Sprintf("unsupported node %T", n)}
	}
	return nil
}

func (c *compiler) compileAlternation(t *AlternationNode, fl Flags) error {
	var jmpEnds []int
	for i, alt := range t.Alternatives {
		var splitIdx int
		if i < len(t.Alternatives)-1 {
			splitIdx = c.emit(Inst{Op: OpSplit})
			c.insts[splitIdx].X = len(c.insts)
		}
		if err := c.compileNode(alt, fl); err != nil {
			return err
		}
		if i < len(t.Alternatives)-1 {
			jmpEnds = append(jmpEnds, c.emit(Inst{Op: OpJmp}))
			c.insts[splitIdx].Y = len(c.insts)
		}
	}
	end := len(c.insts)
	for _, j := range jmpEnds {
		c.insts[j].X = end
	}
	return nil
}

func (c *compiler) compileQuant(t *QuantNode, fl Flags) error {
	// Required copies.
	for i := 0; i < t.Min; i++ {
		if err := c.compileNode(t.Child, fl); err != nil {
			return err
		}
	}
	if t.Max == -1 {
		// Guarded loop so empty-body iterations terminate.
		c.quantID++
		qid := c.quantID
		l1 := c.emit(Inst{Op: OpSetMark, Arg: qid})
		split := c.emit(Inst{Op: OpSplit})
		return c.finishLoop(t, fl, qid, l1, split)
	}
	// Bounded optional copies.
	var splits []int
	for i := t.Min; i < t.Max; i++ {
		s := c.emit(Inst{Op: OpSplit})
		splits = append(splits, s)
		if t.Greedy {
			c.insts[s].X = len(c.insts)
		} else {
			c.insts[s].Y = len(c.insts)
		}
		if err := c.compileNode(t.Child, fl); err != nil {
			return err
		}
	}
	end := len(c.insts)
	for _, s := range splits {
		if t.Greedy {
			c.insts[s].Y = end
		} else {
			c.insts[s].X = end
		}
	}
	return nil
}

func (c *compiler) finishLoop(t *QuantNode, fl Flags, qid, l1, split int) error {
	if t.Greedy {
		c.insts[split].X = len(c.insts)
	} else {
		c.insts[split].Y = len(c.insts)
	}
	if err := c.compileNode(t.Child, fl); err != nil {
		return err
	}
	guard := c.emit(Inst{Op: OpGuard, Arg: qid})
	c.emit(Inst{Op: OpJmp, X: l1})
	end := len(c.insts)
	c.insts[guard].X = end
	if t.Greedy {
		c.insts[split].Y = end
	} else {
		c.insts[split].X = end
	}
	return nil
}

func (c *compiler) compileLookaround(t *LookaroundNode, fl Flags) error {
	length := 0
	if t.Behind {
		l, ok := FixedLen(t.Body)
		if !ok {
			return &CompileError{Offset: t.Offset, Msg: "lookbehind requires a fixed-length body"}
		}
		length = l
	}
	c.frameID++
	fid := c.frameID
	push := c.emit(Inst{Op: OpLaPush, Arg: fid, Neg: t.Negate, Behind: t.Behind, Aux: length})
	c.insts[push].X = len(c.insts)
	if err := c.compileNode(t.Body, fl); err != nil {
		return err
	}
	c.emit(Inst{Op: OpLaRet, Arg: fid, Neg: t.Negate, Behind: t.Behind})
	c.insts[push].Y = len(c.insts)
	return nil
}

// FixedLen reports whether the node always consumes exactly n runes.
func FixedLen(n Node) (int, bool) {
	switch t := n.(type) {
	case *CharNode, *DotNode, *ClassNode, *ShorthandNode, *PropNode:
		return 1, true
	case *StrNode:
		return len([]rune(t.Runes)), true
	case *AnchorNode:
		return 0, true
	case *BackrefNode:
		return 0, false
	case *GroupNode:
		return FixedLen(t.Body)
	case *ConcatNode:
		total := 0
		for _, item := range t.Items {
			l, ok := FixedLen(item)
			if !ok {
				return 0, false
			}
			total += l
		}
		return total, true
	case *AlternationNode:
		if len(t.Alternatives) == 0 {
			return 0, true
		}
		first, ok := FixedLen(t.Alternatives[0])
		if !ok {
			return 0, false
		}
		for _, alt := range t.Alternatives[1:] {
			l, ok := FixedLen(alt)
			if !ok || l != first {
				return 0, false
			}
		}
		return first, true
	case *QuantNode:
		if t.Min != t.Max {
			return 0, false
		}
		l, ok := FixedLen(t.Child)
		if !ok {
			return 0, false
		}
		return l * t.Min, true
	case *LookaroundNode:
		return 0, true
	}
	return 0, false
}
