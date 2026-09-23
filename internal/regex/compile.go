package regex

// Compile produces a Program from an AST. The host regexp library is not
// used at any point.
func Compile(n Node, info *GroupInfo) *Program {
	c := &compiler{prog: &Program{Groups: info.Count, Names: info.Names, Pattern: info.Pattern, Flags: info.Flags}}
	c.compile(n, c.flags(info.Flags))
	c.emitRaw(Op{Kind: OpMatch})
	return c.prog
}

type fstate struct {
	ci, dotAll, multiline bool
}

type compiler struct {
	prog *Program
	next int
	fs   fstate
}

func (c *compiler) flags(f Flags) fstate {
	return fstate{ci: f.CaseInsensitive, dotAll: f.DotAll, multiline: f.Multiline}
}

func (c *compiler) emitAt(n Node, o Op) int {
	o.Src = n.Pos()
	c.prog.Code = append(c.prog.Code, o)
	return len(c.prog.Code) - 1
}

func (c *compiler) emitRaw(o Op) int {
	c.prog.Code = append(c.prog.Code, o)
	return len(c.prog.Code) - 1
}

func (c *compiler) subProgram(n Node, fs fstate) int {
	sub := &compiler{prog: &Program{Groups: c.prog.Groups, Names: c.prog.Names, Pattern: c.prog.Pattern, Flags: Flags{CaseInsensitive: fs.ci, DotAll: fs.dotAll, Multiline: fs.multiline}}, fs: fs, next: c.next}
	sub.compile(n, fs)
	sub.emitRaw(Op{Kind: OpMatch})
	id := len(c.prog.Subs)
	c.prog.Subs = append(c.prog.Subs, sub.prog)
	c.next = sub.next
	return id
}

func (c *compiler) compile(n Node, fs fstate) {
	switch x := n.(type) {
	case *Concat:
		for _, it := range x.Items {
			c.compile(it, fs)
		}
	case *Alt:
		// split/commit chain preserving documented left-to-right priority:
		//   SplitAlt L1,Lnext ; <b1> ; Commit END ;
		//   Lnext: SplitAlt L2,Lnext2 ; <b2> ; Commit END ; ... ; <bN> ; END:
		var commits []int
		for i, b := range x.Branches {
			if i < len(x.Branches)-1 {
				c.emitAt(x, Op{Kind: OpSplitAlt, A: 0, B: len(c.prog.Code) + 1})
				c.prog.Code[len(c.prog.Code)-1].A = len(c.prog.Code) + 1
			}
			c.compile(b, fs)
			if i < len(x.Branches)-1 {
				cm := c.emitAt(x, Op{Kind: OpCommit, A: 0})
				commits = append(commits, cm)
			}
		}
		end := len(c.prog.Code)
		for _, cm := range commits {
			c.prog.Code[cm].A = end
		}
	case *Quant:
		c.compileQuant(x, fs)
	case *Group:
		gfs := fs
		if x.Flags.CaseInsensitive || x.Flags.DotAll || x.Flags.Multiline {
			gfs = c.flags(x.Flags)
		}
		if x.Capture {
			c.emitAt(x, Op{Kind: OpCapture, Group: x.Index, A: 0, Name: groupName(c.prog, x.Index)})
		}
		c.compile(x.Child, gfs)
		if x.Capture {
			c.emitAt(x, Op{Kind: OpCapture, Group: x.Index, A: 1, Name: groupName(c.prog, x.Index)})
		}
	case *Look:
		id := c.subProgram(x.Child, fs)
		ln, _ := RequireFixedLength(x.Child)
		c.emitAt(x, Op{Kind: OpLaPos, Look: id, Behind: x.Behind, LookNeg: x.Neg, LookLen: ln})
	case *AtomLit:
		c.emitAt(x, Op{Kind: OpLit, Lit: x.R, LitInv: x.Invalid})
	case *AtomDot:
		c.emitAt(x, Op{Kind: OpDot, DotAll: fs.dotAll})
	case *AtomGrapheme:
		c.emitAt(x, Op{Kind: OpGrapheme})
	case *AtomBoundary:
		c.emitAt(x, Op{Kind: OpAnchor, Anchor: x.Kind})
	case *AtomBackref:
		c.emitAt(x, Op{Kind: OpBackref, Group: x.Index, Name: x.Name})
	case *AtomClass:
		c.emitAt(x, Op{Kind: OpClass, Elems: append([]ClassElem(nil), x.Elems...), Negate: x.Negate})
	}
}

func groupName(pr *Program, idx int) string {
	for n, i := range pr.Names {
		if i == idx {
			return n
		}
	}
	return ""
}

func (c *compiler) compileQuant(q *Quant, fs fstate) {
	for i := 0; i < q.Min; i++ {
		c.compile(q.Child, fs)
	}
	extra := q.Max
	if q.Max != -1 {
		extra = q.Max - q.Min
	}
	if extra == 0 {
		return
	}
	if q.Lazy {
		sp := c.emitAt(q, Op{Kind: OpSplitL, A: 0, B: 0})
		body := len(c.prog.Code)
		c.compile(q.Child, fs)
		c.emitAt(q, Op{Kind: OpLoopEnd, A: sp})
		exit := len(c.prog.Code)
		c.prog.Code[sp].A = exit
		c.prog.Code[sp].B = body
		if extra != -1 {
			c.prog.Code[sp].Kind = OpSplitLB
			c.prog.Code[sp].C = extra
		}
		return
	}
	sp := c.emitAt(q, Op{Kind: OpSplitG, A: 0, B: 0})
	body := len(c.prog.Code)
	c.compile(q.Child, fs)
	c.emitAt(q, Op{Kind: OpLoopEnd, A: sp})
	exit := len(c.prog.Code)
	c.prog.Code[sp].A = body
	c.prog.Code[sp].B = exit
	if extra != -1 {
		c.prog.Code[sp].Kind = OpSplitGB
		c.prog.Code[sp].C = extra
	}
}
