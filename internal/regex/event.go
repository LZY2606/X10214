package regex

// EventKind enumerates recorded trace events.
type EventKind string

const (
	EvStep      EventKind = "step"       // ordinary instruction executed
	EvChoose    EventKind = "choose"     // split branch selected
	EvBacktrack EventKind = "backtrack"  // a branch failed; restored a frame
	EvCapUpdate EventKind = "cap-update" // capture positions changed
	EvPrune     EventKind = "prune"      // atomic group discarded alternatives
	EvLook      EventKind = "look"       // lookaround boundary
	EvBudget    EventKind = "budget"     // a budget counter advanced
	EvHalt      EventKind = "halt"       // match / fail / indeterminate
)

// Event is one entry in the capture trace.
type Event struct {
	Seq      int            `json:"seq"`
	Kind     EventKind      `json:"kind"`
	PC       int            `json:"pc"`
	Pos      int            `json:"pos"`       // element position
	BytePos  int            `json:"bytePos"`   // UTF-8 byte offset
	CpPos    int            `json:"cpPos"`     // code point offset
	Label    string         `json:"label,omitempty"`
	Detail   string         `json:"detail,omitempty"`
	Branch   string         `json:"branch,omitempty"`
	Caps     map[string]Cap `json:"caps,omitempty"`
	Deltas   []CapDelta     `json:"deltas,omitempty"`
	Reason   string         `json:"reason,omitempty"`
}

// Cap is one capture at trace time.
type Cap struct {
	Group int    `json:"group"`
	Name  string `json:"name,omitempty"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	StartByte int `json:"startByte"`
	EndByte   int `json:"endByte"`
	StartCp   int `json:"startCp"`
	EndCp     int `json:"endCp"`
	Matched   bool `json:"matched"`
}

// CapDelta shows a single capture change.
type CapDelta struct {
	Group int      `json:"group"`
	Name   string   `json:"name,omitempty"`
	Before *[2]int  `json:"before,omitempty"`
	After  *[2]int  `json:"after,omitempty"`
}

// Outcome is the terminal status of a run.
type Outcome string

const (
	OutMatch        Outcome = "match"
	OutNoMatch      Outcome = "no-match"
	OutIndeterminate Outcome = "indeterminate"
)

// BudgetUse reports resource consumption.
type BudgetUse struct {
	Steps       int `json:"steps"`
	MaxStates   int `json:"maxStates"`
	Snapshots   int `json:"snapshots"`
	StepBudget  int `json:"stepBudget"`
	StateBudget int `json:"stateBudget"`
	SnapBudget  int `json:"snapBudget"`
}

// Budgets are execution limits; zero means unlimited for that resource.
type Budgets struct {
	Steps     int `json:"steps"`
	States    int `json:"states"`
	Snapshots int `json:"snapshots"`
}
