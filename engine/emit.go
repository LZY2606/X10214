package engine

import "fmt"

func (c *compiler) compileNode(n *Node, flags string) {
	flags = mergeFlags(flags, n.Flags)
	switch n.Kind {
	case NEmpty:
		// nothing
	case NFlags:
		for _, ch := range n.Children {
			c.compileNode(ch, flags)
		}
	case NChar:
		if len(n.Class) == 1 && n.Class[0].Kind == "perl" {
			c.emit(Inst{Op: OpChar, Node: n.ID, Match: &CharSpec{IsClass: true, Terms: n.Class}})
			return
		}
		if len(n.Class) == 1 && n.Class[0].Kind == "cat" {
			c.emit(Inst{Op: OpChar, Node: n.ID, Match: &CharSpec{IsClass: true, Terms: n.Class}})
			return
		}
		c.emit(Inst{Op: OpChar, Node: n.ID, Match: &CharSpec{R: n.R}, Flags: flags})
	case NClass:
		c.emit(Inst{Op: OpChar, Node: n.ID, Match: &CharSpec{IsClass: true, Neg: n.ClassNeg, Terms: n.Class}})
	case NDot:
		c.emit(Inst{Op: OpDot, Node: n.ID, Flags: flags})
	case NAnchor:
		c.emit(Inst{Op: OpAnchor, Node: n.ID, Kind: n.Anchor, Flags: flags})
	case NBackref:
		c.emit(Inst{Op: OpBackref, Node: n.ID, Arg: n.RefIdx, Flags: flags})
	case NRef:
		idx := c.prog.Names[n.GroupNm]
		c.emit(Inst{Op: OpBackref, Node: n.ID, Arg: idx, Flags: flags})
	case NConcat:
		for _, ch := range n.Children {
			c.compileNode(ch, flags)
		}
	case NAlt:
		end := c.label("alt_end")
		for i, ch := range n.Children {
			if i < len(n.Children)-1 {
				primary := c.label("alt_fall")
				next := c.label("alt_branch")
				c.emit(Inst{Op: OpSplit, Node: n.ID, LArg: primary, LArg2: next})
				c.compileNode(ch, flags)
				c.emit(Inst{Op: OpJmp, Node: n.ID, LArg: end})
				c.mark(next)
			} else {
				c.compileNode(ch, flags)
			}
		}
		c.mark(end)
		_ = end
	case NGroup:
		g := n.GroupIdx
		c.emit(Inst{Op: OpSave, Node: n.ID, Arg: 2 * g})
		c.compileNode(n.Children[0], flags)
		c.emit(Inst{Op: OpSave, Node: n.ID, Arg: 2*g + 1})
	case NRepeat:
		c.compileRepeat(n, flags)
	case NLook:
		c.compileLook(n, flags)
	default:
		panic(fmt.Sprintf("unhandled node %s", n.Kind))
	}
}

func (c *compiler) compileRepeat(n *Node, flags string) {
	c.loops++
	id := c.loops
	body := n.Children[0]
	c.emit(Inst{Op: OpRReset, Node: n.ID, Arg: id})
	entry := c.label("rep_entry")
	c.mark(entry)
	bodyL := c.label("rep_body")
	out := c.label("rep_out")
	ls := &LoopSpec{ID: id, Min: n.Min, Max: n.Max, Greedy: n.Greedy}
	c.emit(Inst{Op: OpRPhase, Node: n.ID, Loop: ls, LArg: bodyL, LArg2: out})
	c.mark(bodyL)
	c.compileNode(body, flags)
	c.emit(Inst{Op: OpRAgain, Node: n.ID, Arg: id, LArg: entry, LArg2: out})
	c.mark(out)
}

func (c *compiler) compileLook(n *Node, flags string) {
	if n.Ahead {
		body := c.label("la_body")
		end := c.label("la_end")
		if n.Positive {
			// continuation first in code (pushed later at runtime);
			// LAOpen then fall into body.
			c.emit(Inst{Op: OpLAOpen, Node: n.ID, Arg: boolToInt(n.Positive), LArg: end, LArg2: body})
			c.mark(body)
			c.compileNode(n.Children[0], flags)
			c.emit(Inst{Op: OpLAClose, Node: n.ID, LArg: end})
			c.mark(end)
		} else {
			// negative: body success => fail; body exhaustion => succeed.
			c.emit(Inst{Op: OpLAOpen, Node: n.ID, Arg: boolToInt(n.Positive), LArg: end, LArg2: body})
			c.mark(body)
			c.compileNode(n.Children[0], flags)
			c.emit(Inst{Op: OpLABound, Node: n.ID, LArg: end})
			c.mark(end)
		}
		return
	}
	c.emit(Inst{Op: OpLBOpen, Node: n.ID, Arg: boolToInt(n.Positive)})
	c.compileNode(n.Children[0], flags)
	c.emit(Inst{Op: OpLBClose, Node: n.ID})
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
