package regex

// Op identifies one VM instruction.
type Op uint16

const (
	OpChar  Op = iota // one element via Matcher (arg)
	OpSplit           // choice: A/B PCs; PreferA chooses semantics
	OpJmp
	OpLoopBegin // arg=loopID; zero-width guard
	OpLoopEnd   // arg=loopID
	OpCapStart  // arg=group
	OpCapEnd    // arg=group
	OpCut       // atomic: discard frames <= arg frame marker (set at compile time as cut id; runtime maps to frame)
	OpLAStart   // positive lookahead: snapshot caps+trackers
	OpNLAStart  // negative lookahead
	OpLAEnd
	OpLBStart  // positive lookbehind: snapshot; candidate scan
	OpNLBStart // negative lookbehind
	OpLBEnd
	OpAnchor
	OpBackref
	OpAdvance // search wrapper: one more start position
	OpMatch
	OpFail
)

// Instr is a compiled instruction.
type Instr struct {
	Op      Op
	A, B    int         // generic operands (pcs, group id, loop id, anchor kind)
	PreferA bool        // split ordering
	Match   matcher     // OpChar
	Flags   Flags       // anchor/backref semantics
	Trace   TraceMarker // source mapping for the trace view
}

// TraceMarker records which AST construct produced this instruction.
type TraceMarker struct {
	Label string
	Kind  string
}

// Program is a compiled, executable pattern.
type Program struct {
	Meta    *ProgramMeta
	Code    []Instr
	Start   int
	Full    bool // true = fullmatch wrapper, false = unanchored search
	AST     Node
	loopSeq int
}

// Compile parses and compiles. full=true emits a fullmatch wrapper;
// otherwise the VM searches start positions left to right.
func Compile(pattern, flagStr string, full bool) (*Program, error) {
	meta, ast, err := Parse(pattern, flagStr)
	if err != nil {
		return nil, err
	}
	fl, err := parseFlagString(flagStr, true)
	if err != nil {
		return nil, err
	}
	c := &compiler{prog: &Program{Meta: meta, AST: ast, Full: full}, flags: fl}
	bodyStart := 0
	if full {
		c.emit(Instr{Op: OpCapStart, A: 0, Trace: TraceMarker{Label: "$0", Kind: "capture-start"}})
		bodyStart = len(c.prog.Code)
		c.compileNode(ast, fl)
		c.emit(Instr{Op: OpAnchor, A: int(AnchorEndNL), Flags: fl, Trace: TraceMarker{Label: `\Z`, Kind: "anchor"}})
		c.emit(Instr{Op: OpCapEnd, A: 0, Trace: TraceMarker{Label: "$0", Kind: "capture-end"}})
		c.emit(Instr{Op: OpMatch})
	} else {
		// Search wrapper:
		//   split -> body : advance
		//   body: capstart0 <pattern> capend0 match
		//   advance -> split
		split := c.emit(Instr{Op: OpSplit, PreferA: true, Trace: TraceMarker{Label: "search-start", Kind: "split"}})
		bodyPC := len(c.prog.Code)
		c.patch(split, bodyPC, 0)
		c.emit(Instr{Op: OpCapStart, A: 0, Trace: TraceMarker{Label: "$0", Kind: "capture-start"}})
		bodyStart = len(c.prog.Code)
		c.compileNode(ast, fl)
		c.emit(Instr{Op: OpCapEnd, A: 0, Trace: TraceMarker{Label: "$0", Kind: "capture-end"}})
		c.emit(Instr{Op: OpMatch})
		adv := c.emit(Instr{Op: OpAdvance, Trace: TraceMarker{Label: "advance", Kind: "advance"}})
		c.jmpTo(split, adv)
		c.prog.Code[split].B = adv
	}
	c.prog.Start = 0
	_ = bodyStart
	return c.prog, nil
}

type compiler struct {
	prog  *Program
	flags Flags
}

func (c *compiler) emit(in Instr) int {
	c.prog.Code = append(c.prog.Code, in)
	return len(c.prog.Code) - 1
}

func (c *compiler) patch(pc, a, b int) {
	c.prog.Code[pc].A = a
	c.prog.Code[pc].B = b
}

func (c *compiler) jmpTo(from, to int) { c.prog.Code[from].A = to }

func (c *compiler) compileNode(n Node, fl Flags) {
	switch t := n.(type) {
	case *Empty:
		// no instructions
	case *Concat:
		for _, it := range t.Items {
			c.compileNode(it, fl)
		}
	case *Alt:
		var splits, jumps []int
		for i, b := range t.Branches {
			if i < len(t.Branches)-1 {
				sp := c.emit(Instr{Op: OpSplit, PreferA: true, Trace: TraceMarker{Label: branchLabel(i), Kind: "alt-split"}})
				splits = append(splits, sp)
			}
			aPC := len(c.prog.Code)
			if i > 0 {
				// patch previous split's B to this branch
				c.prog.Code[splits[i-1]].B = aPC
			} else {
				c.prog.Code[splits[0]].A = aPC
			}
			c.compileNode(b, fl)
			if i < len(t.Branches)-1 {
				j := c.emit(Instr{Op: OpJmp, Trace: TraceMarker{Label: "alt-join", Kind: "jmp"}})
				jumps = append(jumps, j)
			}
		}
		join := len(c.prog.Code)
		for _, j := range jumps {
			c.prog.Code[j].A = join
		}
	case *Char:
		var m matcher = charMatcher{code: t.Code, fold: fl.I}
		if t.Dot {
			m = dotMatcher{newline: fl.S}
		}
		label := string(t.Code)
		kind := "char"
		if t.Dot {
			label, kind = ".", "dot"
		}
		c.emit(Instr{Op: OpChar, Match: m, Trace: TraceMarker{Label: label, Kind: kind}})
	case *Class:
		c.emit(Instr{Op: OpChar, Match: buildClassMatcher(t, fl), Trace: TraceMarker{Label: "class", Kind: "class"}})
	case *Anchor:
		labels := map[AnchorKind]string{
			AnchorSOL: "^", AnchorEOL: "$", AnchorStart: `\A`,
			AnchorEnd: `\z`, AnchorEndNL: `\Z`, AnchorWordB: `\b`, AnchorNotWordB: `\B`,
		}
		c.emit(Instr{Op: OpAnchor, A: int(t.Kind), Flags: fl, Trace: TraceMarker{Label: labels[t.Kind], Kind: "anchor"}})
	case *Backref:
		c.emit(Instr{Op: OpBackref, A: t.Ref, B: 0, Flags: fl, Trace: TraceMarker{Label: backrefLabel(t), Kind: "backref"}})
	case *Group:
		c.compileGroup(t, fl)
	case *Repeat:
		c.compileRepeat(t, fl)
	}
}

func branchLabel(i int) string {
	return "alt#" + itoa(i+1)
}

func backrefLabel(b *Backref) string {
	if b.Name != "" {
		return `\k<` + b.Name + ">"
	}
	return `\` + itoa(b.Ref)
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [24]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func (c *compiler) compileGroup(g *Group, fl Flags) {
	innerFlags := applyFlags(fl, g.AddFlags, g.DelFlags)
	switch g.Kind {
	case GroupCapture:
		c.emit(Instr{Op: OpCapStart, A: g.Index, Trace: TraceMarker{Label: capLabel(g), Kind: "capture-start"}})
		c.compileNode(g.Child, innerFlags)
		c.emit(Instr{Op: OpCapEnd, A: g.Index, Trace: TraceMarker{Label: capLabel(g), Kind: "capture-end"}})
	case GroupNonCap, GroupFlags:
		c.compileNode(g.Child, innerFlags)
	case GroupAtomic:
		// cutID is a unique small integer; the VM tags the frame below the
		// body when OpCut executes at entry, then pops everything up to and
		// including the tag at OpCut (second emission) on body success.
		id := c.prog.loopSeq
		c.prog.loopSeq++
		c.emit(Instr{Op: OpCut, A: cutOpen | (id << cutIDShift), Trace: TraceMarker{Label: "(?>", Kind: "cut-open"}})
		c.compileNode(g.Child, innerFlags)
		c.emit(Instr{Op: OpCut, A: cutClose | (id << cutIDShift), Trace: TraceMarker{Label: "?>)", Kind: "cut-close"}})
	case GroupLookAhead:
		c.emit(Instr{Op: OpLAStart, Trace: TraceMarker{Label: "(?=", Kind: "la-start"}})
		c.compileNode(g.Child, innerFlags)
		c.emit(Instr{Op: OpLAEnd, Trace: TraceMarker{Label: "?=)", Kind: "la-end"}})
	case GroupNegLookAhead:
		c.emit(Instr{Op: OpNLAStart, Trace: TraceMarker{Label: "(?!", Kind: "nla-start"}})
		c.compileNode(g.Child, innerFlags)
		c.emit(Instr{Op: OpLAEnd, Trace: TraceMarker{Label: "?!)", Kind: "nla-end"}})
	case GroupLookBehind:
		c.emit(Instr{Op: OpLBStart, Trace: TraceMarker{Label: "(?<=", Kind: "lb-start"}})
		c.compileNode(g.Child, innerFlags)
		c.emit(Instr{Op: OpLBEnd, Trace: TraceMarker{Label: "?<=)", Kind: "lb-end"}})
	case GroupNegLookBehind:
		c.emit(Instr{Op: OpNLBStart, Trace: TraceMarker{Label: "(?<!", Kind: "nlb-start"}})
		c.compileNode(g.Child, innerFlags)
		c.emit(Instr{Op: OpLBEnd, Trace: TraceMarker{Label: "?<!)", Kind: "nlb-end"}})
	}
}

const (
	cutOpen    = 1
	cutClose   = 2
	cutIDShift = 2
)

func capLabel(g *Group) string {
	if g.Name != "" {
		return "(?P<" + g.Name + ">"
	}
	return "(" + itoa(g.Index)
}

func (c *compiler) compileRepeat(r *Repeat, fl Flags) {
	if r.Max >= 0 {
		// Bounded: emit Min mandatory copies then (Max-Min) optional copies.
		for i := 0; i < r.Min; i++ {
			c.compileNode(r.Child, fl)
		}
		for i := r.Min; i < r.Max; i++ {
			if r.Possessive {
				id := c.prog.loopSeq
				c.prog.loopSeq++
				c.emit(Instr{Op: OpCut, A: cutOpen | (id << cutIDShift), Trace: TraceMarker{Label: "{cut}", Kind: "cut-open"}})
			}
			label := "quant"
			if !r.Greedy {
				label = "quant-lazy"
			}
			sp := c.emit(Instr{Op: OpSplit, PreferA: r.Greedy, Trace: TraceMarker{Label: label, Kind: "quant-split"}})
			body := len(c.prog.Code)
			c.prog.Code[sp].A = body
			c.compileNode(r.Child, fl)
			end := len(c.prog.Code)
			c.prog.Code[sp].B = end
			if r.Possessive {
				id := c.prog.loopSeq - 1
				c.emit(Instr{Op: OpCut, A: cutClose | (id << cutIDShift), Trace: TraceMarker{Label: "{/cut}", Kind: "cut-close"}})
			}
		}
		return
	}
	// Unbounded: unique loop id for zero-width guard.
	id := c.prog.loopSeq
	c.prog.loopSeq++
	begin := c.emit(Instr{Op: OpLoopBegin, A: id, Trace: TraceMarker{Label: "loop-begin", Kind: "loop-begin"}})
	if r.Possessive {
		c.emit(Instr{Op: OpCut, A: cutOpen | (c.prog.loopSeq << cutIDShift), Trace: TraceMarker{Label: "{cut}", Kind: "cut-open"}})
		c.prog.loopSeq++
	}
	sp := c.emit(Instr{Op: OpSplit, PreferA: r.Greedy, Trace: TraceMarker{Label: "quant", Kind: "quant-split"}})
	body := len(c.prog.Code)
	c.prog.Code[sp].A = body
	c.compileNode(r.Child, fl)
	if r.Possessive {
		c.emit(Instr{Op: OpCut, A: cutClose | ((c.prog.loopSeq - 1) << cutIDShift), Trace: TraceMarker{Label: "{/cut}", Kind: "cut-close"}})
	}
	end := c.emit(Instr{Op: OpLoopEnd, A: id, Trace: TraceMarker{Label: "loop-end", Kind: "loop-end"}})
	c.prog.Code[end].A = begin
	exit := len(c.prog.Code)
	c.prog.Code[sp].B = exit
	_ = begin
}
