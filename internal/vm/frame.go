package vm

// TermKinds are success-continuation frames on the T stack.
const (
	TermNode         = "node"          // evaluate node NodeID
	TermGroupEnd     = "group_end"     // close capture group NodeID/GroupIndex
	TermLoop         = "loop"          // quantifier continuation
	TermAtomicCommit = "atomic_commit" // trim alternatives behind an atomic barrier
)

// Term is one success-continuation frame. Concrete payload lives in fields;
// the shape is JSON-friendly so a halted machine (frontier) can be persisted
// and resumed later.
type Term struct {
	Kind   string `json:"kind"`
	NodeID int    `json:"node_id"`

	// loop bookkeeping
	Count    int `json:"count,omitempty"`
	StartPos int `json:"start_pos,omitempty"`
	// group_end
	GroupIndex int `json:"group_index,omitempty"`
}

// AltKinds are failure/alternative handlers on the A stack. Order on the
// stack defines documented branch priority (documented order first).
const (
	AltBranch    = "branch"     // next alternation branch
	AltLoopStop  = "loop_stop"  // greedy: stop repeating here
	AltLoopRetry = "loop_retry" // lazy: try one more repetition
	AltMandatory = "mandatory"  // a required repetition failed -> propagate
	AltBarrier   = "barrier"    // atomic group / possessive cut point
	AltNextStart = "next_start" // root search: try the next start position
	AltLookDone  = "look_done"  // complete a finished lookaround sub-level
	AltGroupUndo = "group_undo" // restore captures when a required group fails
)

// Alt is one failure-continuation frame.
type Alt struct {
	Kind   string `json:"kind"`
	NodeID int    `json:"node_id"`

	// branch index for AltBranch; repetition Count for loop frames
	Branch int `json:"branch,omitempty"`
	Count  int `json:"count,omitempty"`
	Group  int `json:"group,omitempty"`

	// T-height to truncate the term stack to before taking this alternative
	Height int `json:"height"`
	// execution coordinates to restore (backtracking)
	Pos  int      `json:"pos"`
	Snap Snapshot `json:"snap"`
	// IterPos is the position before that iteration's child ran (for loop
	// frames), used to detect and forbid zero-width re-expansion.
	IterPos int `json:"iter_pos,omitempty"`
	// PrevSnap is the capture state before the construct mutated it
	// (used by group undo to restore exact prior participation).
	PrevSnap *Snapshot `json:"prev_snap,omitempty"`

	// barrier linkage (atomic/possessive)
	Barrier int `json:"barrier,omitempty"`

	// root search: next start position
	NextStart int `json:"next_start,omitempty"`

	// lookaround completion
	Look *AltLook `json:"look,omitempty"`
}

// AltLook is the payload of an AltLookDone frame.
type AltLook struct {
	Ahead      bool     `json:"ahead"`
	Positive   bool     `json:"positive"`
	Anchor     int      `json:"anchor"`
	Start      int      `json:"start"`
	Width      int      `json:"width"`
	ParentPos  int      `json:"parent_pos"`
	ParentSnap Snapshot `json:"parent_snap"`
	ParentTerm Term     `json:"parent_term"`
}

// LookState describes an in-flight lookaround level.
type LookState struct {
	NodeID   int  `json:"node_id"`
	Ahead    bool `json:"ahead"`
	Positive bool `json:"positive"`
	// Anchor is the parent's byte position; lookbehind must finish exactly here.
	Anchor int `json:"anchor"`
	// Start is where the subpattern began scanning.
	Start int `json:"start"`
}

// Level is one nested execution level (root or a lookaround).
type Level struct {
	Name string     `json:"name"` // "root" or "look#<nodeid>"
	T    []Term     `json:"t"`
	A    []Alt      `json:"a"`
	Pos  int        `json:"pos"`
	Snap Snapshot   `json:"snap"`
	Look *LookState `json:"look,omitempty"`
	// Completion is set on non-root levels: how to resume the parent.
	Completion *AltLook `json:"completion,omitempty"`
	// Parent index into the machine levels slice (stable while nested).
	Parent int `json:"parent,omitempty"`
	// Start is the current root search start position.
	Start int `json:"start,omitempty"`

	// barrier counter local to this level
	nextBarrier int `json:"-"`
}

func (l *Level) pushT(ts ...Term) { l.T = append(l.T, ts...) }
func (l *Level) popT() Term {
	top := l.T[len(l.T)-1]
	l.T = l.T[:len(l.T)-1]
	return top
}

// newBarrierID hands out an atomic/possessive barrier id.
func (l *Level) newBarrierID() int {
	l.nextBarrier++
	return l.nextBarrier
}

// trimA cuts the alternative stack down to (exclusive of) the barrier id.
// Returns the index of the barrier, or -1 if absent.
func (l *Level) barrierIndex(id int) int {
	for i := len(l.A) - 1; i >= 0; i-- {
		if l.A[i].Kind == AltBarrier && l.A[i].Barrier == id {
			return i
		}
	}
	return -1
}
