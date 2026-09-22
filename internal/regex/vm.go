package regex

import "strings"

// Opcode enumerates the machine instructions.
type Opcode string

const (
	OpChar       Opcode = "char"      // consume one code point matching Class
	OpAny        Opcode = "any"       // consume one code point (dot, flags aware)
	OpMatch      Opcode = "match"     // current thread succeeds
	OpJmp        Opcode = "jmp"       // unconditional jump
	OpSplit      Opcode = "split"     // fork: A=preferred continuation
	OpSave       Opcode = "save"      // write slot (snapshot through fork copy)
	OpAnchor     Opcode = "anchor"    // zero-width assertion
	OpBackref    Opcode = "backref"   // match text captured in slot pair
	OpLookAhead  Opcode = "lookahead" // enter lookahead subroutine
	OpLookBehind Opcode = "lookbehind"
	OpLookEnd    Opcode = "lookend"    // leave lookahead / resume outer
	OpGuardEntry Opcode = "guardentry" // start measuring an iteration
	OpGuardCheck Opcode = "guardcheck" // stop repeating if no progress
	OpStartTry   Opcode = "starttry"   // beginning of a search attempt at ip+1
)

type Instr struct {
	Op        Opcode
	Class     classMatcher
	Rune      rune
	A, B      int // split/jump targets, save slot or guard id
	Anchor    string
	Ref       int
	Behind    bool
	Negative  bool
	DotAll    bool
	Width     int  // lookbehind fixed byte width
	Multiline bool // anchor context
	CI        bool // case-insensitive backref/char context
	NodeID    int  // back-reference to AST node for breakpoints
}

// Program is the compiled, immutable execution artifact.
type Program struct {
	Instr        []Instr
	NumGroups    int
	NumGuards    int
	MaxExpansion int
	Pattern      string
	Optimized    bool
	AST          *Node
}

type compiler struct {
	out       []Instr
	numGroups int
	guards    int
	flags     Flags
}

// Compile lowers an AST into a Program.
func Compile(ast *Node, pattern string) (*Program, error) {
	c := &compiler{flags: Flags{}}
	// Emit a leading StartTry; search re-attempts jump to its next ip.
	c.emit(Instr{Op: OpStartTry, NodeID: ast.ID})
	groupCount := countGroups(ast)
	c.numGroups = groupCount
	if err := c.compileNode(ast, Flags{}); err != nil {
		return nil, err
	}
	c.emit(Instr{Op: OpMatch})
	return &Program{
		Instr:        c.out,
		NumGroups:    groupCount,
		NumGuards:    c.guards,
		MaxExpansion: maxBoundedExpansion(ast),
		Pattern:      pattern,
		AST:          ast,
	}, nil
}

func (c *compiler) emit(i Instr) int {
	c.out = append(c.out, i)
	return len(c.out) - 1
}

func (c *compiler) patch(ip, target int) { c.out[ip].A = target }

func (c *compiler) compileNode(n *Node, flags Flags) error {
	_ = c.flags
	c.flags = flags
	switch n.Kind {
	case NEmpty:
		return nil
	case NChar:
		cls := classMatcher{ranges: []RuneRange{{Lo: n.Value, Hi: n.Value}}, ci: flags.I || n.CI}
		c.emit(Instr{Op: OpChar, Class: cls, Rune: n.Value, CI: flags.I || n.CI, NodeID: n.ID})
	case NDot:
		c.emit(Instr{Op: OpAny, DotAll: flags.S, NodeID: n.ID})
	case NAnchor:
		c.emit(Instr{Op: OpAnchor, Anchor: n.Anchor, Multiline: flags.M, NodeID: n.ID})
	case NClass:
		for _, rr := range n.Ranges {
			if len(rr.Kind) > 8 && rr.Kind[:8] == "unicode:" && !validProperty(rr.Kind[8:]) {
				return &CompileError{Msg: "unknown unicode property: " + rr.Kind[8:]}
			}
		}
		cls := compileClass(n)
		cls.ci = cls.ci || flags.I
		c.emit(Instr{Op: OpChar, Class: cls, NodeID: n.ID})
	case NBackref:
		c.emit(Instr{Op: OpBackref, Ref: n.RefIndex, CI: flags.I, NodeID: n.ID})
		// no extra state needed
	case NGroup:
		s1, s2 := (n.Capture-1)*2, (n.Capture-1)*2+1
		c.emit(Instr{Op: OpSave, A: s1, NodeID: n.ID})
		if err := c.compileNode(n.Body, flags); err != nil {
			return err
		}
		c.emit(Instr{Op: OpSave, A: s2, NodeID: n.ID})
	case NLook:
		if err := c.compileLook(n, flags); err != nil {
			return err
		}
	case NConcat:
		f := flags
		for _, ch := range n.Children {
			if ch.Kind == NFlagScope {
				f = applyFlags(f, ch.SetFlags, ch.ClearFlags)
				if err := c.compileNode(ch.Body, f); err != nil {
					return err
				}
				// Scoped (?:...) groups reset flags after the body; a bare
				// (?i) marker (empty body inside a concat) leaks flags to the
				// following siblings, matching PCRE global-modifier semantics.
				if scopedFlagBody(ch) {
					f = flags
				}
				continue
			}
			if err := c.compileNode(ch, f); err != nil {
				return err
			}
		}
	case NAlt:
		var endJumps []int
		for i, br := range n.Children {
			if i == len(n.Children)-1 {
				if err := c.compileNode(br, flags); err != nil {
					return err
				}
				break
			}
			split := c.emit(Instr{Op: OpSplit, NodeID: n.ID})
			aStart := len(c.out)
			if err := c.compileNode(br, flags); err != nil {
				return err
			}
			endJumps = append(endJumps, c.emit(Instr{Op: OpJmp}))
			bStart := len(c.out)
			c.out[split].A, c.out[split].B = aStart, bStart
		}
		end := len(c.out)
		for _, j := range endJumps {
			c.patch(j, end)
		}
	case NFlagScope:
		nf := flags
		for _, r := range n.SetFlags {
			nf, _ = nf.with(r, true)
		}
		for _, r := range n.ClearFlags {
			nf, _ = nf.with(r, false)
		}
		// SetFlags/ClearFlags are stored on the node; verify they match the
		// parser-computed nf by rebuilding here.
		return c.compileNode(n.Body, nf)
	case NRepeat:
		return c.compileRepeat(n, flags)
	}
	return nil
}

func (c *compiler) compileLook(n *Node, flags Flags) error {
	w := 0
	if n.Behind {
		var err error
		w, err = fixedWidth(n.Body)
		if err != nil {
			return err
		}
	}
	if n.Negative {
		// split: A = evaluate body first (must FAIL for the assertion),
		// B = continuation (reached via backtracking only when body dies).
		split := c.emit(Instr{Op: OpSplit, Negative: true, Behind: n.Behind, Width: w, NodeID: n.ID})
		bodyStart := len(c.out)
		if err := c.compileNode(n.Body, flags); err != nil {
			return err
		}
		c.emit(Instr{Op: OpLookEnd, Negative: true, Behind: n.Behind, NodeID: n.ID})
		cont := len(c.out)
		c.out[split].A, c.out[split].B = bodyStart, cont
		return nil
	}
	op := OpLookAhead
	if n.Behind {
		op = OpLookBehind
	}
	look := c.emit(Instr{Op: op, Behind: n.Behind, Width: w, NodeID: n.ID})
	if err := c.compileNode(n.Body, flags); err != nil {
		return err
	}
	end := c.emit(Instr{Op: OpLookEnd, Negative: false, Behind: n.Behind, NodeID: n.ID})
	c.out[look].B = end
	return nil
}

func (c *compiler) compileRepeat(n *Node, flags Flags) error {
	if err := validateRepeatBody(n.Body); err != nil {
		return err
	}
	// Fixed small / bounded counts: unroll.
	if n.Max != -1 {
		if n.Max > maxUnroll {
			return &CompileError{Msg: "bounded quantifier exceeds unroll limit 4096"}
		}
		for i := 0; i < n.Min; i++ {
			if err := c.compileNode(n.Body, flags); err != nil {
				return err
			}
		}
		for i := n.Min; i < n.Max; i++ {
			split := c.emit(Instr{Op: OpSplit, NodeID: n.ID})
			bodyStart := len(c.out)
			if err := c.compileNode(n.Body, flags); err != nil {
				return err
			}
			if n.Lazy {
				c.out[split].A, c.out[split].B = len(c.out), bodyStart
			} else {
				c.out[split].A, c.out[split].B = bodyStart, len(c.out)
			}
		}
		return nil
	}

	// Unbounded: guarded loop, never unrolled, so nested zero-width repeats
	// cannot explode. Layout (the guard entry is the jump target so every
	// iteration re-arms it; mandatory copies run straight through):
	//   body x min
	// loop:
	//   [guardentry G]
	//   split body / exit
	//   body
	//   guardcheck G
	//   jmp loop
	guard := c.guards
	c.guards++
	for i := 0; i < n.Min; i++ {
		if err := c.compileNode(n.Body, flags); err != nil {
			return err
		}
	}
	loop := len(c.out)
	c.emit(Instr{Op: OpGuardEntry, A: guard, NodeID: n.ID})
	split := c.emit(Instr{Op: OpSplit, NodeID: n.ID})
	bodyStart := len(c.out)
	if err := c.compileNode(n.Body, flags); err != nil {
		return err
	}
	c.emit(Instr{Op: OpGuardCheck, A: guard, NodeID: n.ID})
	jmp := c.emit(Instr{Op: OpJmp})
	exit := len(c.out)
	if n.Lazy {
		c.out[split].A, c.out[split].B = exit, bodyStart
	} else {
		c.out[split].A, c.out[split].B = bodyStart, exit
	}
	c.out[jmp].A = loop
	return nil
}

// CompileError marks a lowering failure.
type CompileError struct{ Msg string }

func (e *CompileError) Error() string { return "compile error: " + e.Msg }

const maxUnroll = 4096

func countGroups(n *Node) int {
	max := 0
	walk(n, func(x *Node) {
		if x.Kind == NGroup && x.Capture > max {
			max = x.Capture
		}
	})
	return max
}

func walk(n *Node, f func(*Node)) {
	if n == nil {
		return
	}
	f(n)
	for _, ch := range n.Children {
		walk(ch, f)
	}
	walk(n.Body, f)
}

func maxBoundedExpansion(n *Node) int {
	m := 1
	walk(n, func(x *Node) {
		if x.Kind == NRepeat && x.Max > 0 {
			m += x.Max
		}
	})
	return m
}

func validateRepeatBody(b *Node) error {
	empty := true
	walk(b, func(x *Node) {
		switch x.Kind {
		case NEmpty, NGroup:
			// group with empty body still consumes nothing
		default:
			empty = false
		}
	})
	if empty {
		// Quantifying a purely zero-width atom is legal but pointless; the
		// guard machinery handles it deterministically (single iteration).
		return nil
	}
	return nil
}

// fixedWidth returns the consumed byte width of n, or an error if variable.
// fixedWidth validates a lookbehind body and returns its width measured in
// code points. Each char/class/dot consumes exactly one code point regardless
// of its UTF-8 byte width; the VM rewinds in rune units and records both the
// byte and rune positions. Backreferences and unbounded repeats are rejected.
func fixedWidth(n *Node) (int, error) {
	switch n.Kind {
	case NEmpty, NAnchor, NLook:
		return 0, nil
	case NChar, NClass, NDot:
		return 1, nil
	case NBackref:
		return 0, &CompileError{Msg: "lookbehind requires a fixed-width pattern"}
	case NGroup, NFlagScope:
		return fixedWidth(n.Body)
	case NConcat:
		total := 0
		for _, ch := range n.Children {
			w, err := fixedWidth(ch)
			if err != nil {
				return 0, err
			}
			total += w
		}
		return total, nil
	case NAlt:
		w0 := -1
		for _, ch := range n.Children {
			w, err := fixedWidth(ch)
			if err != nil {
				return 0, err
			}
			if w0 < 0 {
				w0 = w
			} else if w != w0 {
				return 0, &CompileError{Msg: "lookbehind requires a fixed-width pattern"}
			}
		}
		if w0 < 0 {
			w0 = 0
		}
		return w0, nil
	case NRepeat:
		w, err := fixedWidth(n.Body)
		if err != nil {
			return 0, err
		}
		if n.Max == -1 {
			return 0, &CompileError{Msg: "lookbehind requires a fixed-width pattern"}
		}
		return w * n.Max, nil
	}
	return 0, &CompileError{Msg: "lookbehind requires a fixed-width pattern"}
}

// disassemble renders the program for debugging/UI.
func (p *Program) disassemble() string {
	var b strings.Builder
	for i, in := range p.Instr {
		b.WriteString(itoa(i))
		b.WriteString("\t")
		b.WriteString(string(in.Op))
		switch in.Op {
		case OpSplit, OpJmp:
			b.WriteString(" -> ")
			b.WriteString(itoa(in.A))
			if in.Op == OpSplit {
				b.WriteString("/")
				b.WriteString(itoa(in.B))
			}
		case OpSave:
			b.WriteString(" slot=")
			b.WriteString(itoa(in.A))
		case OpGuardEntry, OpGuardCheck:
			b.WriteString(" g")
			b.WriteString(itoa(in.A))
		case OpAnchor:
			b.WriteByte(' ')
			b.WriteString(in.Anchor)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func applyFlags(f Flags, set, clear string) Flags {
	for _, r := range set {
		f, _ = f.with(r, true)
	}
	for _, r := range clear {
		f, _ = f.with(r, false)
	}
	return f
}

// scopedFlagBody reports whether the flag scope node is the (?:flags:body)
// form (flags only apply inside) versus a bare (?i) global marker.
func scopedFlagBody(n *Node) bool {
	// The parser produces NFlagScope with an explicit body only for the
	// colon form; a bare marker wraps an NEmpty that is the only child.
	return n.Body != nil && n.Body.Kind != NEmpty
}
