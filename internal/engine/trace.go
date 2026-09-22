package engine

// EventKind classifies trace events emitted by the VM.
type EventKind string

const (
	EvSpawn      EventKind = "spawn"      // thread created
	EvChar       EventKind = "char"       // rune/class/dot matched or failed
	EvAnchor     EventKind = "anchor"     // anchor evaluated
	EvSplit      EventKind = "split"      // priority fork; Detail names the branch taken
	EvBacktrack  EventKind = "backtrack"  // thread died; next alternative popped
	EvKill       EventKind = "kill"       // thread pruned (lookahead sibling, negative hit)
	EvGroupOpen  EventKind = "group-open"
	EvGroupClose EventKind = "group-close"
	EvBackref    EventKind = "backref"
	EvLookIn     EventKind = "look-in"
	EvLookOut    EventKind = "look-out"
	EvGuard      EventKind = "guard" // empty-iteration guard fired
	EvMatch      EventKind = "match"
	EvNoMatch    EventKind = "no-match"
	EvHalt       EventKind = "halt" // budget exhausted, frontier available
)

// Event is one entry in the execution trace. Pos is a code-point index;
// Byte is the corresponding byte offset. Caps is present on capture-related
// events so the UI can show capture history without replaying.
type Event struct {
	Seq    int       `json:"seq"`
	Step   int       `json:"step"`
	Thread int       `json:"thread"`
	PC     int       `json:"pc"`
	Pos    int       `json:"pos"`
	Byte   int       `json:"byte"`
	Kind   EventKind `json:"kind"`
	Detail string    `json:"detail,omitempty"`
	Group  int       `json:"group,omitempty"`
	Value  string    `json:"value,omitempty"`
	Caps   []int     `json:"caps,omitempty"`
}

// ThreadState is a serializable snapshot of a live thread; it is also the
// frontier element used to resume an interrupted run.
type ThreadState struct {
	ID     int            `json:"id"`
	PC     int            `json:"pc"`
	Pos    int            `json:"pos"`
	Caps   []int          `json:"caps"`
	Marks  map[int]int    `json:"marks,omitempty"`
	Frames []FrameState   `json:"frames,omitempty"`
}

// FrameState is a serializable lookaround frame.
type FrameState struct {
	ID        int            `json:"id"`
	StaticID  int            `json:"static_id"`
	Neg       bool           `json:"neg"`
	Behind    bool           `json:"behind"`
	RetPC     int            `json:"ret_pc"`
	StartPos  int            `json:"start_pos"`
	TargetPos int            `json:"target_pos"`
	SavedCaps []int          `json:"saved_caps"`
	SavedMarks map[int]int   `json:"saved_marks,omitempty"`
}

// Status is the outcome of a run.
type Status string

const (
	StatusMatch         Status = "match"
	StatusNoMatch       Status = "no-match"
	StatusIndeterminate Status = "indeterminate"
)

// Budgets bound a run. Zero means unlimited.
type Budgets struct {
	Steps     int `json:"steps"`
	States    int `json:"states"`
	Snapshots int `json:"snapshots"`
}

// DefaultBudgets are used when the caller does not specify any.
var DefaultBudgets = Budgets{Steps: 200000, States: 20000, Snapshots: 20000}

// GroupCapture describes one capture group in a match.
type GroupCapture struct {
	Index     int    `json:"index"`
	Name      string `json:"name,omitempty"`
	Set       bool   `json:"set"`
	Start     int    `json:"start"`
	End       int    `json:"end"`
	ByteStart int    `json:"byte_start"`
	ByteEnd   int    `json:"byte_end"`
	Text      string `json:"text"`
}

// Match is the winning thread's result.
type Match struct {
	Start     int            `json:"start"`
	End       int            `json:"end"`
	ByteStart int            `json:"byte_start"`
	ByteEnd   int            `json:"byte_end"`
	Text      string         `json:"text"`
	Groups    []GroupCapture `json:"groups"`
}

// Result is the outcome of a single search (possibly across several start
// positions). Every result pins the grammar and Unicode data versions.
type Result struct {
	Status         Status        `json:"status"`
	Match          *Match        `json:"match,omitempty"`
	Events         []Event       `json:"events"`
	Frontier       []ThreadState `json:"frontier,omitempty"`
	Steps          int           `json:"steps"`
	States         int           `json:"states"`
	Snapshots      int           `json:"snapshots"`
	SyntaxVersion  string        `json:"syntax_version"`
	UnicodeVersion string        `json:"unicode_version"`
	HaltReason     string        `json:"halt_reason,omitempty"`
}
