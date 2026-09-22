package rx

import (
	"encoding/json"
)

// FrameKind enumerates choice-stack entries.
type FrameKind int

const (
	FAlt FrameKind = iota // alternation split alternative
	FQChoice              // quantifier stop / another-body choice
	FQGuard               // zero-width barrier
	FLook                 // lookaround continuation
)

// Frame is one choice-stack frame, fully JSON serializable.
type Frame struct {
	Kind      FrameKind `json:"k"`
	PC        int       `json:"pc"`
	Pos       int       `json:"pos"`
	CapDepth  int       `json:"cd"`
	QID       int       `json:"q"`
	Count     int       `json:"c"`
	StartPos  int       `json:"sp"`
	LastPos   int       `json:"lp"`
	GuardMark int       `json:"gm"`
	LookPC    int       `json:"lpc"`
	LookPos   int       `json:"lpos"`
	CandStart int       `json:"cs"`
	CandStep  int       `json:"cst"`
	CandCount int       `json:"cn"`
	NegLook   bool      `json:"ng"`
	Behind    bool      `json:"bh"`
	InnerFull bool      `json:"ifull,omitempty"`
	NodeID    int       `json:"nid"`
}

// Cap is a captured group span in code-point positions. -1 = open/unset.
type Cap struct {
	Start int `json:"s"`
	End   int `json:"e"`
	Set   bool `json:"set"`
}

// Undo records one capture write for rollback.
type Undo struct {
	Group int   `json:"g"`
	Start int   `json:"s"`
	End   int   `json:"e"`
	Set   bool  `json:"set"`
}

// MatchMode selects full-match vs leftmost search semantics.
type MatchMode int

const (
	ModeSearch MatchMode = iota
	ModeFull
)

// Budget bounds VM resource consumption.
type Budget struct {
	MaxSteps    int `json:"max_steps"`
	MaxStates   int `json:"max_states"`
	MaxSnapshots int `json:"max_snapshots"`
}

// Status of a run.
type Status string

const (
	StatusMatch       Status = "match"
	StatusNoMatch     Status = "no-match"
	StatusIndeterminate Status = "indeterminate"
	StatusPausedBreak Status = "paused-breakpoint"
)

// Event kinds streamed into the trail.
type EventKind string

const (
	EvStep      EventKind = "step"
	EvTry       EventKind = "try"
	EvConsume   EventKind = "consume"
	EvBranch    EventKind = "branch"
	EvPrune     EventKind = "prune"
	EvSave      EventKind = "capture"
	EvRollback  EventKind = "rollback"
	EvQuant     EventKind = "quant"
	EvLookEnter EventKind = "look-enter"
	EvLookExit  EventKind = "look-exit"
	EvAnchor    EventKind = "anchor"
	EvFail      EventKind = "fail"
	EvSearch    EventKind = "search-start"
	EvBudget    EventKind = "budget"
)

// Event is one trail record.
type Event struct {
	Seq      int       `json:"seq"`
	Kind     EventKind `json:"kind"`
	PC       int       `json:"pc,omitempty"`
	NodeID   int       `json:"id,omitempty"`
	Pos      int       `json:"pos,omitempty"`
	BytePos  int       `json:"byte,omitempty"`
	Rune     string    `json:"rune,omitempty"`
	Detail   string    `json:"detail,omitempty"`
	Group    int       `json:"group,omitempty"`
	Name     string    `json:"name,omitempty"`
	Start    int       `json:"start,omitempty"`
	End      int       `json:"end,omitempty"`
	Text     string    `json:"text,omitempty"`
	Budget   string    `json:"budget,omitempty"`
}

// Config configures a machine.
type Config struct {
	Mode       MatchMode `json:"mode"`
	Breakpoints []int    `json:"breakpoints"` // AST node IDs
	MaxBehind  int       `json:"max_behind"`
	TrailLimit int       `json:"trail_limit"`
}

// Machine is the complete resumable state of one execution.
type Machine struct {
	PC          int      `json:"pc"`
	Pos         int      `json:"pos"`
	StartPos    int      `json:"pos0"`
	Frames      []Frame  `json:"frames"`
	Caps        []Cap    `json:"caps"`
	Undo        []Undo   `json:"undo"`
	Status      Status   `json:"status"`
	Steps       int      `json:"steps"`
	States      int      `json:"states"`
	Snapshots   int      `json:"snapshots"`
	Events      []Event  `json:"events"`
	Dropped     int      `json:"dropped"`
	Budget      Budget   `json:"budget"`
	Cfg         Config   `json:"cfg"`
	Input       string   `json:"input"`
	Pattern     string   `json:"pattern"`
	MatchStart  int      `json:"mstart"`
	MatchEnd    int      `json:"mend"`
	HasMatch    bool     `json:"has_match"`
	Frontier    string   `json:"frontier,omitempty"`
	StopReason  string   `json:"stop_reason,omitempty"`
	SkipBreakPC int      `json:"skip_break_pc,omitempty"`
	TrailLimit  int      `json:"trail_limit"`
	Grammar     string   `json:"grammar"`
	Unicode     string   `json:"unicode"`
}

// State is the exported snapshot view of a machine.
type State struct {
	PC        int     `json:"pc"`
	Pos       int     `json:"pos"`
	BytePos   int     `json:"byte_pos"`
	Frames    []Frame `json:"frames"`
	Caps      []Cap   `json:"caps"`
	Status    Status  `json:"status"`
	Steps     int     `json:"steps"`
	StepsLeft int     `json:"steps_left"`
}

func defaultBudget() Budget {
	return Budget{MaxSteps: 200000, MaxStates: 100000, MaxSnapshots: 200000}
}

func defaultConfig() Config {
	return Config{Mode: ModeSearch, MaxBehind: 256, TrailLimit: 4000}
}

// NewMachine prepares a run.
func NewMachine(prog *Program, input string, cfg Config, bud Budget) *Machine {
	if cfg.MaxBehind == 0 {
		cfg.MaxBehind = 256
	}
	if cfg.TrailLimit == 0 {
		cfg.TrailLimit = 4000
	}
	if bud == (Budget{}) {
		bud = defaultBudget()
	}
	m := &Machine{
		Input:      input,
		Pattern:    prog.Pat.Source,
		Frames:     nil,
		Caps:       make([]Cap, len(prog.Groups)),
		Status:     "",
		Budget:     bud,
		Cfg:        cfg,
		TrailLimit: cfg.TrailLimit,
		Grammar:    SyntaxVersion,
		Unicode:    UnicodeVersion,
	}
	for i := range m.Caps {
		m.Caps[i] = Cap{Start: -1, End: -1}
	}
	if cfg.Mode == ModeSearch {
		m.PC, m.Pos, m.StartPos = 0, 0, 0
	} else {
		m.PC, m.Pos, m.StartPos = 0, 0, 0
	}
	return m
}

// Marshal serializes the machine for frontier persistence.
func (m *Machine) Marshal() ([]byte, error) {
	m.Frontier = ""
	return json.Marshal(m)
}

// UnmarshalMachine restores a paused machine.
func UnmarshalMachine(b []byte) (*Machine, error) {
	var m Machine
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}
