package vm

// Op identifies one VM instruction.
type Op uint8

const (
	OpChar        Op = iota // Rune, Fold
	OpClass                 // Class
	OpAny                   // DotAll
	OpAnchor                // A: anchor kind
	OpJmp                   // Pc
	OpSplit                 // Pc(preferred); alt=Pc+1; A=loopID; Greedy
	OpGroupBegin            // A=group index
	OpGroupEnd              // A=group index
	OpBackref               // A=group index
	OpRepeatBegin           // A=loopID, B=min, C=max, Greedy
	OpRepeatPost            // A=loopID (body completed one iteration)
	OpRepeatAfter           // A=loopID (min check + cleanup)
	OpLookPush              // Neg, Behind, B=maxLen, A=lookID; body at Pc+1
	OpLookEnd               // A=lookID
	OpMatch
)

// Anchor kinds for OpAnchor.
const (
	AnchorBOL = iota
	AnchorEOL
	AnchorWB
	AnchorNWB
)

// Instr is one compiled instruction.
type Instr struct {
	Op        Op         `json:"op"`
	NID       int        `json:"nid"`
	Pc        int        `json:"pc,omitempty"`
	Target    int        `json:"target,omitempty"`
	Rune      rune       `json:"rune,omitempty"`
	Class     *ClassPred `json:"class,omitempty"`
	A         int        `json:"a,omitempty"`
	B         int        `json:"b,omitempty"`
	C         int        `json:"c,omitempty"`
	Greedy    bool       `json:"greedy,omitempty"`
	Neg       bool       `json:"neg,omitempty"`
	Behind    bool       `json:"behind,omitempty"`
	Fold      bool       `json:"fold,omitempty"`
	DotAll    bool       `json:"dotall,omitempty"`
	Multiline bool       `json:"multiline,omitempty"`
}

func (i Instr) OpName() string {
	switch i.Op {
	case OpChar:
		return "Char"
	case OpClass:
		return "Class"
	case OpAny:
		return "Any"
	case OpAnchor:
		return "Anchor"
	case OpJmp:
		return "Jmp"
	case OpSplit:
		return "Split"
	case OpGroupBegin:
		return "GroupBegin"
	case OpGroupEnd:
		return "GroupEnd"
	case OpBackref:
		return "Backref"
	case OpRepeatBegin:
		return "RepeatBegin"
	case OpRepeatPost:
		return "RepeatPost"
	case OpRepeatAfter:
		return "RepeatAfter"
	case OpLookPush:
		return "LookPush"
	case OpLookEnd:
		return "LookEnd"
	case OpMatch:
		return "Match"
	}
	return "?"
}

// Program is the compiled, serialisable bytecode program.
type Program struct {
	Code       []Instr        `json:"code"`
	GroupCount int            `json:"groupCount"`
	GroupNames map[int]string `json:"groupNames,omitempty"`
	MaxLookLen []int          `json:"lookMaxLen"` // indexed by lookID
}
