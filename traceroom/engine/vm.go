package engine

import (
	"unicode"

	"traceroom/compile"
	"traceroom/syntax"
)

// UnicodeDataVersion is pinned to the Go unicode tables this build links.
const UnicodeDataVersion = unicode.Version

// Result is the outcome of a run (possibly resumable).
type Result struct {
	Status         Status         `json:"status"`
	Reason         LimitReason    `json:"reason,omitempty"`
	Match          bool           `json:"match"`
	Caps           []Capture      `json:"caps,omitempty"`
	Start          Pos            `json:"start"`
	End            Pos            `json:"end"`
	Events         []Event        `json:"events,omitempty"`
	Steps          int            `json:"steps"`
	Snapshots      int            `json:"snapshots"`
	Frontier       *Frontier      `json:"frontier,omitempty"`
	SyntaxVersion  string         `json:"syntaxVersion"`
	UnicodeVersion string         `json:"unicodeVersion"`
	ActiveStates   int            `json:"activeStates"`
	Pruned         []PrunedBranch `json:"pruned,omitempty"`
	Budget         BudgetInfo     `json:"budget"`
}

// BudgetInfo reports budget ranges actually applied.
type BudgetInfo struct {
	MaxSteps     int `json:"maxSteps"`
	MaxStates    int `json:"maxStates"`
	MaxSnapshots int `json:"maxSnapshots"`
}

// PrunedBranch documents a discarded branch.
type PrunedBranch struct {
	EventSeq int    `json:"eventSeq"`
	Reason   string `json:"reason"`
	Pc       int    `json:"pc"`
	Pos      Pos    `json:"pos"`
}

// Frontier is the resumable execution state.
type Frontier struct {
	StartScans []int    `json:"startScans"` // remaining leftmost start positions
	Machine    *Machine `json:"machine"`
	NextStart  int      `json:"nextStart"`
	TriedTo    int      `json:"triedTo"`
	Config     Config   `json:"config"`
}

// MachineState is the serializable form of a machine plus context.
type MachineState = Machine

// VM executes a compiled program over one input.
type VM struct {
	prog  *compile.Program
	input *Input
	cfg   Config

	r *runner

	// outer driver state
	startPos   int
	triedUntil int
	cur        *machine
	upcoming   []int // remaining start positions (leftmost scan)

	events    []Event
	pruned    []PrunedBranch
	status    Status
	reason    LimitReason
	paused    bool
	snapshots int
	steps     int
}

// NewVM constructs a VM. When fullMatch is set, the program is wrapped.
func NewVM(prog *compile.Program, input string, cfg Config) *VM {
	in := NewInput(input)
	if cfg.FullMatch {
		prog = wrapFullMatch(prog)
	}
	vm := &VM{prog: prog, input: in, cfg: cfg}
	vm.r = &runner{
		prog:      prog,
		input:     in,
		cfg:       cfg,
		maxEvents: 200000,
		pausedAt:  -1,
	}
	return vm
}

func wrapFullMatch(prog *compile.Program) *compile.Program {
	// Prepend \A and append \z: build [AnchorStart, ...code without final Match, AnchorEnd, Match].
	code := make([]compile.Instr, 0, len(prog.Code)+2)
	code = append(code, compile.Instr{Op: compile.OpAnchor, Arg: int(syntax.AnchorStart)})
	for _, in := range prog.Code {
		if in.Op == compile.OpMatch {
			code = append(code, compile.Instr{Op: compile.OpAnchor, Arg: int(syntax.AnchorEnd)})
		}
		code = append(code, in)
	}
	cp := *prog
	cp.Code = code
	return &cp
}

func (r *runner) chargeStep() bool {
	r.steps++
	if r.cfg.MaxSteps > 0 && r.steps > r.cfg.MaxSteps {
		r.reason = LimitSteps
		return false
	}
	return true
}

func (r *runner) chargeSnapshot() bool {
	r.snapshots++
	if r.cfg.MaxSnapshots > 0 && r.snapshots > r.cfg.MaxSnapshots {
		r.reason = LimitSnapshots
		return false
	}
	return true
}

func (r *runner) atBreakpoint(lo, hi int) bool {
	for _, bp := range r.cfg.Breakpoints {
		if bp >= lo && bp < hi {
			return true
		}
	}
	return false
}

// findSub resolves a subprogram id, descending through the map (ids unique).
func (r *runner) findSub(id int) *compile.Program {
	if s := r.prog.Subs[id]; s != nil {
		return s
	}
	return findSubRec(r.prog, id)
}

func findSubRec(p *compile.Program, id int) *compile.Program {
	for _, s := range p.Subs {
		if x := s.Subs[id]; x != nil {
			return x
		}
		if x := findSubRec(s, id); x != nil {
			return x
		}
	}
	return nil
}

// noteCap emits a capture update event for one slot.
func (r *runner) noteCap(slot, val, prev int) {
	group := slot / 2
	which := "start"
	if slot%2 == 1 {
		which = "end"
	}
	r.events = append(r.events, Event{
		Seq: len(r.events), Kind: EvCapUpdate, Depth: r.depth,
		Pos:    r.input.PosOf(val),
		Detail: map[string]any{"group": group, "edge": which, "prev": prev, "value": val},
	})
}

func (r *runner) rollbackCaps(m *machine, caps []int, why string) {
	diff := false
	for i := range m.caps {
		if m.caps[i] != caps[i] {
			diff = true
		}
	}
	if diff {
		r.events = append(r.events, Event{
			Seq: len(r.events), Kind: EvCapRollback, Depth: r.depth,
			Pos: r.input.PosOf(m.pos), Detail: map[string]any{"why": why},
		})
	}
	m.caps = caps
}

func (r *runner) noteCapsDiff(old, neu []int, why string) {
	r.events = append(r.events, Event{
		Seq: len(r.events), Kind: EvCapUpdate, Depth: r.depth,
		Pos: r.input.PosOf(0), Detail: map[string]any{"why": why, "lookCommit": true},
	})
	_ = old
}

// initialCaps builds [-1,-1,...] slots.
func initialCaps(n int) []int {
	c := make([]int, 2*(n+1))
	for i := range c {
		c[i] = -1
	}
	c[0] = 0
	return c
}
