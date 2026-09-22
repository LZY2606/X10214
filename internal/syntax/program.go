package syntax

import "fmt"

// Op is a compiled instruction opcode.
type Op int

const (
	OpChar      Op = iota // match a specific rune (or raw invalid byte)
	OpStr                 // match a fused run of literal runes
	OpDot                 // any rune (newline excluded unless dotall)
	OpClass               // bracketed character class
	OpShorthand           // \d \D \w \W \s \S
	OpProp                // \p{...} \P{...}
	OpAnchor              // ^ $ \b \B
	OpBackref             // backreference by group index
	OpGroupStart          // open capture group
	OpGroupEnd            // close capture group
	OpSplit               // fork: try X then Y
	OpJmp                 // unconditional jump
	OpSetMark             // record position for quantifier guard
	OpGuard               // exit loop if no progress since mark
	OpLaPush              // enter lookaround
	OpLaRet               // leave lookaround body
	OpMatch               // accept
)

// Inst is one compiled instruction. Targets X/Y are program counters.
type Inst struct {
	Op    Op
	X, Y  int
	Rune  rune
	Raw   byte   // invalid-UTF-8 singleton byte when Rune==0xFFFD
	Str   string // fused literal for OpStr, property name for OpProp
	Flags Flags  // per-instruction flag snapshot
	Arg   int    // group index, quantifier id, class index, anchor kind
	Neg   bool   // negated lookaround / negated property
	Behind bool  // lookbehind
	Aux   int    // lookbehind fixed length
}

// Program is a compiled pattern.
type Program struct {
	Insts     []Inst
	Start     int
	NumGroups int
	Names     map[string]int
	Classes   []ClassNode
	Source    string
}

func (p *Program) String() string {
	out := ""
	for i, in := range p.Insts {
		out += fmt.Sprintf("%4d %s\n", i, instString(in))
	}
	return out
}

func instString(in Inst) string {
	switch in.Op {
	case OpChar:
		return fmt.Sprintf("char %q", in.Rune)
	case OpStr:
		return fmt.Sprintf("str %q", in.Str)
	case OpDot:
		return "dot"
	case OpClass:
		return fmt.Sprintf("class #%d", in.Arg)
	case OpShorthand:
		return fmt.Sprintf("short \\%c", in.Arg)
	case OpProp:
		return fmt.Sprintf("prop %q neg=%v", in.Str, in.Neg)
	case OpAnchor:
		return fmt.Sprintf("anchor %d", in.Arg)
	case OpBackref:
		return fmt.Sprintf("backref %d", in.Arg)
	case OpGroupStart:
		return fmt.Sprintf("gstart %d", in.Arg)
	case OpGroupEnd:
		return fmt.Sprintf("gend %d", in.Arg)
	case OpSplit:
		return fmt.Sprintf("split %d, %d", in.X, in.Y)
	case OpJmp:
		return fmt.Sprintf("jmp %d", in.X)
	case OpSetMark:
		return fmt.Sprintf("setmark q%d", in.Arg)
	case OpGuard:
		return fmt.Sprintf("guard q%d -> %d", in.Arg, in.X)
	case OpLaPush:
		return fmt.Sprintf("lapush f%d neg=%v behind=%v len=%d -> %d", in.Arg, in.Neg, in.Behind, in.Aux, in.X)
	case OpLaRet:
		return fmt.Sprintf("laret f%d neg=%v", in.Arg, in.Neg)
	case OpMatch:
		return "match"
	}
	return "?"
}
