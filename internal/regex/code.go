package regex

// Op is one instruction of the explicit backtracking execution model.
type Op struct {
	Kind OpKind
	A, B int
	C    int
	// Literal / class payloads.
	Lit     rune
	LitInv  bool
	Elems   []ClassElem
	Negate  bool
	Group   int    // capture group index
	Name    string // backreference name (resolved) or debug label
	Anchor  BoundaryKind
	DotAll  bool
	Look    int // program id for lookaround
	LookLen int // fixed atom length for lookbehind
	Behind  bool
	LookNeg bool
	Src     int // pattern byte offset for trace/breakpoints
}

type OpKind int

const (
	OpLit OpKind = iota
	OpClass
	OpDot
	OpGrapheme
	OpAny // matches one atom including invalid byte
	OpAnchor
	OpBackref

	OpCapture
	OpJmp
	OpCommit // discard the most recent alt frame
	OpSplitAlt
	OpSplitG  // greedy unbounded loop split: A=body, B=exit
	OpSplitGB // greedy bounded: C = extra iterations allowed
	OpSplitL  // lazy unbounded loop split: A=exit, B=body
	OpSplitLB // lazy bounded: C = extra iterations allowed
	OpLoopEnd // body target back-edge marker

	OpLaPos // execute sub-program id A (lookaround): Behind/Neg in payload
	OpMatch
)

// Program is a compiled pattern with its own sub-program table.
type Program struct {
	ID       int
	Code     []Op
	Subs     []*Program
	Groups   int
	Names    map[string]int
	Pattern  string
	Flags    Flags
	SrcName  string
}

// anchor at end of code helper
func (pr *Program) emit(o Op) int {
	pr.Code = append(pr.Code, o)
	return len(pr.Code) - 1
}
