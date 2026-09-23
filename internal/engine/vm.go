package engine

// Cap captures one group slot [lo,hi); (-1,-1) means not participated.
type Cap [2]int

// EventKind enumerates recorded trace events.
type EventKind string

const (
	EvConsume   EventKind = "consume"
	EvSplit     EventKind = "split"
	EvPrune     EventKind = "prune"
	EvCapSet    EventKind = "cap-set"
	EvCapRoll   EventKind = "cap-rollback"
	EvAnchor    EventKind = "anchor"
	EvBackref   EventKind = "backref"
	EvEnterLook EventKind = "look-enter"
	EvExitLook  EventKind = "look-exit"
	EvBacktrack EventKind = "backtrack"
	EvSearch    EventKind = "search-start"
	EvMatch     EventKind = "match"
	EvFail      EventKind = "fail"
	EvPause     EventKind = "pause"
)

// Event is one trace record shown in the capture-trace room.
type Event struct {
	Seq       int       `json:"seq"`
	Kind      EventKind `json:"kind"`
	Pc        int       `json:"pc,omitempty"`
	Pos       int       `json:"pos,omitempty"`
	BytePos   int       `json:"bytePos,omitempty"`
	CPPos     int       `json:"cpPos,omitempty"`
	Group     int       `json:"group,omitempty"`
	NewLo     int       `json:"newLo,omitempty"`
	NewHi     int       `json:"newHi,omitempty"`
	OldLo     int       `json:"oldLo,omitempty"`
	OldHi     int       `json:"oldHi,omitempty"`
	QuantID   int       `json:"quantId,omitempty"`
	Chosen    string    `json:"chosen,omitempty"`
	LookID    int       `json:"lookId,omitempty"`
	Neg       bool      `json:"neg,omitempty"`
	Ahead     bool      `json:"ahead,omitempty"`
	LookStart int       `json:"lookStart,omitempty"`
	ByteStart int       `json:"byteStart,omitempty"`
	CPS       int       `json:"cpStart,omitempty"`
	Rune      string    `json:"rune,omitempty"`
	Note      string    `json:"note,omitempty"`
	Caps      []CapView `json:"caps,omitempty"`
}

// CapView is a point-in-time snapshot of one capture group.
type CapView struct {
	Group   int    `json:"group"`
	Name    string `json:"name,omitempty"`
	Lo      int    `json:"lo"`
	Hi      int    `json:"hi"`
	ByteLo  int    `json:"byteLo"`
	ByteHi  int    `json:"byteHi"`
	CPLo    int    `json:"cpLo"`
	CPHi    int    `json:"cphi"`
	Text    string `json:"text,omitempty"`
	Matched bool   `json:"matched"`
}

// Budget bounds resource use for one execution. Zero means unlimited.
type Budget struct {
	MaxSteps     int `json:"maxSteps"`
	MaxStates    int `json:"maxStates"`
	MaxSnapshots int `json:"maxSnapshots"`
}

func (b Budget) stepLimited(v int) bool  { return b.MaxSteps > 0 && v >= b.MaxSteps }
func (b Budget) stateLimited(v int) bool { return b.MaxStates > 0 && v >= b.MaxStates }
func (b Budget) snapLimited(v int) bool  { return b.MaxSnapshots > 0 && v >= b.MaxSnapshots }

// alt is one suspended alternative continuation.
type alt struct {
	pc, pos  int
	cap      []int
	pathLen  int
	isMarker bool
	marker   *markerState
}

// markerState holds the suspended lookaround bookkeeping.
type markerState struct {
	lookID        int
	body, cont    int
	anchorPos     int
	neg, ahead    bool
	maxW          int
	nextStart     int // next candidate start (behind); p for ahead
	attempts      int
	capBefore     []int
	pathLenBefore int
	emittedEnter  bool
}

// cursor is the active DFS execution of one thread.
type cursor struct {
	prog *Program
	in   *Input
	pc   int
	pos  int
	cap  []int
	path [][2]int
	alts []alt

	steps, states, snaps int
	budget               Budget
	ev                   []Event
	seq                  int
	status               Status
	matched              bool
	matchStart           int
	matchEnd             int
	matchCaps            []Cap
	breakpoints          map[int]map[int]bool // pc -> set of pos (-1=any)
	oneStep              bool
	pausedReason         string
}

// Status is the terminal/paused state of an execution.
type Status string

const (
	StatusMatch      Status = "match"
	StatusNoMatch    Status = "no-match"
	StatusIndet      Status = "indeterminate"
	StatusPausedStep Status = "paused-step"
	StatusPausedBP   Status = "paused-breakpoint"
	StatusPausedBudg Status = "paused-budget"
)

func newCursor(prog *Program, in *Input, pc, pos int, cap []int) *cursor {
	if cap == nil {
		cap = make([]int, 2*(prog.NumGroups+1))
		for i := range cap {
			cap[i] = -1
		}
		cap[0], cap[1] = pos, pos
	}
	return &cursor{prog: prog, in: in, pc: pc, pos: pos, cap: cap,
		status: StatusNoMatch}
}

func (c *cursor) emit(k EventKind) *Event {
	e := Event{Seq: c.seq, Kind: k, Pc: c.pc, Pos: c.pos,
		BytePos: c.in.BytePos(c.pos), CPPos: c.in.CPPos(c.pos)}
	c.seq++
	c.ev = append(c.ev, e)
	return &c.ev[len(c.ev)-1]
}

func (c *cursor) emitAt(k EventKind, pc, pos int) *Event {
	e := Event{Seq: c.seq, Kind: k, Pc: pc, Pos: pos,
		BytePos: c.in.BytePos(pos), CPPos: c.in.CPPos(pos)}
	c.seq++
	c.ev = append(c.ev, e)
	return &c.ev[len(c.ev)-1]
}

func cloneInts(src []int) []int {
	dst := make([]int, len(src))
	copy(dst, src)
	return dst
}

func (c *cursor) capViews() []CapView {
	return buildCapViews(c.cap, c.prog, c.in)
}

func buildCapViews(arr []int, prog *Program, in *Input) []CapView {
	views := make([]CapView, prog.NumGroups+1)
	for i := 0; i <= prog.NumGroups; i++ {
		lo, hi := arr[2*i], arr[2*i+1]
		v := CapView{Group: i, Lo: lo, Hi: hi}
		if i > 0 && i <= len(prog.Groups) {
			v.Name = prog.Groups[i-1].Name
		}
		if lo >= 0 && hi >= 0 {
			v.Matched = true
			v.ByteLo = in.BytePos(lo)
			v.ByteHi = byteHi(in, hi)
			v.CPLo = in.CPPos(lo)
			v.CPHi = cpHi(in, hi)
			v.Text = string(in.raw[v.ByteLo:v.ByteHi])
		}
		views[i] = v
	}
	return views
}

func byteHi(in *Input, pos int) int {
	if pos >= len(in.Syms) {
		if len(in.Syms) == 0 {
			return 0
		}
		return in.Syms[len(in.Syms)-1].ByteHi
	}
	return in.Syms[pos].ByteLo
}

func cpHi(in *Input, pos int) int {
	if pos >= len(in.Syms) {
		return in.CPPos(pos)
	}
	return in.Syms[pos].CPLo
}
