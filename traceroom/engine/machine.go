package engine

import (
	"unicode/utf8"

	"traceroom/compile"
	"traceroom/syntax"
)

// stepResult tells the driver what happened executing one instruction.
type stepResult uint8

const (
	srContinue stepResult = iota
	srMatch
	srFail
	srPause  // breakpoint
	srBudget // a budget was exhausted
)

// runner is the execution driver shared by the main machine and lookaround
// sub-machines. vm.go owns config/budget; machine.go owns instruction logic.
type runner struct {
	prog  *compile.Program
	input *Input
	cfg   Config

	steps     int
	snapshots int

	events    []Event
	maxEvents int

	pausedAt int // pc that triggered a breakpoint (-1 none)
	reason   LimitReason

	depth int // lookaround nesting depth for events
}

type machine struct {
	r      *runner
	prog   *compile.Program
	pc     int
	pos    int
	caps   []int
	loops  []LoopState
	stack  []Frame
	active *LookFrame
}

func (r *runner) newMachine(prog *compile.Program, pos int, caps []int) *machine {
	m := &machine{
		r:     r,
		prog:  prog,
		pc:    0,
		pos:   pos,
		caps:  caps,
		loops: make([]LoopState, 0, 4),
	}
	return m
}

func (m *machine) loop(id int) *LoopState {
	for len(m.loops) <= id {
		m.loops = append(m.loops, LoopState{Anchor: -1})
	}
	return &m.loops[id]
}

func (m *machine) push(f Frame) { m.stack = append(m.stack, f) }
func (m *machine) pop() Frame {
	f := m.stack[len(m.stack)-1]
	m.stack = m.stack[:len(m.stack)-1]
	return f
}

// snapshotCaps copies capture slots; charged against the snapshot budget.
func (m *machine) snapshotCaps() ([]int, stepResult) {
	if !m.r.chargeSnapshot() {
		return nil, srBudget
	}
	cp := make([]int, len(m.caps))
	copy(cp, m.caps)
	return cp, srContinue
}

func (m *machine) emit(k EventKind, detail map[string]any) {
	ev := Event{Seq: len(m.r.events), Kind: k, Depth: m.r.depth, Pos: m.r.input.PosOf(m.pos), PC: m.pc, Detail: detail}
	m.r.events = append(m.r.events, ev)
}

// fail pops the DFS stack until a viable alternative is restored.
func (m *machine) fail(reason string) stepResult {
	for len(m.stack) > 0 {
		f := m.pop()
		switch f.Kind {
		case FrameAlt:
			m.emit(EvBacktrack, map[string]any{"toPc": f.PC, "reason": reason})
			m.pc = f.PC
			m.pos = f.Pos
			if f.Caps != nil {
				m.r.rollbackCaps(m, f.Caps, "backtrack")
			}
			return srContinue
		case FrameCutMarker:
			// leaving atomic region through failure: continue popping
			continue
		case FrameCutSuccess:
			// surviving path arriving here performs the cut; on failure discard
			continue
		case FrameDie:
			continue
		case FrameBehind, FrameBehindCont:
			// lookaround sub evaluation died without producing a continuation
			continue
		}
	}
	return srFail
}

// step executes one micro-step: one sub instruction while inside a look, or
// one outer instruction otherwise.
func (m *machine) step() stepResult {
	if m.active != nil {
		return m.stepLook()
	}
	return m.execInstr()
}

// execInstr charges budget and executes exactly one instruction.
func (m *machine) execInstr() stepResult {
	if !m.r.chargeStep() {
		return srBudget
	}
	if m.pc >= len(m.prog.Code) {
		return m.fail("pc-out-of-range")
	}
	in := m.prog.Code[m.pc]

	// breakpoint (outer machine only)
	if m.r.depth == 0 && m.r.atBreakpoint(in.Lo, in.Hi) {
		m.r.pausedAt = m.pc
		m.emit(EvPause, map[string]any{"srcLo": in.Lo, "srcHi": in.Hi})
		return srPause
	}

	switch in.Op {
	case compile.OpChar:
		r, sz := m.input().DecodeRune(m.pos)
		if sz == 0 || !runeEq(r, rune(in.Arg), m.prog.Flags&syntax.FlagI != 0) {
			return m.fail("char")
		}
		old := m.pos
		m.pos += sz
		m.emitConsume(old, "char")
		m.pc++
	case compile.OpAny:
		r, sz := m.input().DecodeRune(m.pos)
		if sz == 0 {
			return m.fail("any-eof")
		}
		if r == '\n' && m.prog.Flags&syntax.FlagS == 0 {
			return m.fail("any-newline")
		}
		old := m.pos
		m.pos += sz
		m.emitConsume(old, "any")
		m.pc++
	case compile.OpClass:
		r, sz := m.input().DecodeRune(m.pos)
		if sz == 0 || !matchClass(m.prog.Classes[in.Arg], r) {
			return m.fail("class")
		}
		old := m.pos
		m.pos += sz
		m.emitConsume(old, "class")
		m.pc++
	case compile.OpAnchor:
		if !matchAnchor(syntax.AnchorKind(in.Arg), m.input(), m.pos, m.prog.Flags) {
			return m.fail("anchor")
		}
		m.pc++
	case compile.OpJmp:
		m.pc = in.Arg
	case compile.OpSave:
		prev := m.caps[in.Arg]
		m.caps[in.Arg] = m.pos
		m.r.noteCap(in.Arg, m.pos, prev)
		m.pc++
	case compile.OpSplit:
		res := m.pushSplit(in)
		if res != srContinue {
			return res
		}
	case compile.OpLoopStart:
		ls := m.loop(in.Arg)
		ls.Anchor = m.pos
		m.pc++
	case compile.OpLoopGuard:
		ls := m.loop(in.Arg)
		if m.pos == ls.Anchor {
			m.emit(EvLoopZero, map[string]any{"loop": in.Arg})
			return m.fail("zero-width")
		}
		// body advanced; re-anchor for the next iteration
		ls.Anchor = m.pos
		m.pc++
	case compile.OpOpenCut:
		m.push(Frame{Kind: FrameCutMarker, LoopID: in.Arg})
		m.pc++
	case compile.OpCloseCut:
		m.push(Frame{Kind: FrameCutSuccess, LoopID: in.Arg, PC: m.pc + 1, Pos: m.pos})
		m.pc++
		return m.maybeCut(in.Arg)
	case compile.OpBackref:
		res := m.execBackref(in)
		if res != srContinue {
			return res
		}
	case compile.OpLook:
		return m.enterLook(in)
	case compile.OpMatch:
		return srMatch
	case compile.OpSubEnd:
		// end of a lookaround subprogram: success of the sub evaluation
		return srMatch
	}
	return srContinue
}

// pushSplit pushes the secondary branch then proceeds on the primary.
// Lazy splits (Arg2==1) swap priorities: secondary is taken immediately and
// the fall-through is parked.
func (m *machine) pushSplit(in compile.Instr) stepResult {
	primaryPC := m.pc + 1
	secondaryPC := in.Arg
	takePC, parkPC := primaryPC, secondaryPC
	if in.Arg2 == 1 {
		takePC, parkPC = secondaryPC, primaryPC
	}
	caps, res := m.snapshotCaps()
	if res != srContinue {
		return res
	}
	m.push(Frame{Kind: FrameAlt, PC: parkPC, Pos: m.pos, Caps: caps})
	m.emit(EvBranchPush, map[string]any{"parkPc": parkPC, "takePc": takePC, "lazy": in.Arg2 == 1})
	m.pc = takePC
	return srContinue
}

// maybeCut runs when the active path reached OpCloseCut: discard all parked
// alternatives up to and including the matching OpenCut marker.
func (m *machine) maybeCut(cutID int) stepResult {
	// top is the CutSuccess we just pushed; pop it and find marker
	m.pop()
	for len(m.stack) > 0 {
		top := m.stack[len(m.stack)-1]
		if top.Kind == FrameCutMarker && top.LoopID == cutID {
			m.stack = m.stack[:len(m.stack)-1]
			break
		}
		m.stack = m.stack[:len(m.stack)-1]
	}
	return srContinue
}

func (m *machine) emitConsume(old int, kind string) {
	np := m.r.input.PosOf(m.pos)
	m.r.events = append(m.r.events, Event{
		Seq: len(m.r.events), Kind: EvConsume, Depth: m.r.depth,
		Pos: m.r.input.PosOf(old), NextPos: &np, Detail: map[string]any{"how": kind},
	})
}

func (m *machine) input() *Input { return m.r.input }

// execBackref matches the previously captured text of a group.
func (m *machine) execBackref(in compile.Instr) stepResult {
	id := in.Arg
	s, e := m.caps[2*id], m.caps[2*id+1]
	if s < 0 || e < 0 || s > e {
		// unmatched group: backreference matches empty string
		m.pc++
		return srContinue
	}
	want := m.r.input.Bytes[s:e]
	if m.pos+len(want) > len(m.r.input.Bytes) {
		return m.fail("backref-eof")
	}
	got := m.r.input.Bytes[m.pos : m.pos+len(want)]
	ci := m.prog.Flags&syntax.FlagI != 0
	if ci {
		// compare rune-wise with folding
		si, pi := 0, m.pos
		for si < len(want) {
			r1, z1 := utf8.DecodeRune(want[si:])
			r2, z2 := m.r.input.DecodeRune(pi)
			if z2 == 0 || !runeEq(r1, r2, true) {
				return m.fail("backref-ci")
			}
			si += z1
			pi += z2
		}
	} else if string(got) != string(want) {
		return m.fail("backref")
	}
	old := m.pos
	m.pos += len(want)
	m.emitConsume(old, "backref")
	m.pc++
	return srContinue
}
