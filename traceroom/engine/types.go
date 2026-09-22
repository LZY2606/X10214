package engine

import "traceroom/compile"

// Status of a run.
type Status string

const (
	StatusMatch         Status = "match"
	StatusNoMatch       Status = "no-match"
	StatusIndeterminate Status = "indeterminate" // a budget was exhausted
	StatusPaused        Status = "paused"        // hit a breakpoint / step limit
)

// LimitReason explains which budget stopped the run.
type LimitReason string

const (
	LimitSteps     LimitReason = "steps"
	LimitStates    LimitReason = "states"
	LimitSnapshots LimitReason = "snapshots"
)

// Config holds resource budgets and stepping controls.
type Config struct {
	MaxSteps     int `json:"maxSteps"`
	MaxStates    int `json:"maxStates"`
	MaxSnapshots int `json:"maxSnapshots"`
	StepLimit    int `json:"stepLimit"` // stop after this many micro-steps (UI stepping)
	// Breakpoints are pattern-source column-1 based offsets to pause at when
	// an instruction whose [Lo,Hi) contains it executes on the active thread.
	Breakpoints []int `json:"breakpoints,omitempty"`
	// StartFrom/StartTo constrain leftmost-start scanning (byte offsets).
	StartFrom int `json:"startFrom,omitempty"`
	StartTo   int `json:"startTo,omitempty"`
	// Anchored disables leftmost-start scanning (^ or \A semantics at 0).
	Anchored bool `json:"anchored,omitempty"`
	// FullMatch wraps the pattern in implicit \A...\z.
	FullMatch bool `json:"fullMatch,omitempty"`
}

// Capture is one resolved group result.
type Capture struct {
	Index        int    `json:"index"`
	Name         string `json:"name,omitempty"`
	Start        int    `json:"start"` // -1 when unmatched
	End          int    `json:"end"`
	Text         string `json:"text,omitempty"`
	Participated bool   `json:"participated"`
}

// FrameKind enumerates DFS alternative-stack frame types.
type FrameKind uint8

const (
	FrameAlt        FrameKind = iota // a pending alternative (backtrack point)
	FrameCutMarker                   // marks an atomic region boundary
	FrameCutSuccess                  // placed so surviving path performs cut on arrival
	FrameBehind
	FrameBehindCont
	FrameDie
)

// Frame is one DFS stack entry of a machine.
type Frame struct {
	Kind    FrameKind `json:"k"`
	PC      int       `json:"pc"`
	Pos     int       `json:"pos"`
	LoopID  int       `json:"lid,omitempty"`
	LookID  int       `json:"look,omitempty"`
	Caps    []int     `json:"caps,omitempty"`
	SubPC   int       `json:"subpc,omitempty"`
	SubPos  int       `json:"subpos,omitempty"`
	SubProg int       `json:"subprog,omitempty"`
}

// LoopState tracks zero-width progress per loop id. Anchor is the byte
// position at the current loop head; LoopGuard fails an iteration whose body
// made no progress.
type LoopState struct {
	Anchor int `json:"a"`
}

// Machine is one serializable execution thread (one start position attempt).
type Machine struct {
	PC     int         `json:"pc"`
	Pos    int         `json:"pos"`
	Caps   []int       `json:"caps"`
	Loops  []LoopState `json:"loops"`
	Stack  []Frame     `json:"stack"`
	Active *LookFrame  `json:"active,omitempty"`
}

// LookFrame is an in-progress lookaround evaluation on the active thread.
// Outer* fields snapshot the enclosing machine context so nested lookaround
// restores the correct program, pc, stack, caps and loops.
type LookFrame struct {
	SubID      int         `json:"subId"`
	PC         int         `json:"pc"`
	Pos        int         `json:"pos"`
	Parent     *LookFrame  `json:"parent,omitempty"`
	OuterSubID int         `json:"outerSubId"` // -1 when outer is main program
	OuterPC    int         `json:"outerPc"`
	OuterPos   int         `json:"outerPos"`
	OuterStack []Frame     `json:"outerStack"`
	Caps       []int       `json:"caps"`
	Loops      []LoopState `json:"loops"`
	Stack      []Frame     `json:"stack"`
	Behind     bool        `json:"behind"`
	Negative   bool        `json:"neg"`
	StartPos   int         `json:"startPos"`
}

// EventKind enumerates trace events.
type EventKind string

const (
	EvStep         EventKind = "step"
	EvConsume      EventKind = "consume"
	EvBranchPush   EventKind = "branch-push"
	EvBranchTake   EventKind = "branch-take"
	EvBranchPruned EventKind = "branch-pruned"
	EvBacktrack    EventKind = "backtrack"
	EvCapUpdate    EventKind = "cap-update"
	EvCapRollback  EventKind = "cap-rollback"
	EvLookEnter    EventKind = "look-enter"
	EvLookExit     EventKind = "look-exit"
	EvLoopZero     EventKind = "loop-zero-width"
	EvStartAttempt EventKind = "start-attempt"
	EvAnchoredFail EventKind = "anchored-fail"
	EvPause        EventKind = "pause"
	EvDone         EventKind = "done"
)

// Event is one UI-visible trace record.
type Event struct {
	Seq         int            `json:"seq"`
	Kind        EventKind      `json:"kind"`
	Depth       int            `json:"depth"`
	Pos         Pos            `json:"pos"`
	NextPos     *Pos           `json:"nextPos,omitempty"`
	PC          int            `json:"pc,omitempty"`
	SubID       int            `json:"subId,omitempty"`
	Detail      map[string]any `json:"detail,omitempty"`
	Caps        []Capture      `json:"caps,omitempty"`
	PruneReason string         `json:"pruneReason,omitempty"`
}

// Program provider for resume: the engine resolves subprograms through it.
type ProgramProvider interface {
	Main() *compile.Program
	Sub(id int) *compile.Program
}
