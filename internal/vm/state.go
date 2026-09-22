package vm

// CapSlot is one capture interval in rune offsets.
type CapSlot struct {
	Begin int `json:"begin"` // -1 when unset
	End   int `json:"end"`
}

func unsetSlot() CapSlot { return CapSlot{Begin: -1, End: -1} }

// Captures is the per-thread capture vector; slot 0 is the whole match.
type Captures []CapSlot

func newCaptures(n int) Captures {
	cs := make(Captures, n)
	for i := range cs {
		cs[i] = unsetSlot()
	}
	return cs
}

// Clone copies the capture vector (counts against the snapshot budget).
func (c Captures) Clone() Captures {
	out := make(Captures, len(c))
	copy(out, c)
	return out
}

// FrameKind tags a continuation-stack frame.
type FrameKind uint8

const (
	FrameChoice FrameKind = iota // OpSplit branch point
	FrameRepeat                  // mutable repeat counter
	FrameLook                    // lookaround boundary
)

// Frame is one serialisable continuation-stack element.
type Frame struct {
	Kind    FrameKind `json:"kind"`
	Pc      int       `json:"pc"`                // resume instruction
	Pos     int       `json:"pos"`               // resume rune position
	LoopID  int       `json:"loopId"`            // FrameChoice: tagged loop split, else -1
	Count   int       `json:"count"`             // FrameRepeat: iterations so far
	LoopPos int       `json:"loopPos,omitempty"` // FrameRepeat: pos at current iteration entry
	Min     int       `json:"min,omitempty"`
	Max     int       `json:"max,omitempty"` // -1 unbounded
	LookID  int       `json:"lookId"`        // FrameLook
	Neg     bool      `json:"neg"`
	Behind  bool      `json:"behind"`
	Length  int       `json:"length"` // lookbehind candidate length tried
	MaxLen  int       `json:"maxLen"`
	EndPc   int       `json:"endPc,omitempty"` // instruction after LookEnd
	Caps    Captures  `json:"caps,omitempty"`  // FrameLook snapshot at assertion entry
	Tag     string    `json:"tag,omitempty"`
}

// Thread is one active execution state.
type Thread struct {
	Pc    int      `json:"pc"`
	Pos   int      `json:"pos"`
	Stack []Frame  `json:"stack"`
	Caps  Captures `json:"caps"`
}

func (t *Thread) push(f Frame) {
	t.Stack = append(t.Stack, f)
}

func (t *Thread) pop() Frame {
	f := t.Stack[len(t.Stack)-1]
	t.Stack = t.Stack[:len(t.Stack)-1]
	return f
}

// SnapshotCap records that the caps vector has been replaced by a copy.
func (t *Thread) replaceCaps(newCaps Captures) {
	t.Caps = newCaps
}

// Budgets bound the resources one Run may consume.
type Budgets struct {
	MaxSteps     int `json:"maxSteps"`     // executed instructions
	MaxStates    int `json:"maxStates"`    // pushed alternative states (choice points)
	MaxSnapshots int `json:"maxSnapshots"` // capture-vector copies
}

// DefaultBudgets are deliberately tight but usable for the demo.
func DefaultBudgets() Budgets {
	return Budgets{MaxSteps: 200000, MaxStates: 50000, MaxSnapshots: 60000}
}
