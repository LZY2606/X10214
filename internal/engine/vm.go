package engine

import (
	"maps"
	"slices"

	"capturetraceroom/internal/syntax"
	"capturetraceroom/internal/unicodex"
)

// frame is a live lookaround instance on a thread's frame stack.
type frame struct {
	id           int // unique instance id
	staticID     int // compile-time frame id
	neg          bool
	behind       bool
	retPC        int
	startPos     int
	targetPos    int
	savedCaps    []int
	savedMarks   map[int]int
	parentFrames []frame
}

// thread is one priority-ordered execution path.
type thread struct {
	id     int
	pc     int
	pos    int
	caps   []int
	marks  map[int]int
	frames []frame
}

// VM executes a compiled program against one start position with an
// explicit thread stack so that runs can be paused, budgeted and resumed.
type VM struct {
	prog   *syntax.Program
	in     *Input
	budget Budgets

	steps     int
	states    int
	snapshots int

	stack []*thread
	cur   *thread

	sink    *[]Event
	matched *thread
	halted  bool
	reason  string
	paused  bool
	pauseWhy string

	counts     map[int]int // lookaround instance id -> live thread count
	frameReg   map[int]*frame // lookaround instance id -> frame data
	nextThread int
	nextFrame  int
	startPos   int

	stopAtEvents int // pause when len(*sink) reaches this (0 = never)
	breakpoints  map[int]bool
}

// NewVM builds a VM that starts matching at startPos. Counters seed the
// budget accounting so a search can span several start positions.
func NewVM(prog *syntax.Program, in *Input, startPos int, b Budgets, steps, states, snapshots int, sink *[]Event) *VM {
	vm := &VM{
		prog:   prog,
		in:     in,
		budget: b,
		steps:  steps,
		states: states,
		snapshots: snapshots,
		sink:   sink,
		counts: map[int]int{},
		frameReg: map[int]*frame{},
		startPos: startPos,
	}
	caps := make([]int, 2*(prog.NumGroups+1))
	for i := range caps {
		caps[i] = -1
	}
	caps[0] = startPos
	t := &thread{id: 0, pc: prog.Start, pos: startPos, caps: caps, marks: map[int]int{}}
	vm.nextThread = 1
	vm.cur = t
	vm.states++
	vm.emit(EvSpawn, t, "initial thread")
	return vm
}

// ResumeVM rebuilds a VM from a serialized frontier.
func ResumeVM(prog *syntax.Program, in *Input, b Budgets, steps, states, snapshots int, sink *[]Event, frontier []ThreadState) *VM {
	vm := &VM{
		prog: prog, in: in, budget: b,
		steps: steps, states: states, snapshots: snapshots,
		sink: sink, counts: map[int]int{},
		frameReg: map[int]*frame{},
	}
	maxThread, maxFrame := -1, -1
	threads := make([]*thread, 0, len(frontier))
	for _, ts := range frontier {
		t := threadFromState(ts)
		threads = append(threads, t)
		if t.id > maxThread {
			maxThread = t.id
		}
		for _, f := range t.frames {
			vm.counts[f.id]++
			fc := f
			vm.frameReg[f.id] = &fc
			if f.id > maxFrame {
				maxFrame = f.id
			}
		}
	}
	vm.nextThread = maxThread + 1
	vm.nextFrame = maxFrame + 1
	if len(threads) > 0 {
		vm.cur = threads[0]
		for i := len(threads) - 1; i >= 1; i-- {
			vm.stack = append(vm.stack, threads[i])
		}
	}
	return vm
}

func threadFromState(ts ThreadState) *thread {
	t := &thread{
		id: ts.ID, pc: ts.PC, pos: ts.Pos,
		caps:  slices.Clone(ts.Caps),
		marks: maps.Clone(ts.Marks),
	}
	if t.marks == nil {
		t.marks = map[int]int{}
	}
	for _, fs := range ts.Frames {
		t.frames = append(t.frames, frame{
			id: fs.ID, staticID: fs.StaticID, neg: fs.Neg, behind: fs.Behind,
			retPC: fs.RetPC, startPos: fs.StartPos, targetPos: fs.TargetPos,
			savedCaps: slices.Clone(fs.SavedCaps), savedMarks: maps.Clone(fs.SavedMarks),
		})
	}
	return t
}

func threadToState(t *thread) ThreadState {
	ts := ThreadState{
		ID: t.id, PC: t.pc, Pos: t.pos,
		Caps:  slices.Clone(t.caps),
		Marks: maps.Clone(t.marks),
	}
	for _, f := range t.frames {
		ts.Frames = append(ts.Frames, FrameState{
			ID: f.id, StaticID: f.staticID, Neg: f.neg, Behind: f.behind,
			RetPC: f.retPC, StartPos: f.startPos, TargetPos: f.targetPos,
			SavedCaps: slices.Clone(f.savedCaps), SavedMarks: maps.Clone(f.savedMarks),
		})
	}
	return ts
}

// Frontier serializes the remaining live threads in priority order.
func (vm *VM) Frontier() []ThreadState {
	var out []ThreadState
	if vm.cur != nil {
		out = append(out, threadToState(vm.cur))
	}
	for i := len(vm.stack) - 1; i >= 0; i-- {
		out = append(out, threadToState(vm.stack[i]))
	}
	return out
}

func (vm *VM) emit(kind EventKind, t *thread, detail string) {
	ev := Event{
		Seq:    len(*vm.sink),
		Step:   vm.steps,
		Thread: t.id,
		PC:     t.pc,
		Pos:    t.pos,
		Byte:   vm.in.ByteAt(t.pos),
		Kind:   kind,
		Detail: detail,
	}
	*vm.sink = append(*vm.sink, ev)
	if vm.stopAtEvents > 0 && len(*vm.sink) >= vm.stopAtEvents {
		vm.paused = true
		vm.pauseWhy = "event limit"
	}
}

func (vm *VM) emitCaps(kind EventKind, t *thread, group int, detail string) {
	ev := Event{
		Seq:    len(*vm.sink),
		Step:   vm.steps,
		Thread: t.id,
		PC:     t.pc,
		Pos:    t.pos,
		Byte:   vm.in.ByteAt(t.pos),
		Kind:   kind,
		Detail: detail,
		Group:  group,
		Caps:   slices.Clone(t.caps),
	}
	*vm.sink = append(*vm.sink, ev)
	if vm.stopAtEvents > 0 && len(*vm.sink) >= vm.stopAtEvents {
		vm.paused = true
		vm.pauseWhy = "event limit"
	}
}

// Run executes until the current search is matched, exhausted, halted or
// paused. It is safe to call Run again after a pause.
func (vm *VM) Run() {
	vm.paused = false
	for {
		if vm.halted || vm.matched != nil || vm.paused {
			return
		}
		if vm.cur == nil {
			if len(vm.stack) == 0 {
				return
			}
			vm.cur = vm.stack[len(vm.stack)-1]
			vm.stack = vm.stack[:len(vm.stack)-1]
		}
		vm.execOne()
	}
}

func (vm *VM) halt(reason string) {
	vm.halted = true
	vm.reason = reason
	if vm.cur != nil {
		vm.emit(EvHalt, vm.cur, "budget exceeded: "+reason)
	}
}

// execOne executes a single instruction for the current thread.
func (vm *VM) execOne() {
	t := vm.cur
	if vm.breakpoints != nil && vm.breakpoints[t.pc] {
		vm.paused = true
		vm.pauseWhy = "breakpoint"
		return
	}
	vm.steps++
	if vm.budget.Steps > 0 && vm.steps > vm.budget.Steps {
		vm.halt("steps")
		return
	}
	inst := vm.prog.Insts[t.pc]
	switch inst.Op {
	case syntax.OpChar:
		if t.pos < vm.in.Len() && matchChar(inst, vm.in.Runes[t.pos], vm.in.Raw[t.pos]) {
			vm.emit(EvChar, t, "match "+describeRune(vm.in.Runes[t.pos]))
			t.pos++
			t.pc++
		} else {
			vm.emit(EvChar, t, "fail")
			vm.killCurrent("literal mismatch")
		}
	case syntax.OpStr:
		vm.execStr(t, inst)
	case syntax.OpDot:
		if t.pos < vm.in.Len() && (inst.Flags.S || vm.in.Runes[t.pos] != '\n') {
			vm.emit(EvChar, t, "dot match")
			t.pos++
			t.pc++
		} else {
			vm.emit(EvChar, t, "dot fail")
			vm.killCurrent("dot mismatch")
		}
	case syntax.OpClass:
		if t.pos < vm.in.Len() && matchClass(vm.prog.Classes[inst.Arg], inst.Flags, vm.in.Runes[t.pos], vm.in.Raw[t.pos]) {
			vm.emit(EvChar, t, "class match")
			t.pos++
			t.pc++
		} else {
			vm.emit(EvChar, t, "class fail")
			vm.killCurrent("class mismatch")
		}
	case syntax.OpShorthand:
		if t.pos < vm.in.Len() && matchShorthand(byte(inst.Arg), vm.in.Runes[t.pos], vm.in.Raw[t.pos]) {
			vm.emit(EvChar, t, "shorthand match")
			t.pos++
			t.pc++
		} else {
			vm.emit(EvChar, t, "shorthand fail")
			vm.killCurrent("shorthand mismatch")
		}
	case syntax.OpProp:
		if t.pos < vm.in.Len() && matchProp(inst, vm.in.Runes[t.pos], vm.in.Raw[t.pos]) {
			vm.emit(EvChar, t, "property match")
			t.pos++
			t.pc++
		} else {
			vm.emit(EvChar, t, "property fail")
			vm.killCurrent("property mismatch")
		}
	case syntax.OpAnchor:
		if vm.matchAnchor(t, inst) {
			vm.emit(EvAnchor, t, "anchor ok")
			t.pc++
		} else {
			vm.emit(EvAnchor, t, "anchor fail")
			vm.killCurrent("anchor failed")
		}
	case syntax.OpBackref:
		vm.execBackref(t, inst)
	case syntax.OpGroupStart:
		t.caps[2*inst.Arg] = t.pos
		vm.emitCaps(EvGroupOpen, t, inst.Arg, "open")
		t.pc++
	case syntax.OpGroupEnd:
		t.caps[2*inst.Arg+1] = t.pos
		vm.emitCaps(EvGroupClose, t, inst.Arg, "close")
		t.pc++
	case syntax.OpSplit:
		if vm.budget.States > 0 && vm.states+1 > vm.budget.States {
			vm.halt("states")
			return
		}
		child := vm.fork(t, inst.Y)
		if child == nil {
			return // budget halt already recorded
		}
		vm.stack = append(vm.stack, child)
		vm.emit(EvSplit, t, "fork thread "+itoa(child.id))
		t.pc = inst.X
	case syntax.OpJmp:
		t.pc = inst.X
	case syntax.OpSetMark:
		t.marks[inst.Arg] = t.pos
		t.pc++
	case syntax.OpGuard:
		if t.marks[inst.Arg] == t.pos {
			vm.emit(EvGuard, t, "empty iteration, exit loop")
			t.pc = inst.X
		} else {
			t.pc++
		}
	case syntax.OpLaPush:
		vm.execLaPush(t, inst)
	case syntax.OpLaRet:
		vm.execLaRet(t, inst)
	case syntax.OpMatch:
		t.caps[1] = t.pos
		vm.matched = t
		vm.emitCaps(EvMatch, t, 0, "match")
	}
}

func (vm *VM) fork(t *thread, pc int) *thread {
	if vm.budget.Snapshots > 0 && vm.snapshots+1 > vm.budget.Snapshots {
		// The caller checks states first; treat snapshot overflow the same.
		vm.halt("snapshots")
		return nil
	}
	child := &thread{
		id:     vm.nextThread,
		pc:     pc,
		pos:    t.pos,
		caps:   slices.Clone(t.caps),
		marks:  maps.Clone(t.marks),
		frames: t.frames,
	}
	vm.nextThread++
	vm.states++
	vm.snapshots++
	for _, f := range child.frames {
		vm.counts[f.id]++
	}
	vm.emit(EvSpawn, child, "fork")
	return child
}

// killCurrent retires the executing thread and resolves any lookaround
// frames whose live count reaches zero.
func (vm *VM) killCurrent(reason string) {
	t := vm.cur
	vm.emit(EvBacktrack, t, reason)
	vm.cur = nil
	vm.releaseThread(t)
}

func (vm *VM) releaseThread(t *thread) {
	var zeros []int
	for i := len(t.frames) - 1; i >= 0; i-- {
		fid := t.frames[i].id
		vm.counts[fid]--
		if vm.counts[fid] == 0 {
			zeros = append(zeros, fid)
		}
	}
	vm.resolveZeros(zeros)
}

// resolveZeros spawns continuations for exhausted negative lookarounds,
// deepest frame first, re-checking counts as continuations may revive
// parent frames.
func (vm *VM) resolveZeros(zeros []int) {
	for _, fid := range zeros {
		if vm.counts[fid] != 0 {
			continue
		}
		f := vm.findFrame(fid)
		if f == nil || !f.neg {
			continue
		}
		// Negative assertion succeeded: nobody inside matched.
		cont := &thread{
			id:     vm.nextThread,
			pc:     f.retPC,
			pos:    f.startPos,
			caps:   slices.Clone(f.savedCaps),
			marks:  maps.Clone(f.savedMarks),
			frames: slices.Clone(f.parentFrames),
		}
		vm.nextThread++
		vm.states++
		vm.snapshots++
		for _, pf := range cont.frames {
			vm.counts[pf.id]++
		}
		vm.emit(EvLookOut, cont, "negative lookaround satisfied")
		vm.emit(EvSpawn, cont, "lookaround continuation")
		vm.stack = append(vm.stack, cont)
	}
}

// findFrame locates a frame instance by id among live threads. Used only
// when a count reaches zero, at which point the frame's defining data must
// be recovered; we keep a registry instead.
func (vm *VM) findFrame(fid int) *frame {
	if f, ok := vm.frameReg[fid]; ok {
		return f
	}
	return nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
