package regex

import (
	"unicode/utf8"
)

// Budget bounds the resource consumption of one execution. Exceeding any
// bound pauses the search as Indeterminate with a resumable frontier; it never
// reports a false no-match.
type Budget struct {
	MaxSteps     int `json:"max_steps"`
	MaxStates    int `json:"max_states"`
	MaxSnapshots int `json:"max_snapshots"`
	MaxEvents    int `json:"max_events"`
}

// DefaultBudget is the resource envelope used by the API/UI.
func DefaultBudget() Budget {
	return Budget{MaxSteps: 200_000, MaxStates: 50_000, MaxSnapshots: 50_000, MaxEvents: 20_000}
}

// Breakpoint pauses before executing the AST node at the given rune index.
type Breakpoint struct {
	NodeID  int // 0 = any node
	RunePos int // -1 = any position
}

// EventKind enumerates recorded trace events.
type EventKind string

const (
	EvStep          EventKind = "step"            // ordinary instruction
	EvBranch        EventKind = "branch"          // split chose a continuation
	EvPrune         EventKind = "prune"           // a continuation was dropped because a budget was hit
	EvFail          EventKind = "fail"            // continuation died
	EvCapture       EventKind = "capture"         // a save slot changed
	EvTry           EventKind = "try"             // search attempt started at an input position
	EvZeroStop      EventKind = "zero_width_stop" // guard halted an empty repetition
	EvMatch         EventKind = "match"           // overall success
	EvNoMatch       EventKind = "no_match"        // search exhausted all positions
	EvPause         EventKind = "pause"           // breakpoint or step budget pause
	EvIndeterminate EventKind = "indeterminate"
)

// Event is one entry in the capture trace room.
type Event struct {
	Seq      int             `json:"seq"`
	Kind     EventKind       `json:"kind"`
	RunePos  int             `json:"rune_pos"`
	BytePos  int             `json:"byte_pos"`
	NodeID   int             `json:"node_id"`
	Op       Opcode          `json:"op,omitempty"`
	IP       int             `json:"ip,omitempty"`
	Detail   string          `json:"detail,omitempty"`
	Slot     int             `json:"slot,omitempty"`
	OldValue *Span           `json:"old_value,omitempty"`
	NewValue *Span           `json:"new_value,omitempty"`
	Active   int             `json:"active_states,omitempty"`
	Pruned   string          `json:"pruned_reason,omitempty"`
	Captures map[string]Span `json:"captures,omitempty"`
}

// Span is a captured byte/rune interval.
type Span struct {
	ByteLo  int  `json:"byte_lo"`
	ByteHi  int  `json:"byte_hi"`
	RuneLo  int  `json:"rune_lo"`
	RuneHi  int  `json:"rune_hi"`
	Matched bool `json:"matched"`
}

// Status of a run.
type Status string

const (
	StatusMatch         Status = "match"
	StatusNoMatch       Status = "no_match"
	StatusIndeterminate Status = "indeterminate"
	StatusPaused        Status = "paused"
)

// LookFrame marks an active lookaround evaluation.
type LookFrame struct {
	ResumeIP  int  `json:"resume_ip"`  // instruction after LookEnd on success path
	StartByte int  `json:"start_byte"` // assertion input position
	OuterByte int  `json:"outer_byte"` // outer position (lookbehind)
	Negative  bool `json:"negative"`
	Behind    bool `json:"behind"`
	Depth     int  `json:"depth"`
}

// Thread is one backtracking state (fully independent capture snapshot).
type Thread struct {
	IP      int         `json:"ip"`
	BytePos int         `json:"byte_pos"`
	Slots   []int       `json:"slots"`  // -1 unset; pairs are open/close byte offsets
	Guards  []int       `json:"guards"` // byte position at guard entry; -1 unused
	Looks   []LookFrame `json:"looks"`
}

// Frontier is the resumable search state.
type Frontier struct {
	Stack       []Thread `json:"stack"`
	TryBytePos  int      `json:"try_byte_pos"`
	Steps       int      `json:"steps"`
	States      int      `json:"states"`
	Snapshots   int      `json:"snapshots"`
	Seq         int      `json:"event_seq"`
	NextRunePos int      `json:"next_rune_pos"`
}

// Result is the outcome of a search.
type Result struct {
	Status          Status          `json:"status"`
	SyntaxVersion   string          `json:"syntax_version"`
	UnicodeVersion  string          `json:"unicode_version"`
	Match           *Span           `json:"match,omitempty"`
	Captures        []Span          `json:"captures,omitempty"`
	Named           map[string]Span `json:"named,omitempty"`
	Events          []Event         `json:"events,omitempty"`
	Frontier        *Frontier       `json:"frontier,omitempty"`
	Budget          Budget          `json:"budget"`
	BudgetUsed      BudgetCounters  `json:"budget_used"`
	EventsTruncated bool            `json:"events_truncated,omitempty"`
	Reason          string          `json:"reason,omitempty"`
}

// BudgetCounters reports how much of the envelope was consumed.
type BudgetCounters struct {
	Steps     int `json:"steps"`
	States    int `json:"states"`
	Snapshots int `json:"snapshots"`
}

// Options controls a search.
type Options struct {
	Budget      Budget
	Breakpoints []Breakpoint
	StepLimit   int // pause after this many newly executed steps (single-step)
	Anchored    bool
}

// Search runs the compiled program over input and returns the first match.
func Search(prog *Program, input []byte, opts Options) *Result {
	eng := newEngine(prog, input, opts)
	return eng.run()
}

// Resume continues a paused/indeterminate run from its frontier.
func Resume(prog *Program, input []byte, f *Frontier, opts Options) *Result {
	eng := newEngine(prog, input, opts)
	eng.stack = f.Stack
	eng.tryByte = f.TryBytePos
	eng.steps = f.Steps
	eng.states = f.States
	eng.snapshots = f.Snapshots
	eng.seq = f.Seq
	eng.runeAtAttempt = f.NextRunePos
	return eng.run()
}

type engine struct {
	prog  *Program
	input []byte
	opts  Options

	stack         []Thread
	tryByte       int
	runeAtAttempt int

	steps, states, snapshots int
	events                   []Event
	eventsTruncated          bool
	seq                      int

	// rune index tracking: runeIndex[k] = rune index of byte k start.
	runeIndex []int
}

func newEngine(prog *Program, input []byte, opts Options) *engine {
	if opts.Budget.MaxSteps == 0 {
		opts.Budget = DefaultBudget()
	}
	slots := make([]int, prog.NumGroups*2)
	for i := range slots {
		slots[i] = -1
	}
	guards := make([]int, prog.NumGuards)
	for i := range guards {
		guards[i] = -1
	}
	e := &engine{
		prog: prog, input: input, opts: opts,
		stack:     []Thread{{IP: 0, BytePos: 0, Slots: slots, Guards: guards}},
		tryByte:   0,
		runeIndex: buildRuneIndex(input),
	}
	e.record(EvTry, 0, 0, 0, "attempt at rune 0")
	e.runeAtAttempt = 0
	return e
}

func buildRuneIndex(input []byte) []int {
	idx := make([]int, len(input)+1)
	n := 0
	for i := 0; i <= len(input); i++ {
		idx[i] = n
		if i < len(input) && utf8.RuneStart(input[i]) {
			n++
		}
	}
	return idx
}

func (e *engine) runePos(bytePos int) int {
	if bytePos < 0 {
		return -1
	}
	if bytePos > len(e.input) {
		return -1
	}
	return e.runeIndex[bytePos]
}

// byteAtRune returns the byte offset of the 0-based code point index.
func (e *engine) byteAtRune(rp int) int {
	lo, hi := 0, len(e.runeIndex)-1
	for lo < hi {
		mid := (lo + hi) / 2
		if e.runeIndex[mid] < rp {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

func (e *engine) run() *Result {
	stepBudget := e.opts.StepLimit
	for len(e.stack) > 0 {
		t := e.pop()
		status := e.exec(&t, &stepBudget)
		switch status {
		case execMatched:
			return e.finishMatch(t)
		case execPaused:
			// Thread has not consumed the breakpoint step; retain it on top.
			e.stack = append(e.stack, t)
			return e.finish(StatusPaused, "breakpoint or step limit", true)
		case execBudget:
			e.stack = append(e.stack, t)
			return e.finish(StatusIndeterminate, "resource budget exhausted; frontier retained", true)
		case execFailed:
			if len(t.Looks) > 0 {
				// Dying inside a negative lookaround means its body failed:
				// the assertion succeeds. Pop to the nearest negative frame
				// (nested positive frames are abandoned) and continue after it.
				for len(t.Looks) > 0 {
					top := t.Looks[len(t.Looks)-1]
					t.Looks = t.Looks[:len(t.Looks)-1]
					if top.Negative {
						if top.Behind {
							t.BytePos = top.OuterByte
						} else {
							t.BytePos = top.StartByte
						}
						// ResumeIP for the negative-split continuation is the
						// instruction after the assertion (cont). top.StartByte
						// is the anchor position, which for lookahead equals the
						// outer position.
						t.IP = top.ResumeIP
						status = e.exec(&t, &stepBudget)
						if status == execMatched {
							return e.finishMatch(t)
						}
						if status == execPaused {
							e.stack = append(e.stack, t)
							return e.finish(StatusPaused, "breakpoint or step limit", true)
						}
						if status == execBudget {
							e.stack = append(e.stack, t)
							return e.finish(StatusIndeterminate, "resource budget exhausted; frontier retained", true)
						}
						break
					}
				}
			}
			if status == execFailed && len(e.stack) == 0 {
				if !e.nextAttempt() {
					return e.finish(StatusNoMatch, "", false)
				}
			}
		}
	}
	return e.finish(StatusNoMatch, "", false)
}

type execStatus int

const (
	execCont execStatus = iota
	execMatched
	execFailed
	execPaused
	execBudget
)

func (e *engine) pop() Thread {
	t := e.stack[len(e.stack)-1]
	e.stack = e.stack[:len(e.stack)-1]
	return t
}

// exec runs a thread until it dies, succeeds, pauses, or exceeds budget.
func (e *engine) exec(t *Thread, stepBudget *int) execStatus {
	for {
		if t.IP >= len(e.prog.Instr) {
			e.recordAt(t, EvFail, "ran past program end")
			return execFailed
		}
		in := e.prog.Instr[t.IP]
		// Breakpoints only fire on non-bookkeeping AST instructions.
		if in.NodeID != 0 && e.hitBreakpoint(in.NodeID, t.BytePos) {
			e.recordAt(t, EvPause, "breakpoint before node")
			return execPaused
		}
		if *stepBudget > 0 {
			*stepBudget--
			if *stepBudget == 0 {
				e.recordAt(t, EvPause, "step limit reached")
				return execPaused
			}
		}
		e.steps++
		if e.steps > e.opts.Budget.MaxSteps {
			e.recordAt(t, EvPrune, "step budget exceeded")
			return execBudget
		}
		e.recordStep(t, in)
		t.IP++
		switch in.Op {
		case OpStartTry:
			// no-op marker; search attempts always begin here
		case OpChar:
			r, w, invalid := decodeAt(e.input, t.BytePos)
			if w == 0 || !in.Class.match(r, invalid) {
				e.recordAt(t, EvFail, "char/class mismatch")
				return execFailed
			}
			t.BytePos += w
		case OpAny:
			r, w, invalid := decodeAt(e.input, t.BytePos)
			if w == 0 || invalid || (!in.DotAll && isNewline(r)) {
				e.recordAt(t, EvFail, "dot mismatch")
				return execFailed
			}
			t.BytePos += w
		case OpAnchor:
			if !e.checkAnchor(in, t.BytePos, t) {
				e.recordAt(t, EvFail, "anchor "+in.Anchor+" failed")
				return execFailed
			}
		case OpJmp:
			t.IP = in.A
		case OpSplit:
			var st execStatus
			*t, st = e.doSplit(*t, in)
			if st != execCont {
				return st
			}
		case OpSave:
			e.doSave(t, in)
		case OpBackref:
			if !e.doBackref(t, in) {
				return execFailed
			}
		case OpLookAhead, OpLookBehind:
			st := e.doLookEnter(t, in)
			if st != execCont {
				return st
			}
		case OpLookEnd:
			st := e.doLookEnd(t, in)
			if st != execCont {
				return st
			}
		case OpGuardEntry:
			if len(t.Guards) <= in.A {
				g := make([]int, in.A+1)
				for i := range g {
					g[i] = -1
				}
				t.Guards = append(t.Guards, g...)
			}
			t.Guards[in.A] = t.BytePos
		case OpGuardCheck:
			if t.Guards[in.A] == t.BytePos {
				e.recordAt(t, EvZeroStop, "zero-width iteration terminated")
				return execFailed
			}
			t.Guards[in.A] = -1
		case OpMatch:
			if len(t.Looks) > 0 {
				// Body of a positive lookaround ended successfully without an
				// explicit LookEnd (defensive; compiler emits LookEnd normally).
				top := t.Looks[len(t.Looks)-1]
				if top.Negative {
					return execFailed
				}
				t.Looks = t.Looks[:len(t.Looks)-1]
				if top.Behind {
					t.BytePos = top.StartByte
				}
				t.IP = top.ResumeIP
				continue
			}
			return execMatched
		}
	}
}

func isNewline(r rune) bool {
	switch r {
	case '\n', '\r', 0x85, 0x2028, 0x2029:
		return true
	}
	return false
}

// nextAttempt starts a fresh search attempt at the next lead byte.
func (e *engine) nextAttempt() bool {
	if e.opts.Anchored {
		return false
	}
	next := e.tryByte + 1
	// Skip continuation bytes so every attempt sits on a code point boundary.
	for next < len(e.input) && !utf8.RuneStart(e.input[next]) {
		next++
	}
	if next > len(e.input) {
		return false
	}
	e.tryByte = next
	slots := make([]int, e.prog.NumGroups*2)
	for i := range slots {
		slots[i] = -1
	}
	guards := make([]int, e.prog.NumGuards)
	for i := range guards {
		guards[i] = -1
	}
	e.stack = []Thread{{IP: 0, BytePos: next, Slots: slots, Guards: guards}}
	rp := e.runePos(next)
	e.runeAtAttempt = rp
	e.record(EvTry, next, rp, 0, "attempt at rune boundary")
	return true
}

func (e *engine) hitBreakpoint(nodeID, bytePos int) bool {
	if len(e.opts.Breakpoints) == 0 {
		return false
	}
	rp := e.runePos(bytePos)
	for _, b := range e.opts.Breakpoints {
		if b.NodeID != 0 && b.NodeID != nodeID {
			continue
		}
		if b.RunePos >= 0 && b.RunePos != rp {
			continue
		}
		return true
	}
	return false
}
