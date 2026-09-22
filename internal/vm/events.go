package vm

// EventKind enumerates the observable trace events emitted while stepping.
// Equivalence testing compares the sequence of these kinds with associated
// coordinates; it ignores volatile thread/snapshot identifiers.
type EventKind string

const (
	EvStart           EventKind = "start"
	EvNodeEnter       EventKind = "node_enter"
	EvCharTest        EventKind = "char_test"
	EvCharMatch       EventKind = "char_match"
	EvCharFail        EventKind = "char_fail"
	EvAnchorTest      EventKind = "anchor_test"
	EvAnchorMatch     EventKind = "anchor_match"
	EvAnchorFail      EventKind = "anchor_fail"
	EvBranchTry       EventKind = "branch_try"
	EvBranchPruned    EventKind = "branch_pruned"
	EvCaptureOpen     EventKind = "capture_open"
	EvCaptureClose    EventKind = "capture_close"
	EvCaptureRollback EventKind = "capture_rollback"
	EvRepeatEnter     EventKind = "repeat_enter"
	EvRepeatZeroBlock EventKind = "repeat_zero_block"
	EvLookEnter       EventKind = "look_enter"
	EvLookStart       EventKind = "look_start"
	EvLookSuccess     EventKind = "look_success"
	EvLookFail        EventKind = "look_fail"
	EvBacktrack       EventKind = "backtrack"
	EvStartAdvance    EventKind = "start_advance"
	EvMatch           EventKind = "match"
	EvNoMatch         EventKind = "no_match"
	EvIndeterminate   EventKind = "indeterminate"
	EvBudgetExceeded  EventKind = "budget_exceeded"
)

// Event is one observable step. Capture fields describe the update or the
// restored snapshot so the UI can show capture history precisely.
type Event struct {
	Seq       int        `json:"seq"`
	Kind      EventKind  `json:"kind"`
	NodeID    int        `json:"node_id,omitempty"`
	BytePos   int        `json:"byte_pos,omitempty"`
	RunePos   int        `json:"rune_pos,omitempty"`
	FromPos   int        `json:"from_pos,omitempty"`
	ToPos     int        `json:"to_pos,omitempty"`
	Detail    string     `json:"detail,omitempty"`
	Group     int        `json:"group,omitempty"`
	SnapID    int        `json:"snap_id,omitempty"`
	Level     string     `json:"level,omitempty"`
	Snapshots []Group    `json:"groups,omitempty"`
	Budget    *BudgetUse `json:"budget,omitempty"`
	Frontier  *Frontier  `json:"frontier,omitempty"`
}

// Budget caps the search. Limits <= 0 mean unlimited for that dimension.
type Budget struct {
	MaxSteps     int `json:"max_steps"`
	MaxStates    int `json:"max_states"`
	MaxSnapshots int `json:"max_snapshots"`
}

// DefaultBudget is used when no explicit limits are supplied.
func DefaultBudget() Budget {
	return Budget{MaxSteps: 200000, MaxStates: 200000, MaxSnapshots: 50000}
}

// BudgetUse reports consumption and remaining headroom.
type BudgetUse struct {
	Steps     int `json:"steps"`
	States    int `json:"states"`
	Snapshots int `json:"snapshots"`
	MaxSteps  int `json:"max_steps"`
	MaxStates int `json:"max_states"`
	MaxSnaps  int `json:"max_snapshots"`
}

func (b BudgetUse) exhaustedSteps() bool  { return b.MaxSteps > 0 && b.Steps >= b.MaxSteps }
func (b BudgetUse) exhaustedStates() bool { return b.MaxStates > 0 && b.States >= b.MaxStates }
