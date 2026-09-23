package regex

// Budget bounds resource consumption. Exceeding any limit yields an
// indeterminate result carrying a resumable frontier, never a no-match.
type Budget struct {
	MaxSteps     int64
	MaxStates    int64
	MaxSnapshots int64
}

// DefaultBudget is used when a caller supplies a zero Budget.
func DefaultBudget() Budget {
	return Budget{MaxSteps: 1_000_000, MaxStates: 200_000, MaxSnapshots: 200_000}
}

// Capture is one group interval in both byte and code point coordinates.
type Capture struct {
	Index   int
	Name    string
	Start   Loc
	End     Loc
	Defined bool
}

// FrameKind tags backtracking frames.
type FrameKind int

const (
	FrameAlt FrameKind = iota
	FrameLoopG
	FrameLoopL
)

// Frame is a resumable backtracking record composed solely of value types,
// so the whole executor state can be serialized into a frontier.
type Frame struct {
	Kind    FrameKind
	Resume  int    // PC to resume at when backtracking into this frame
	Snap    []Capture
	Pos     int    // input index captured at the split
	SplitPC int
	Entry   int    // most recent successful body entry position
	Remain  int    // additional iterations still permitted (-1 = unbounded)
	Depth   int    // lookaround nesting depth for display
}

// EventKind enumerates trace events.
type EventKind string

const (
	EvTry       EventKind = "try"
	EvConsume   EventKind = "consume"
	EvBranch    EventKind = "branch"
	EvCaptureUp EventKind = "capture_update"
	EvSplit     EventKind = "split"
	EvBacktrack EventKind = "backtrack"
	EvPrune     EventKind = "prune"
	EvEnter     EventKind = "enter_look"
	EvExit      EventKind = "exit_look"
	EvMatch     EventKind = "match"
	EvFail      EventKind = "fail"
	EvBudget    EventKind = "budget_exceeded"
	EvAnchor    EventKind = "anchor"
	EvStart     EventKind = "start_attempt"
)

// Event is one recorded trajectory entry.
type Event struct {
	Seq     int          `json:"seq"`
	Kind    EventKind    `json:"kind"`
	PC      int          `json:"pc"`
	Src     int          `json:"src"`
	Pos     Loc          `json:"pos"`
	Depth   int          `json:"depth"`
	Detail  string       `json:"detail,omitempty"`
	Group   int          `json:"group,omitempty"`
	Capture *CaptureView `json:"capture,omitempty"`
	Reason  string       `json:"reason,omitempty"`
}

// CaptureView is the JSON-safe rendering of one group.
type CaptureView struct {
	Index     int    `json:"index"`
	Name      string `json:"name,omitempty"`
	StartByte int    `json:"start_byte"`
	EndByte   int    `json:"end_byte"`
	StartCP   int    `json:"start_cp"`
	EndCP     int    `json:"end_cp"`
	Defined   bool   `json:"defined"`
}

func viewCap(c Capture) CaptureView {
	return CaptureView{
		Index: c.Index, Name: c.Name,
		StartByte: c.Start.Byte, EndByte: c.End.Byte,
		StartCP: c.Start.CP, EndCP: c.End.CP, Defined: c.Defined,
	}
}

// Status is the overall outcome.
type Status string

const (
	StatusRunning       Status = "running"
	StatusMatched       Status = "matched"
	StatusNoMatch       Status = "no_match"
	StatusIndeterminate Status = "indeterminate"
)

// State is the fully resumable executor state.
type State struct {
	Prog        *Program
	Input       *Input
	PC          int
	Pos         int
	Caps        []Capture
	Stack       []Frame
	Steps       int64
	States      int64
	Snapshots   int64
	Status      Status
	Events      []Event
	Seq         int
	Depth       int
	Anchored    bool
	StartSearch int
	Budget      Budget
	MaxEvents   int
	StopReason  string
	Result      *MatchResult
	Breakpoints map[int]bool
	BreakPC     int
}

// MatchResult describes a successful search match.
type MatchResult struct {
	Matched   bool          `json:"matched"`
	StartByte int           `json:"start_byte"`
	EndByte   int           `json:"end_byte"`
	StartCP   int           `json:"start_cp"`
	EndCP     int           `json:"end_cp"`
	Captures  []CaptureView `json:"captures"`
	Versions  VersionInfo   `json:"versions"`
}

// Options configures a run.
type Options struct {
	Anchored    bool
	Budget      Budget
	MaxEvents   int
	Breakpoints map[int]bool
}

// NewState initializes a run. Anchored=false performs a leftmost search.
func NewState(pr *Program, in *Input, opts Options) *State {
	b := opts.Budget
	if b.MaxSteps == 0 && b.MaxStates == 0 && b.MaxSnapshots == 0 {
		b = DefaultBudget()
	}
	me := opts.MaxEvents
	if me == 0 {
		me = 200000
	}
	st := &State{
		Prog: pr, Input: in, Caps: make([]Capture, pr.Groups+1),
		Status: StatusRunning, Budget: b, Anchored: opts.Anchored,
		MaxEvents: me, Breakpoints: opts.Breakpoints,
	}
	for i := range st.Caps {
		st.Caps[i] = Capture{Index: i, Name: groupName(pr, i)}
	}
	st.record(Event{Kind: EvStart, Pos: st.loc(), Detail: "begin execution"})
	return st
}

func (s *State) loc() Loc { return s.Input.locAt(s.Pos) }

func (s *State) record(e Event) {
	e.Seq = s.Seq
	s.Seq++
	if e.Pos == (Loc{}) {
		e.Pos = s.loc()
	}
	if len(s.Events) < s.MaxEvents {
		s.Events = append(s.Events, e)
	}
}

func (s *State) snapshot() []Capture {
	s.Snapshots++
	cp := make([]Capture, len(s.Caps))
	copy(cp, s.Caps)
	return cp
}

func restoreInto(dst *[]Capture, snap []Capture) {
	out := make([]Capture, len(snap))
	copy(out, snap)
	*dst = out
}

func (s *State) push(f Frame) {
	s.Stack = append(s.Stack, f)
	s.States++
}

func (s *State) overBudget() string {
	if s.Steps >= s.Budget.MaxSteps {
		return "step budget exceeded"
	}
	if s.States >= s.Budget.MaxStates {
		return "state budget exceeded"
	}
	if s.Snapshots >= s.Budget.MaxSnapshots {
		return "snapshot budget exceeded"
	}
	return ""
}
