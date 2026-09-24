package engine

import (
	"fmt"

	"pairwise-gsb/internal/syntax"
)

// Status is the outcome of a run. Indeterminate is never collapsed
// into no-match: it always carries a resumable frontier.
type Status string

const (
	StatusMatch         Status = "match"
	StatusNoMatch       Status = "no-match"
	StatusIndeterminate Status = "indeterminate"
)

// Budget bounds execution. A zero field means unlimited.
type Budget struct {
	MaxSteps  int64 `json:"maxSteps"`
	MaxStates int64 `json:"maxStates"`
	MaxSaves  int64 `json:"maxSaves"`
}

// Counters accumulate resource usage across a run and its resumes.
type Counters struct {
	Steps  int64 `json:"steps"`
	States int64 `json:"states"`
	Saves  int64 `json:"saves"`
}

// State is one suspended backtracking point.
type State struct {
	PC    int   `json:"pc"`
	Pos   int   `json:"pos"`
	Caps  []int `json:"caps"`
	Marks []int `json:"marks"`
}

// Frontier is the set of pending states when execution pauses
// (budget exhaustion or breakpoint). It fully captures resumability.
type Frontier struct {
	Cur   *State  `json:"cur,omitempty"`
	Stack []State `json:"stack"`
	Next  int     `json:"next"` // next unanchored start position
}

// Depth returns the number of pending states.
func (f *Frontier) Depth() int {
	n := len(f.Stack)
	if f.Cur != nil {
		n++
	}
	return n
}

// Event is one trace record.
type Event struct {
	Step   int64  `json:"step"`
	Depth  int    `json:"depth"`
	Kind   string `json:"kind"`
	PC     int    `json:"pc"`
	CP     int    `json:"cp"`
	Byte   int    `json:"byte"`
	Detail string `json:"detail"`
}

// Result is the outcome of Run.
type Result struct {
	Status   Status
	CapsCP   []int
	CapsByte []int
	Reason   string
	Frontier *Frontier
}

// VM is a backtracking executor with explicit state stack.
type VM struct {
	prog     *Program
	in       *Input
	budget   Budget
	c        *Counters
	breaks   map[int]bool
	events   []Event
	depth    int
	anchored bool

	cur           State
	haveCur       bool
	hasPrev       bool
	stack         []State
	next          int
	suppressBreak bool
}

// NewVM builds a VM that searches leftmost from position 0.
func NewVM(prog *Program, in *Input, b Budget, breaks map[int]bool) *VM {
	v := &VM{prog: prog, in: in, budget: b, c: &Counters{}, breaks: breaks}
	v.stack = []State{v.initial(0)}
	v.next = 1
	return v
}

// ResumeVM rebuilds a VM from a frontier, sharing counters and events.
func ResumeVM(prog *Program, in *Input, b Budget, c *Counters, f *Frontier, breaks map[int]bool, events []Event) *VM {
	v := &VM{prog: prog, in: in, budget: b, c: c, breaks: breaks, events: events}
	if f.Cur != nil {
		v.cur = *f.Cur
		v.haveCur = true
		v.hasPrev = true
		v.suppressBreak = true
	}
	v.stack = append([]State(nil), f.Stack...)
	v.next = f.Next
	return v
}

func (v *VM) initial(pos int) State {
	caps := make([]int, v.prog.NumSlots)
	for i := range caps {
		caps[i] = -1
	}
	return State{PC: 0, Pos: pos, Caps: caps, Marks: make([]int, v.prog.NumMarks)}
}

// Events returns the trace accumulated so far.
func (v *VM) Events() []Event { return v.events }

// Counters returns the shared counters.
func (v *VM) Counters() *Counters { return v.c }

func (v *VM) emit(kind string, pc, pos int, detail string) {
	v.events = append(v.events, Event{
		Step:   v.c.Steps,
		Depth:  v.depth,
		Kind:   kind,
		PC:     pc,
		CP:     pos,
		Byte:   v.in.BytePos(pos),
		Detail: detail,
	})
}

func (v *VM) exceeded() bool {
	b := v.budget
	return (b.MaxSteps > 0 && v.c.Steps >= b.MaxSteps) ||
		(b.MaxStates > 0 && v.c.States >= b.MaxStates) ||
		(b.MaxSaves > 0 && v.c.Saves >= b.MaxSaves)
}

func (v *VM) indeterminate(reason string) Result {
	f := &Frontier{Stack: append([]State(nil), v.stack...), Next: v.next}
	if v.haveCur {
		cur := v.cur
		f.Cur = &cur
	}
	return Result{Status: StatusIndeterminate, Reason: reason, Frontier: f}
}

func cloneInts(s []int) []int {
	out := make([]int, len(s))
	copy(out, s)
	return out
}

// Run executes until match, exhaustive failure, budget, or breakpoint.
func (v *VM) Run() Result {
	for {
		if !v.haveCur {
			if len(v.stack) == 0 {
				if !v.anchored && v.next <= v.in.Len() {
					v.stack = append(v.stack, v.initial(v.next))
					v.emit("restart", 0, v.next, "advance search start")
					v.next++
					continue
				}
				return Result{Status: StatusNoMatch}
			}
			top := v.stack[len(v.stack)-1]
			v.stack = v.stack[:len(v.stack)-1]
			if v.hasPrev {
				v.emitRollback(top)
			}
			v.cur = top
			v.haveCur = true
			v.hasPrev = true
			v.emit("backtrack", top.PC, top.Pos, fmt.Sprintf("restore pc=%d", top.PC))
			continue
		}
		if v.exceeded() {
			return v.indeterminate("budget exceeded")
		}
		pc := v.cur.PC
		inst := v.prog.Insts[pc]
		if v.breaks[pc] && !v.suppressBreak {
			return v.indeterminate(fmt.Sprintf("breakpoint at pc=%d", pc))
		}
		v.suppressBreak = false
		v.c.Steps++
		switch inst.Op {
		case OpRune:
			if v.cur.Pos < v.in.Len() && v.in.Runes[v.cur.Pos] == inst.R {
				v.emit("char", pc, v.cur.Pos, fmt.Sprintf("match %q", string(inst.R)))
				v.cur.Pos++
				v.cur.PC++
			} else {
				v.fail(fmt.Sprintf("want %q", string(inst.R)))
			}
		case OpAny:
			if v.cur.Pos < v.in.Len() {
				v.emit("char", pc, v.cur.Pos, fmt.Sprintf("any matches %q", string(v.in.Runes[v.cur.Pos])))
				v.cur.Pos++
				v.cur.PC++
			} else {
				v.fail("any at end of input")
			}
		case OpClass:
			if v.cur.Pos < v.in.Len() && inst.Class.Match(v.in.Runes[v.cur.Pos]) {
				v.emit("char", pc, v.cur.Pos, fmt.Sprintf("class %s matches %q", inst.Class.Desc, string(v.in.Runes[v.cur.Pos])))
				v.cur.Pos++
				v.cur.PC++
			} else {
				v.fail("class " + inst.Class.Desc)
			}
		case OpSplit:
			alt := State{PC: inst.B, Pos: v.cur.Pos, Caps: cloneInts(v.cur.Caps), Marks: cloneInts(v.cur.Marks)}
			v.stack = append(v.stack, alt)
			v.c.States++
			v.emit("branch", pc, v.cur.Pos, fmt.Sprintf("try pc=%d, defer pc=%d", inst.A, inst.B))
			v.cur.PC = inst.A
		case OpJmp:
			v.cur.PC = inst.A
		case OpSave:
			old := v.cur.Caps[inst.Slot]
			v.cur.Caps[inst.Slot] = v.cur.Pos
			v.c.Saves++
			v.emit("capture-set", pc, v.cur.Pos, fmt.Sprintf("%s: %d -> %d", slotName(v.prog, inst.Slot), old, v.cur.Pos))
			v.cur.PC++
		case OpProgress:
			if v.cur.Marks[inst.Slot] == v.cur.Pos {
				v.fail("zero-width repeat terminated")
			} else {
				v.cur.Marks[inst.Slot] = v.cur.Pos
				v.cur.PC++
			}
		case OpBackref:
			v.execBackref(inst)
		case OpAssert:
			if v.checkAssert(inst.Anchor) {
				v.cur.PC++
			} else {
				v.fail("assert " + inst.Anchor.String())
			}
		case OpLook:
			matched, ok := v.runLook(inst)
			if !ok {
				return v.indeterminate("budget exceeded inside lookaround")
			}
			if matched == inst.Look.Positive() {
				v.emit("look-end", pc, v.cur.Pos, fmt.Sprintf("%s satisfied=%v", inst.Look.String(), matched))
				v.cur.PC++
			} else {
				v.fail(fmt.Sprintf("look %s unsatisfied", inst.Look.String()))
			}
		case OpMatch:
			v.emit("match", pc, v.cur.Pos, "match found")
			for _, st := range v.stack {
				v.emit("pruned", st.PC, st.Pos, fmt.Sprintf("pruned pending branch pc=%d pos=%d", st.PC, st.Pos))
			}
			caps := cloneInts(v.cur.Caps)
			byteCaps := make([]int, len(caps))
			for i, cp := range caps {
				if cp < 0 {
					byteCaps[i] = -1
				} else {
					byteCaps[i] = v.in.BytePos(cp)
				}
			}
			return Result{Status: StatusMatch, CapsCP: caps, CapsByte: byteCaps}
		}
	}
}

func (v *VM) fail(detail string) {
	v.emit("fail", v.cur.PC, v.cur.Pos, detail)
	v.haveCur = false
}

func (v *VM) emitRollback(next State) {
	for i := range next.Caps {
		if next.Caps[i] != v.cur.Caps[i] {
			v.emit("capture-rollback", next.PC, next.Pos,
				fmt.Sprintf("%s: %d -> %d", slotName(v.prog, i), v.cur.Caps[i], next.Caps[i]))
		}
	}
}

func slotName(p *Program, slot int) string {
	g := slot / 2
	end := "start"
	if slot%2 == 1 {
		end = "end"
	}
	name := ""
	if g < len(p.Groups) && p.Groups[g] != "" {
		name = "(" + p.Groups[g] + ")"
	}
	return fmt.Sprintf("g%d%s.%s", g, name, end)
}

func (v *VM) execBackref(inst Inst) {
	g := inst.Group
	s, e := v.cur.Caps[2*g], v.cur.Caps[2*g+1]
	if s < 0 || e < 0 {
		v.fail(fmt.Sprintf("backref g%d unset", g))
		return
	}
	l := e - s
	if v.cur.Pos+l > v.in.Len() {
		v.fail(fmt.Sprintf("backref g%d past end", g))
		return
	}
	for i := 0; i < l; i++ {
		if v.in.Runes[v.cur.Pos+i] != v.in.Runes[s+i] {
			v.fail(fmt.Sprintf("backref g%d mismatch", g))
			return
		}
	}
	v.emit("backref", v.cur.PC, v.cur.Pos, fmt.Sprintf("g%d matched %q", g, v.in.Slice(s, e)))
	v.cur.Pos += l
	v.cur.PC++
}

func (v *VM) checkAssert(a syntax.AnchorKind) bool {
	pos := v.cur.Pos
	n := v.in.Len()
	switch a {
	case syntax.AnchorStart, syntax.AnchorTextStart:
		return pos == 0
	case syntax.AnchorEnd, syntax.AnchorTextEnd:
		return pos == n
	case syntax.AnchorWordBoundary, syntax.AnchorNotWordBoundary:
		before := pos > 0 && IsWordRune(v.in.Runes[pos-1])
		after := pos < n && IsWordRune(v.in.Runes[pos])
		wb := before != after
		if a == syntax.AnchorNotWordBoundary {
			return !wb
		}
		return wb
	}
	return false
}

// runLook evaluates a lookaround subprogram. Captures inside a
// lookaround are discarded by design. The second return value is false
// when the shared budget ran out; the caller then pauses with the
// outer frontier (the LOOK instruction is re-executed on resume).
func (v *VM) runLook(inst Inst) (bool, bool) {
	sub := v.prog.Subs[inst.Sub]
	v.emit("look-begin", v.cur.PC, v.cur.Pos, inst.Look.String())
	run := func(start int) (matched bool, end int, ok bool) {
		sv := &VM{
			prog: sub, in: v.in, budget: v.budget, c: v.c,
			breaks: map[int]bool{}, depth: v.depth + 1, anchored: true,
		}
		sv.stack = []State{sv.initial(start)}
		sv.events = v.events
		res := sv.Run()
		v.events = sv.events
		if res.Status == StatusIndeterminate {
			return false, 0, false
		}
		if res.Status == StatusMatch {
			return true, res.CapsCP[1], true
		}
		return false, 0, true
	}
	if inst.Look == syntax.LookAhead || inst.Look == syntax.LookAheadNeg {
		m, _, ok := run(v.cur.Pos)
		return m, ok
	}
	// Lookbehind: try every start position, require the match to end
	// exactly at the current position.
	for start := 0; start <= v.cur.Pos; start++ {
		m, end, ok := run(start)
		if !ok {
			return false, false
		}
		if m && end == v.cur.Pos {
			return true, true
		}
	}
	return false, true
}
