package regex

import (
	"fmt"
	"sort"
)

// Group span: -1,-1 means "participated but currently unmatched" is not
// used; nil = did not participate, [s,e] = captured s..e.
type span = [2]int

// frameKind tags explicit-stack frames.
type frameKind uint8

const (
	fSplit frameKind = iota
	fCut
	fLook
)

// frame is a backtracking choice point / resource marker.
type frame struct {
	kind  frameKind
	altPC int   // split: alternative PC
	pc    int   // split: the split PC (for traces)
	caps  []*[2]int
	loops map[int]int
	trace bool // split: emit choose event when resumed
	// look frames
	look     *lookState
	cutOpen  bool
	cutID    int
}

type lookKind uint8

const (
	lkLA lookKind = iota
	lkNLA
	lkLB
	lkNLB
)

// lookState is the mutable state of one inlined lookaround attempt.
type lookState struct {
	kind       lookKind
	parentPC   int    // PC of the instruction to continue at after the look
	parentPos  int    // element position at entry (LB scans backward from it)
	cand       int    // LB: current candidate start being tested
	savePC     int    // LB: body entry PC (LBStart pc+1)
}

// VM is the explicit-stack backtracking interpreter.
type VM struct {
	prog *Program
	in   *Input
	pc   int
	pos  int
	caps []*[2]int
	// loops[loopID] = element position of last iteration entry while active
	loops map[int]int
	stack []*frame

	budgets Budgets
	used    BudgetUse
	events  []Event
	seq     int
	outcome Outcome
	halted  bool
	paused  bool
	stop    bool // breakpoint requested before executing at this pc
	bp      map[int]bool

	lastDeltas []CapDelta
}

// NewVM builds an interpreter. The subject may contain invalid UTF-8 bytes;
// they are decoded into synthetic elements by NewInput.
func NewVM(prog *Program, input []byte, b Budgets) *VM {
	in := NewInput(input)
	return &VM{
		prog: prog, in: in,
		caps:    make([]*[2]int, prog.Meta.NumGroups+1),
		loops:   map[int]int{},
		budgets: b,
		bp:      map[int]bool{},
	}
}

// Input exposes the decoded subject (for position displays).
func (v *VM) Input() *Input { return v.in }

// SetBreakpoint halts execution at a given instruction PC (pre-execution).
func (v *VM) SetBreakpoint(pc int, on bool) {
	if on {
		v.bp[pc] = true
	} else {
		delete(v.bp, pc)
	}
}

// Snapshot is the serializable frontier of an indeterminate/paused run.
type Snapshot struct {
	PC      int           `json:"pc"`
	Pos     int           `json:"pos"`
	Caps    []*[2]int     `json:"caps"`
	Loops   []loopEntry   `json:"loops"`
	Stack   []FrameExport `json:"stack"`
	Budgets Budgets       `json:"budgets"`
}

type loopEntry struct {
	ID  int `json:"id"`
	Pos int `json:"pos"`
}

// FrameExport is a stack frame rendered for the UI.
type FrameExport struct {
	Kind    string `json:"kind"`
	PC      int    `json:"pc,omitempty"`
	AltPC   int    `json:"altPc,omitempty"`
	Look    string `json:"look,omitempty"`
	Cand    int    `json:"cand,omitempty"`
	Parent  int    `json:"parentPos,omitempty"`
	Depth   int    `json:"depth"`
}

func (v *VM) Frontier() Snapshot {
	s := Snapshot{PC: v.pc, Pos: v.pos, Caps: v.caps, Budgets: v.budgets}
	for id, p := range v.loops {
		s.Loops = append(s.Loops, loopEntry{id, p})
	}
	sort.Slice(s.Loops, func(i, j int) bool { return s.Loops[i].ID < s.Loops[j].ID })
	for i, f := range v.stack {
		fe := FrameExport{Depth: i}
		switch f.kind {
		case fSplit:
			fe.Kind, fe.PC, fe.AltPC = "split", f.pc, f.altPC
		case fCut:
			fe.Kind = "cut"
		case fLook:
			fe.Kind, fe.Cand, fe.Parent = "look", f.look.cand, f.look.parentPos
			fe.Look = [...]string{"(?=", "(?!", "(?<=", "(?<!"}[f.look.kind]
		}
		s.Stack = append(s.Stack, fe)
	}
	return s
}

func cloneCaps(src []*[2]int) []*[2]int {
	dst := make([]*[2]int, len(src))
	for i, s := range src {
		if s != nil {
			cp := *s
			dst[i] = &cp
		}
	}
	return dst
}

func cloneLoops(src map[int]int) map[int]int {
	if len(src) == 0 {
		return map[int]int{}
	}
	dst := make(map[int]int, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// chargeSnapshot accounts for one capture/loop snapshot against the budget.
func (v *VM) chargeSnapshot(n int) bool {
	v.used.Snapshots += n
	if v.budgets.Snapshots > 0 && v.used.Snapshots > v.budgets.Snapshots {
		return false
	}
	return true
}

func (v *VM) pushFrame(f *frame) bool {
	if f.caps == nil {
		f.caps = cloneCaps(v.caps)
	} else {
		f.caps = cloneCaps(f.caps)
	}
	if f.loops == nil {
		f.loops = cloneLoops(v.loops)
	} else {
		f.loops = cloneLoops(f.loops)
	}
	if !v.chargeSnapshot(1) {
		return false
	}
	v.stack = append(v.stack, f)
	if len(v.stack) > v.used.MaxStates {
		v.used.MaxStates = len(v.stack)
		if v.budgets.States > 0 && len(v.stack) > v.budgets.States {
			return false
		}
	}
	return true
}

func (v *VM) capView() map[string]Cap {
	m := map[string]Cap{}
	for i, sp := range v.caps {
		if sp == nil {
			continue
		}
		c := Cap{Group: i, Start: sp[0], End: sp[1], Matched: true,
			StartByte: v.in.ByteOffset(sp[0]), EndByte: v.in.ByteOffset(sp[1]),
			StartCp:   v.in.CpOffset(sp[0]), EndCp: v.in.CpOffset(sp[1])}
		if i < len(v.prog.Meta.GroupNames) {
			c.Name = v.prog.Meta.GroupNames[i]
		}
		key := fmt.Sprintf("%d", i)
		m[key] = c
		if c.Name != "" {
			m[c.Name] = c
		}
	}
	return m
}

func (v *VM) record(kind EventKind, in Instr, detail, branch string) {
	ev := Event{
		Seq: v.seq, Kind: kind, PC: v.pc, Pos: v.pos,
		BytePos: v.in.ByteOffset(v.pos), CpPos: v.in.CpOffset(v.pos),
		Label: in.Trace.Label, Detail: detail, Branch: branch,
	}
	if kind == EvStep || kind == EvCapUpdate || kind == EvChoose || kind == EvLook {
		ev.Caps = v.capView()
	}
	if len(v.lastDeltas) > 0 {
		ev.Deltas = v.lastDeltas
		v.lastDeltas = nil
	}
	v.events = append(v.events, ev)
	v.seq++
}

func (v *VM) setCap(g, val int, isEnd bool) {
	if g >= len(v.caps) {
		return
	}
	old := v.caps[g]
	var oldv *[2]int
	if old != nil {
		cp := *old
		oldv = &cp
	}
	if old == nil {
		v.caps[g] = &[2]int{-1, -1}
		old = v.caps[g]
	}
	if isEnd {
		v.caps[g][1] = val
	} else {
		v.caps[g][0] = val
	}
	newv := *v.caps[g]
	name := ""
	if g < len(v.prog.Meta.GroupNames) {
		name = v.prog.Meta.GroupNames[g]
	}
	v.lastDeltas = append(v.lastDeltas, CapDelta{Group: g, Name: name, Before: oldv, After: &newv})
}
