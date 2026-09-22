package vm

import (
	"fmt"
	"strings"
)

// Status is the outcome (or provisional outcome) of a run.
type Status string

const (
	StatusMatch         Status = "match"
	StatusNoMatch       Status = "no-match"
	StatusIndeterminate Status = "indeterminate"
	StatusPaused        Status = "paused"
)

// Run is the full serialisable execution state.
type Run struct {
	Prog        *Program `json:"prog"`
	Input       *Input   `json:"input"`
	Budgets     Budgets  `json:"budgets"`
	Status      Status   `json:"status"`
	NextStart   int      `json:"nextStart"` // leftmost-search frontier
	Thread      *Thread  `json:"thread"`
	Events      []Event  `json:"events"`
	eventCap    int
	seq         int
	steps       int
	states      int
	snaps       int
	StepsUsed   int            `json:"stepsUsed"`
	StatesUsed  int            `json:"statesUsed"`
	SnapsUsed   int            `json:"snapsUsed"`
	Frontier    *Frontier      `json:"frontier,omitempty"`
	Match       *Match         `json:"match,omitempty"`
	GroupNames  map[int]string `json:"groupNames,omitempty"`
	Breakpoints map[int]bool   `json:"breakpoints,omitempty"`
	pauseAtBP   bool
	stoppedAtBP bool
}

// Frontier describes where an indeterminate / paused run can resume.
type Frontier struct {
	Reason    string  `json:"reason"`
	NextStart int     `json:"nextStart"`
	Pc        int     `json:"pc"`
	Pos       int     `json:"pos"`
	Steps     int     `json:"steps"`
	States    int     `json:"states"`
	Snapshots int     `json:"snapshots"`
	Limits    Budgets `json:"limits"`
}

// Match is one successful result with rune and byte offsets.
type Match struct {
	RuneLo int       `json:"runeLo"`
	RuneHi int       `json:"runeHi"`
	ByteLo int       `json:"byteLo"`
	ByteHi int       `json:"byteHi"`
	Text   string    `json:"text"`
	Caps   []CapSlot `json:"caps"`
}

const defaultEventCap = 4000

// NewRun prepares a run at the first candidate start position.
func NewRun(prog *Program, in *Input, b Budgets) *Run {
	if b.MaxSteps <= 0 {
		b = DefaultBudgets()
	}
	r := &Run{
		Prog:        prog,
		Input:       in,
		Budgets:     b,
		Status:      "running",
		NextStart:   0,
		Events:      nil,
		eventCap:    defaultEventCap,
		GroupNames:  prog.GroupNames,
		Breakpoints: map[int]bool{},
	}
	r.startThread(0)
	return r
}

func (r *Run) startThread(start int) {
	r.Thread = &Thread{
		Pc:   0,
		Pos:  start,
		Caps: newCaptures(r.Prog.GroupCount),
	}
	r.Thread.Caps[0] = CapSlot{Begin: start, End: -1}
	r.log(Event{Kind: EvSearch, Pc: 0, Pos: start, Detail: fmt.Sprintf("try start at rune %d", start)})
}

// SetBreakpoint toggles a breakpoint on an AST node id (halts before execute).
func (r *Run) SetBreakpoint(nid int, on bool) {
	if r.Breakpoints == nil {
		r.Breakpoints = map[int]bool{}
	}
	r.Breakpoints[nid] = on
}

// Step executes a bounded number of micro-steps. n<=1 means exactly one.
func (r *Run) Step(n int) Status {
	if n <= 0 {
		n = 1
	}
	for i := 0; i < n; i++ {
		if r.finished() {
			return r.Status
		}
		if !r.tick() {
			return r.Status
		}
	}
	if r.finished() {
		return r.Status
	}
	if r.Status == "running" {
		r.Status = StatusPaused
	}
	return r.Status
}

// RunToEnd runs until match / no-match / budget exhaustion.
func (r *Run) RunToEnd() Status {
	for !r.finished() {
		if !r.tick() {
			break
		}
	}
	return r.Status
}

func (r *Run) finished() bool {
	return r.Status == StatusMatch || r.Status == StatusNoMatch ||
		r.Status == StatusIndeterminate || r.Status == StatusPaused
}

func (r *Run) log(e Event) {
	if len(r.Events) >= r.eventCap {
		return
	}
	r.seq++
	e.Seq = r.seq
	if r.Thread != nil {
		e.Stack = len(r.Thread.Stack)
	}
	r.Events = append(r.Events, e)
}

func (r *Run) snapshotCaps(c Captures) (Captures, bool) {
	r.snaps++
	if r.snaps > r.Budgets.MaxSnapshots {
		return nil, false
	}
	return c.Clone(), true
}

func (r *Run) capDiff(old, cur Captures, nid int) []CapChange {
	var out []CapChange
	for i := range cur {
		var o CapSlot
		if i < len(old) {
			o = old[i]
		} else {
			o = unsetSlot()
		}
		if o != cur[i] {
			ch := CapChange{Index: i, OldBegin: o.Begin, OldEnd: o.End, NewBegin: cur[i].Begin, NewEnd: cur[i].End}
			if name := r.Prog.GroupNames[i]; name != "" {
				ch.Name = name
			}
			out = append(out, ch)
		}
	}
	_ = nid
	return out
}

func (r *Run) capName(idx int) string {
	if name := r.Prog.GroupNames[idx]; name != "" {
		return name
	}
	return fmt.Sprintf("%d", idx)
}

// failThread rewinds the current thread; returns false if the whole start dies.
func (r *Run) failThread(reason string) bool {
	t := r.Thread
	for len(t.Stack) > 0 {
		f := t.pop()
		switch f.Kind {
		case FrameChoice:
			caps, ok := r.snapshotCaps(f.Caps)
			if !ok {
				r.indeterminate("snapshot", reason)
				return false
			}
			t.Caps = caps
			t.Pc = f.Pc
			t.Pos = f.Pos
			r.log(Event{Kind: EvBacktrack, Pc: f.Pc, NID: r.nidAt(f.Pc), Pos: f.Pos,
				Detail: fmt.Sprintf("%s -> resume pc %d pos %d", reason, f.Pc, f.Pos)})
			return true
		case FrameLook:
			alive := r.failLook(f, reason)
			return alive
		case FrameRepeat:
			// mutable counter frames are unwound with the enclosing choice.
			continue
		}
	}
	// No continuation at this start; advance the leftmost search.
	r.log(Event{Kind: EvFail, Pos: t.Pos, Detail: reason + " at this start"})
	r.NextStart++
	if r.NextStart > r.Input.lenRunes() {
		r.Status = StatusNoMatch
		return false
	}
	r.startThread(r.NextStart)
	return true
}

// nidAt returns the source node id for a pc, when present.
func (r *Run) nidAt(pc int) int {
	if pc >= 0 && pc < len(r.Prog.Code) {
		return r.Prog.Code[pc].NID
	}
	return 0
}

func (r *Run) indeterminate(kind, detail string) {
	r.Status = StatusIndeterminate
	t := r.Thread
	f := Frontier{
		Reason:    fmt.Sprintf("%s budget exhausted (%s)", kind, detail),
		NextStart: r.NextStart,
		Steps:     r.steps,
		States:    r.states,
		Snapshots: r.snaps,
		Limits:    r.Budgets,
	}
	if t != nil {
		f.Pc, f.Pos = t.Pc, t.Pos
	}
	r.Frontier = &f
	r.StepsUsed, r.StatesUsed, r.SnapsUsed = r.steps, r.states, r.snaps
	r.log(Event{Kind: EvIndeterminate, Pos: f.Pos, Detail: f.Reason})
}

func (r *Run) stopReason(bp bool) bool {
	r.pauseAtBP = bp
	r.Status = StatusPaused
	if t := r.Thread; t != nil {
		r.Frontier = &Frontier{Reason: "breakpoint", NextStart: r.NextStart,
			Pc: t.Pc, Pos: t.Pos, Steps: r.steps, States: r.states, Snapshots: r.snaps, Limits: r.Budgets}
	}
	return false
}

var _ = strings.TrimSpace
