package engine

// Mode selects whole-input matching vs unanchored search.
type Mode string

const (
	ModeFull   Mode = "full"
	ModeSearch Mode = "search"
)

// Budget bounds execution. Zero means unlimited for that dimension.
type Budget struct {
	MaxSteps     int `json:"maxSteps"`
	MaxStates    int `json:"maxStates"`
	MaxSnapshots int `json:"maxSnapshots"`
}

// Status is the terminal/interim status of a run.
type Status string

const (
	StatusMatch         Status = "match"
	StatusNoMatch       Status = "nomatch"
	StatusIndeterminate Status = "indeterminate"
	StatusHalted        Status = "halted" // breakpoints / single steps
)

// Event is one observable trace event in the capture trace room.
type Event struct {
	Seq       int      `json:"seq"`
	Type      string   `json:"type"`
	PC        int      `json:"pc"`
	Node      int      `json:"node"`
	RunePos   int      `json:"runePos"`
	BytePos   int      `json:"bytePos"`
	Text      string   `json:"text,omitempty"`
	Group     int      `json:"group,omitempty"`
	GroupName string   `json:"groupName,omitempty"`
	Start     int      `json:"start,omitempty"`
	End       int      `json:"end,omitempty"`
	Old       *Capture `json:"old,omitempty"`
	New       *Capture `json:"new,omitempty"`
	ThreadID  int      `json:"threadId,omitempty"`
	Alt       int      `json:"alt,omitempty"`
	Reason    string   `json:"reason,omitempty"`
	FromPC    int      `json:"fromPc,omitempty"`
	FromPos   int      `json:"fromPos,omitempty"`
}

// Result is the outcome of (part of) a run.
type Result struct {
	Status         Status             `json:"status"`
	SyntaxVersion  string             `json:"syntaxVersion"`
	UnicodeVersion string             `json:"unicodeVersion"`
	Pattern        string             `json:"pattern"`
	Mode           Mode               `json:"mode"`
	Match          bool               `json:"match"`
	Captures       []Capture          `json:"captures"`
	NamedCaptures  map[string]Capture `json:"namedCaptures"`
	Events         []Event            `json:"events"`
	Steps          int                `json:"steps"`
	StatesSeen     int                `json:"statesSeen"`
	Snapshots      int                `json:"snapshots"`
	Frontier       *Frontier          `json:"frontier,omitempty"`
	Budget         Budget             `json:"budget"`
	Reason         string             `json:"reason,omitempty"`
	SearchStart    int                `json:"searchStart,omitempty"`
}

// Engine executes one compiled program against one input.
type Engine struct {
	prog *Program
	in   *Input
	mode Mode
	bud  Budget

	threads  []*Thread
	events   []Event
	seq      int
	steps    int
	states   int
	snaps    int
	active   int // index (into threads) of currently running thread
	tid      int
	threadID map[*Thread]int

	// search bookkeeping
	searchStart int
	emptyLast   bool

	halt       bool
	haltReason string
}

// NewEngine builds an engine.
func NewEngine(prog *Program, in *Input, mode Mode, bud Budget) *Engine {
	return &Engine{
		prog: prog, in: in, mode: mode, bud: bud,
		threadID: map[*Thread]int{},
		active:   -1,
	}
}

func (e *Engine) initialThread() *Thread {
	t := &Thread{
		PC:      0,
		Pos:     0,
		Caps:    rootCaps(e.prog.NumGroups),
		LBStart: -1,
		Loops:   nil,
		Looks:   nil,
	}
	if e.mode == ModeSearch {
		// program begins at search start 0; advance loop handled in driver
	}
	return t
}

func (e *Engine) emit(ev Event) {
	ev.Seq = e.seq
	e.seq++
	ev.BytePos = e.in.BytePos(ev.RunePos)
	e.events = append(e.events, ev)
}
