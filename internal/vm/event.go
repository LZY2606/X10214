package vm

// EventKind enumerates trajectory event categories.
type EventKind string

const (
	EvStep          EventKind = "step"
	EvSplit         EventKind = "split"
	EvCapture       EventKind = "capture"
	EvBacktrack     EventKind = "backtrack"
	EvPrune         EventKind = "prune"
	EvLook          EventKind = "look"
	EvRepeat        EventKind = "repeat"
	EvSearch        EventKind = "search"
	EvMatch         EventKind = "match"
	EvFail          EventKind = "fail"
	EvIndeterminate EventKind = "indeterminate"
	EvPause         EventKind = "pause"
)

// Event is one displayed trajectory entry.
type Event struct {
	Seq     int         `json:"seq"`
	Kind    EventKind   `json:"kind"`
	Pc      int         `json:"pc"`
	NID     int         `json:"nid"`
	Op      string      `json:"op,omitempty"`
	Pos     int         `json:"pos"`
	Detail  string      `json:"detail,omitempty"`
	Caps    []CapSlot   `json:"caps,omitempty"`
	CapDiff []CapChange `json:"capDiff,omitempty"`
	Stack   int         `json:"stackDepth"`
}

// CapChange records one capture slot update in an event.
type CapChange struct {
	Index    int    `json:"index"`
	Name     string `json:"name,omitempty"`
	OldBegin int    `json:"oldBegin"`
	OldEnd   int    `json:"oldEnd"`
	NewBegin int    `json:"newBegin"`
	NewEnd   int    `json:"newEnd"`
}
