package regex

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

// Status is the outcome of a run. "indeterminate" never masquerades
// as no-match: it always carries a resumable frontier.
type Status string

const (
	StatusRunning       Status = "running"
	StatusMatch         Status = "match"
	StatusNoMatch       Status = "no-match"
	StatusIndeterminate Status = "indeterminate"
)

// Budget bounds execution. Zero fields get defaults. Budgets are
// cumulative: resuming with a larger budget continues the same run.
type Budget struct {
	MaxSteps     int `json:"maxSteps"`
	MaxStates    int `json:"maxStates"`
	MaxSnapshots int `json:"maxSnapshots"`
}

// DefaultBudget is used for fields left at zero.
func (b Budget) withDefaults() Budget {
	if b.MaxSteps <= 0 {
		b.MaxSteps = 200000
	}
	if b.MaxStates <= 0 {
		b.MaxStates = 20000
	}
	if b.MaxSnapshots <= 0 {
		b.MaxSnapshots = 400000
	}
	return b
}

// Stats records resource consumption.
type Stats struct {
	Steps      int `json:"steps"`
	PeakStates int `json:"peakStates"`
	Snapshots  int `json:"snapshots"`
}

// Thread is one backtracking state. Saved holds capture slots
// (2i=start, 2i+1=end of group i; -1 = unset). Loops is the
// zero-width-repeat guard: loop id -> input position of last entry.
type Thread struct {
	PC    int         `json:"pc"`
	Pos   int         `json:"pos"`
	Saved []int       `json:"saved"`
	Loops map[int]int `json:"loops,omitempty"`
}

func (t *Thread) clone() Thread {
	saved := make([]int, len(t.Saved))
	copy(saved, t.Saved)
	loops := make(map[int]int, len(t.Loops))
	for k, v := range t.Loops {
		loops[k] = v
	}
	return Thread{PC: t.PC, Pos: t.Pos, Saved: saved, Loops: loops}
}

// TraceEvent is one observable step of the execution.
type TraceEvent struct {
	Step   int    `json:"step"`
	Depth  int    `json:"depth"`
	PC     int    `json:"pc"`
	Op     string `json:"op"`
	Pos    int    `json:"pos"`  // rune offset
	Byte   int    `json:"byte"` // UTF-8 byte offset
	Action string `json:"action"`
	Detail string `json:"detail"`
	Caps   []int  `json:"caps"` // capture slots snapshot after the event
}

// Frontier is the serialized search frontier of an indeterminate run.
type Frontier struct {
	NextStart int      `json:"nextStart"`
	Cur       *Thread  `json:"cur,omitempty"`
	Stack     []Thread `json:"stack"`
}

const maxTraceEvents = 4000

// VM is an explicit-stack backtracking executor. Branch order is
// documented and stable: alternation arms are tried left to right,
// greedy repeats prefer the body, lazy repeats prefer the exit.
type VM struct {
	prog    []Inst
	input   []rune
	byteOff []int
	slots   int

	search     bool // try successive start positions
	requireEnd int  // -1: any end; otherwise OpMatch must be at this pos
	nextStart  int

	cur     *Thread
	stack   []Thread
	matched *Thread
	pruned  []Thread

	status Status
	reason string

	budget Budget

	stats      *Stats
	events     *[]TraceEvent
	traceTrunc *bool
	depth      int
}

// NewVM builds a top-level VM over input. When anchored is true the
// whole input must match (used by the semantic differ).
func NewVM(prog *Program, input string, budget Budget, anchored bool) *VM {
	runes, offs := DecodeInput(input)
	v := &VM{
		prog:       prog.Insts,
		input:      runes,
		byteOff:    offs,
		slots:      2 * (prog.NumGroups + 1),
		search:     !anchored,
		requireEnd: -1,
		budget:     budget.withDefaults(),
		stats:      &Stats{},
		events:     &[]TraceEvent{},
		status:     StatusRunning,
	}
	v.traceTrunc = new(bool)
	if anchored {
		v.requireEnd = len(runes)
		v.cur = v.newThread(0, 0)
		v.cur.Saved[0] = 0
	}
	return v
}

// DecodeInput decodes UTF-8; invalid bytes become U+FFFD of width 1
// (same rule as Go's range). Returns runes and rune->byte offsets.
func DecodeInput(s string) ([]rune, []int) {
	var rs []rune
	var offs []int
	i := 0
	for i < len(s) {
		r, w := utf8.DecodeRuneInString(s[i:])
		rs = append(rs, r)
		offs = append(offs, i)
		i += w
	}
	offs = append(offs, len(s))
	return rs, offs
}

func (v *VM) newThread(pc, pos int) *Thread {
	saved := make([]int, v.slots)
	for i := range saved {
		saved[i] = -1
	}
	return &Thread{PC: pc, Pos: pos, Saved: saved, Loops: map[int]int{}}
}

// StatusOf reports the current status.
func (v *VM) StatusOf() Status { return v.status }

// Reason explains an indeterminate outcome.
func (v *VM) Reason() string { return v.reason }

// Stats returns a copy of the counters.
func (v *VM) Stats() Stats { return *v.stats }

// Events returns the trace recorded so far.
func (v *VM) Events() []TraceEvent { return *v.events }

// TraceTruncated reports whether the trace hit its event cap.
func (v *VM) TraceTruncated() bool { return *v.traceTrunc }

// Matched returns the winning thread, if any.
func (v *VM) Matched() *Thread { return v.matched }

// Pruned returns backtrack points discarded when the match was found.
func (v *VM) Pruned() []Thread { return v.pruned }

// Stack returns the live backtrack stack (the active states).
func (v *VM) Stack() []Thread { return v.stack }

// Cur returns the active thread.
func (v *VM) Cur() *Thread { return v.cur }

// Input returns the decoded runes and byte offsets.
func (v *VM) Input() ([]rune, []int) { return v.input, v.byteOff }

// Run executes until a terminal status.
func (v *VM) Run() {
	for v.status == StatusRunning {
		v.Step()
	}
}

// Step executes one micro-step (one instruction, one backtrack pop,
// or one search-start). Budget checks happen before any mutation so
// an indeterminate run can be resumed deterministically.
func (v *VM) Step() {
	if v.status != StatusRunning {
		return
	}
	if v.cur == nil {
		if len(v.stack) > 0 {
			t := v.stack[len(v.stack)-1]
			v.stack = v.stack[:len(v.stack)-1]
			v.cur = &t
			v.emit(t.PC, t.Pos, "backtrack", fmt.Sprintf("restore pc=%d pos=%d", t.PC, t.Pos))
			return
		}
		if v.search && v.nextStart <= len(v.input) {
			v.cur = v.newThread(0, v.nextStart)
			v.cur.Saved[0] = v.nextStart
			v.emit(0, v.nextStart, "start", fmt.Sprintf("search from pos=%d", v.nextStart))
			v.nextStart++
			return
		}
		v.status = StatusNoMatch
		return
	}
	if v.stats.Steps >= v.budget.MaxSteps {
		v.status = StatusIndeterminate
		v.reason = fmt.Sprintf("step budget exceeded (%d)", v.budget.MaxSteps)
		return
	}
	in := v.prog[v.cur.PC]
	v.stats.Steps++
	switch in.Op {
	case OpRune:
		if v.cur.Pos < len(v.input) && v.input[v.cur.Pos] == in.R {
			v.emit(v.cur.PC, v.cur.Pos, "char", fmt.Sprintf("match %s", quoteRune(in.R)))
			v.cur.Pos++
			v.cur.PC++
		} else {
			v.fail(fmt.Sprintf("want %s", quoteRune(in.R)))
		}
	case OpClass:
		if v.cur.Pos < len(v.input) && in.Class.Contains(v.input[v.cur.Pos]) {
			v.emit(v.cur.PC, v.cur.Pos, "char", fmt.Sprintf("class matches %s", quoteRune(v.input[v.cur.Pos])))
			v.cur.Pos++
			v.cur.PC++
		} else {
			v.fail("class mismatch")
		}
	case OpAny:
		if v.cur.Pos < len(v.input) && v.input[v.cur.Pos] != '\n' {
			v.emit(v.cur.PC, v.cur.Pos, "char", fmt.Sprintf("any matches %s", quoteRune(v.input[v.cur.Pos])))
			v.cur.Pos++
			v.cur.PC++
		} else {
			v.fail("any mismatch")
		}
	case OpSplit:
		cost := len(v.cur.Saved) + len(v.cur.Loops)
		if v.stats.Snapshots+cost > v.budget.MaxSnapshots {
			v.status = StatusIndeterminate
			v.reason = fmt.Sprintf("capture snapshot budget exceeded (%d)", v.budget.MaxSnapshots)
			return
		}
		if len(v.stack)+1 > v.budget.MaxStates {
			v.status = StatusIndeterminate
			v.reason = fmt.Sprintf("state budget exceeded (%d)", v.budget.MaxStates)
			return
		}
		alt := v.cur.clone()
		alt.PC = in.Y
		v.stack = append(v.stack, alt)
		v.stats.Snapshots += cost
		if len(v.stack) > v.stats.PeakStates {
			v.stats.PeakStates = len(v.stack)
		}
		v.emit(v.cur.PC, v.cur.Pos, "branch", fmt.Sprintf("try pc=%d; pushed pc=%d pos=%d", in.X, in.Y, v.cur.Pos))
		v.cur.PC = in.X
	case OpJmp:
		v.cur.PC = in.X
	case OpSave:
		v.cur.Saved[in.Slot] = v.cur.Pos
		v.emit(v.cur.PC, v.cur.Pos, "capture", fmt.Sprintf("slot %d := %d", in.Slot, v.cur.Pos))
		v.cur.PC++
	case OpMatch:
		if v.requireEnd >= 0 && v.cur.Pos != v.requireEnd {
			v.fail(fmt.Sprintf("match requires end at %d", v.requireEnd))
			return
		}
		v.cur.Saved[1] = v.cur.Pos
		v.matched = v.cur
		v.pruned = v.stack
		v.stack = nil
		v.status = StatusMatch
		v.emit(v.cur.PC, v.cur.Pos, "match", fmt.Sprintf("accept; %d pending branches pruned", len(v.pruned)))
	case OpAssert:
		if v.checkAssert(in.Kind, v.cur.Pos) {
			v.emit(v.cur.PC, v.cur.Pos, "assert-ok", (&Anchor{Kind: in.Kind}).String())
			v.cur.PC++
		} else {
			v.fail("assert " + (&Anchor{Kind: in.Kind}).String())
		}
	case OpBackRef:
		s, e := -1, -1
		if 2*in.N+1 < len(v.cur.Saved) {
			s, e = v.cur.Saved[2*in.N], v.cur.Saved[2*in.N+1]
		}
		if s < 0 || e < 0 {
			v.fail(fmt.Sprintf("backref #%d: group unset", in.N))
			return
		}
		l := e - s
		if v.cur.Pos+l > len(v.input) {
			v.fail(fmt.Sprintf("backref #%d: not enough input", in.N))
			return
		}
		for i := 0; i < l; i++ {
			if v.input[v.cur.Pos+i] != v.input[s+i] {
				v.fail(fmt.Sprintf("backref #%d: mismatch", in.N))
				return
			}
		}
		v.emit(v.cur.PC, v.cur.Pos, "backref", fmt.Sprintf("#%d matched %d rune(s)", in.N, l))
		v.cur.Pos += l
		v.cur.PC++
	case OpLoop:
		if prev, ok := v.cur.Loops[in.ID]; ok && prev == v.cur.Pos {
			v.fail(fmt.Sprintf("empty-loop prune (loop %d at pos %d)", in.ID, v.cur.Pos))
			return
		}
		v.cur.Loops[in.ID] = v.cur.Pos
		v.cur.PC++
	case OpLook:
		ok, saved, err := v.runLook(in)
		if err != nil {
			// cur is untouched: resuming re-runs the lookaround.
			v.status = StatusIndeterminate
			v.reason = err.Error()
			return
		}
		if ok == in.Neg {
			v.fail("lookaround assertion failed")
			return
		}
		if !in.Neg && saved != nil {
			v.cur.Saved = saved
		}
		v.emit(v.cur.PC, v.cur.Pos, "look", "lookaround assertion passed")
		v.cur.PC++
	}
}

func (v *VM) fail(detail string) {
	v.emit(v.cur.PC, v.cur.Pos, "fail", detail)
	v.cur = nil
}

func (v *VM) checkAssert(k AnchorKind, pos int) bool {
	word := func(i int) bool {
		return i >= 0 && i < len(v.input) && isWordRune(v.input[i])
	}
	switch k {
	case AnchorBeginText, AnchorBeginTextCaret:
		return pos == 0
	case AnchorEndText:
		return pos == len(v.input)
	case AnchorEndTextOrFinalNL:
		return pos == len(v.input) || (pos == len(v.input)-1 && v.input[pos] == '\n')
	case AnchorWordBoundary:
		return word(pos-1) != word(pos)
	case AnchorNonWordBoundary:
		return word(pos-1) == word(pos)
	}
	return false
}

// runLook evaluates a lookaround sub-program sharing budget counters
// and the trace sink. Lookbehind tries start positions nearest-first.
func (v *VM) runLook(in Inst) (bool, []int, error) {
	try := func(start int) (bool, []int, error) {
		requireEnd := -1
		if in.Behind {
			requireEnd = v.cur.Pos
		}
		sub := &VM{
			prog:       in.Sub,
			input:      v.input,
			byteOff:    v.byteOff,
			slots:      v.slots,
			search:     false,
			requireEnd: requireEnd,
			budget:     v.budget,
			stats:      v.stats,
			events:     v.events,
			traceTrunc: v.traceTrunc,
			depth:      v.depth + 1,
			status:     StatusRunning,
		}
		t := v.newThread(0, start)
		copy(t.Saved, v.cur.Saved)
		sub.cur = t
		sub.Run()
		switch sub.status {
		case StatusIndeterminate:
			return false, nil, errors.New("lookaround: " + sub.reason)
		case StatusMatch:
			return true, sub.matched.Saved, nil
		}
		return false, nil, nil
	}
	if !in.Behind {
		return try(v.cur.Pos)
	}
	for s := v.cur.Pos; s >= 0; s-- {
		ok, saved, err := try(s)
		if err != nil || ok {
			return ok, saved, err
		}
	}
	return false, nil, nil
}

// Frontier snapshots the resumable state of an indeterminate run.
func (v *VM) Frontier() *Frontier {
	if v.status != StatusIndeterminate {
		return nil
	}
	f := &Frontier{NextStart: v.nextStart}
	f.Stack = append(f.Stack, v.stack...)
	if v.cur != nil {
		c := v.cur.clone()
		f.Cur = &c
	}
	return f
}

// ResumeFrom restores a frontier and continues with a new budget.
func (v *VM) ResumeFrom(f *Frontier, b Budget) {
	v.nextStart = f.NextStart
	v.stack = append([]Thread(nil), f.Stack...)
	v.cur = nil
	if f.Cur != nil {
		c := f.Cur.clone()
		v.cur = &c
	}
	v.budget = b.withDefaults()
	v.status = StatusRunning
	v.reason = ""
}

func (v *VM) emit(pc, pos int, action, detail string) {
	if len(*v.events) >= maxTraceEvents {
		*v.traceTrunc = true
		return
	}
	op := ""
	if pc >= 0 && pc < len(v.prog) {
		op = instString(v.prog[pc])
	}
	var caps []int
	if v.cur != nil {
		caps = append(caps, v.cur.Saved...)
	}
	by := 0
	if pos >= 0 && pos < len(v.byteOff) {
		by = v.byteOff[pos]
	}
	*v.events = append(*v.events, TraceEvent{
		Step:   v.stats.Steps,
		Depth:  v.depth,
		PC:     pc,
		Op:     op,
		Pos:    pos,
		Byte:   by,
		Action: action,
		Detail: detail,
		Caps:   caps,
	})
}
