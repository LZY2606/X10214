package engine

import (
	"unicode/utf8"

	"capture-tracer/internal/syntax"
)

// Status is the outcome of an engine (possibly partial) execution.
type Status string

const (
	StatusPaused       Status = "paused"       // breakpoint / single-step pause
	StatusMatched      Status = "matched"
	StatusNoMatch      Status = "no-match"
	StatusIndeterminate Status = "indeterminate" // a resource budget was reached
	StatusError        Status = "error"
)

// DefaultBudgets are applied when a budget field is zero.
const (
	DefaultStepBudget     = 200000
	DefaultStateBudget    = 50000
	DefaultSnapshotBudget = 200000
	MaxAssertDepth        = 64
)

// Budgets bound engine resources. Zero fields use the defaults.
type Budgets struct {
	Steps     int64 `json:"steps"`
	States    int64 `json:"states"`
	Snapshots int64 `json:"snapshots"`
}

func (b Budgets) withDefaults() Budgets {
	if b.Steps <= 0 {
		b.Steps = DefaultStepBudget
	}
	if b.States <= 0 {
		b.States = DefaultStateBudget
	}
	if b.Snapshots <= 0 {
		b.Snapshots = DefaultSnapshotBudget
	}
	return b
}

// Usage reports consumed budget.
type Usage struct {
	Steps     int64 `json:"steps"`
	States    int64 `json:"states"`
	Snapshots int64 `json:"snapshots"`
}

// CapRec is one capture slot write ("opening" or "closing" a group).
type CapRec struct {
	Slot  int `json:"slot"`
	Pos   int `json:"pos"`
	Step  int64 `json:"step"`
	State int64 `json:"state"`
}

type guardRec struct {
	ID  int
	Pos int
}

// Frame is one subprogram invocation (main or a lookaround).
type Frame struct {
	Sub      int        `json:"sub"`
	PC       int        `json:"pc"`
	ReturnPC int        `json:"returnPc"`
	Assert   bool       `json:"assert"`
	Neg      bool       `json:"neg"`
	Dir      int        `json:"dir"` // 0 ahead, 1 behind
	Width    int        `json:"width"`
	Expect   int        `json:"expect"`
	Depth    int        `json:"depth"`
	Caps     []CapRec   `json:"caps"`
	Guards   []guardRec `json:"guards"`
}

// Thread is one active continuation.
type Thread struct {
	ID     int64    `json:"id"`
	Pos    int      `json:"pos"`
	Frames []*Frame `json:"frames"`
}

// GroupCapture is a resolved capture group result.
type GroupCapture struct {
	Index int    `json:"index"`
	Name  string `json:"name,omitempty"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	Text  string `json:"text"`
}

// Frontier describes where a suspended run can continue.
type Frontier struct {
	NextStart    int          `json:"nextStart"`
	NextRune     int          `json:"nextRune"`
	Current      *ThreadView  `json:"current"`
	Alternatives []ThreadView `json:"alternatives"`
	Usage        Usage        `json:"usage"`
}

// ThreadView is a serialisable summary of a continuation.
type ThreadView struct {
	ID    int `json:"id"`
	Pos   int `json:"pos"`
	Rune  int `json:"rune"`
	Depth int `json:"depth"`
	Node  int `json:"node"`
	Sub   int `json:"sub"`
}

// Event is one trail-room record.
type Event struct {
	Seq    int64          `json:"seq"`
	Step   int64          `json:"step"`
	Type   string         `json:"type"`
	Pos    int            `json:"pos"`
	Rune   int            `json:"rune"`
	Node   int            `json:"node,omitempty"`
	State  int64          `json:"state"`
	Alt    int64          `json:"alt,omitempty"`
	Detail map[string]any `json:"detail,omitempty"`
	Caps   []GroupCapture `json:"caps,omitempty"`
}

// Result is the final or current result.
type Result struct {
	Status   Status         `json:"status"`
	Start    int            `json:"start,omitempty"`
	End      int            `json:"end,omitempty"`
	Text     string         `json:"text,omitempty"`
	Captures []GroupCapture `json:"captures,omitempty"`
	Reason   string         `json:"reason,omitempty"`
}

// State is the full serialisable execution state.
type State struct {
	Pattern     string      `json:"pattern"`
	Input       string      `json:"input"`
	Budgets     Budgets     `json:"budgets"`
	Breakpoints []int       `json:"breakpoints"`
	Full        bool        `json:"full"`
	Optimized   int         `json:"optimized"`
	Status      Status      `json:"status"`
	Result      *Result     `json:"result,omitempty"`
	Usage       Usage       `json:"usage"`
	NextStart   int         `json:"nextStart"`
	Current     *Thread     `json:"current"`
	Stack       []*Thread   `json:"stack"`
	Events      []Event     `json:"events"`
	seq         int64
	stateSeq    int64
	paused      bool

	prog *Program
	// rune index of every byte offset; -1 off-boundary.
	ord []int
	// byte offsets that start a rune.
	bounds []int
}

func (s *State) mainFrame() *Frame {
	return s.Current.Frames[0]
}

func (s *State) topFrame() *Frame {
	return s.Current.Frames[len(s.Current.Frames)-1]
}

// New creates a runnable state from a parsed pattern.
func New(p *syntax.Pattern, input string, b Budgets, full bool, optimized int) *State {
	prog := Compile(p, CompileFlags{Full: full, Optimized: optimized})
	st := &State{
		Pattern:   "",
		Input:     input,
		Budgets:   b.withDefaults(),
		Full:      full,
		Optimized: optimized,
		Status:    StatusPaused,
		prog:      prog,
		ord:       make([]int, len(input)+1),
	}
	st.buildOrds()
	return st
}

func (s *State) buildOrds() {
	for i := range s.ord {
		s.ord[i] = -1
	}
	s.ord[0] = 0
	s.bounds = append(s.bounds, 0)
	k := 0
	for i := 0; i < len(s.Input); {
		_, size := utf8.DecodeRuneInString(s.Input[i:])
		s.ord[i] = k
		s.bounds = append(s.bounds, i)
		i += size
		k++
	}
	s.ord[len(s.Input)] = k
	s.bounds = append(s.bounds, len(s.Input))
}

func (s *State) runeAt(pos int) (rune, int) {
	if pos >= len(s.Input) {
		return -1, 0
	}
	return utf8.DecodeRuneInString(s.Input[pos:])
}

// FrontierView snapshots the continuation frontier for an indeterminate run.
func (s *State) FrontierView() Frontier {
	f := Frontier{
		NextStart: s.NextStart,
		NextRune:  s.ordAt(s.NextStart),
		Usage:     s.Usage,
	}
	if s.Current != nil {
		v := s.threadView(s.Current)
		f.Current = &v
	}
	for _, t := range s.stack {
		f.Alternatives = append(f.Alternatives, s.threadView(t))
	}
	return f
}

func (s *State) ordAt(pos int) int {
	if pos < 0 || pos > len(s.Input) {
		return -1
	}
	return s.ord[pos]
}

func (s *State) threadView(t *Thread) ThreadView {
	top := t.Frames[len(t.Frames)-1]
	node := -1
	if top.PC < len(s.prog.Subs[top.Sub].Code) {
		node = s.prog.Subs[top.Sub].Code[top.PC].Node
	}
	return ThreadView{
		ID: t.ID, Pos: t.Pos, Rune: s.ordAt(t.Pos),
		Depth: len(t.Frames), Node: node, Sub: top.Sub,
	}
}

func (s *State) newThreadID() int64 {
	s.stateSeq++
	return s.stateSeq
}

func (s *State) emit(ev Event) {
	ev.Seq = s.seq
	s.seq++
	if ev.Step == 0 {
		ev.Step = s.Usage.Steps
	}
	s.Events = append(s.Events, ev)
}

