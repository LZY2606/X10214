package engine

import (
	"encoding/json"
	"fmt"
)

// Status is the lifecycle state of a run.
type Status string

const (
	StatusRunning     Status = "running"
	StatusMatched     Status = "match"
	StatusNoMatch     Status = "no-match"
	StatusIndetermine Status = "indeterminate"
)

// LimitReason explains why a run became indeterminate.
type LimitReason string

const (
	LimitSteps    LimitReason = "step-budget"
	LimitStates   LimitReason = "state-budget"
	LimitSnapshot LimitReason = "capture-snapshot-budget"
)

// Budget bounds the resources a single execution may consume. Zero means
// unlimited for that counter.
type Budget struct {
	MaxSteps     int `json:"maxSteps"`
	MaxStates    int `json:"maxStates"`
	MaxSnapshots int `json:"maxSnapshots"`
	MaxEvents    int `json:"maxEvents"`
}

// DefaultBudget is used when the caller supplies no limits.
var DefaultBudget = Budget{
	MaxSteps:     200000,
	MaxStates:    100000,
	MaxSnapshots: 200000,
	MaxEvents:    20000,
}

// EventKind tags each trace entry.
type EventKind string

const (
	EvStart       EventKind = "start"
	EvConsume     EventKind = "consume"
	EvPushChoice  EventKind = "push-choice"
	EvTakeBranch  EventKind = "take-branch"
	EvTryBranch   EventKind = "try-branch"
	EvPruneZW     EventKind = "prune-zero-width-repeat"
	EvSave        EventKind = "capture-update"
	EvAssertEnter EventKind = "assert-enter"
	EvAssertDone  EventKind = "assert-done"
	EvAssertFail  EventKind = "assert-fail-retry"
	EvBacktrack   EventKind = "backtrack"
	EvAdvance     EventKind = "advance-start"
	EvMatch       EventKind = "match"
	EvFail        EventKind = "fail"
	EvPruneLimit  EventKind = "prune-budget"
)

// Event is one immutable trace entry shown in the room.
type Event struct {
	Seq      int            `json:"seq"`
	Kind     EventKind      `json:"kind"`
	Pc       int            `json:"pc,omitempty"`
	Pos      int            `json:"pos"`
	RunePos  int            `json:"runePos"`
	Detail   string         `json:"detail,omitempty"`
	Captures map[string][2]int `json:"captures,omitempty"`
}

// ChoiceFrame is one alternative kept for backtracking.
type ChoiceFrame struct {
	Pc        int    `json:"pc"`        // OpStart pc to retry (alt target)
	AltPc     int    `json:"altPc"`     // alternative continuation
	Pos       int    `json:"pos"`       // input position when pushed
	Caps      []int  `json:"caps"`      // capture snapshot to restore
	Guard     bool   `json:"guard"`     // one-shot zero-width guard consumed
	Kind      string `json:"kind"`      // "alt" | "repeat" | "search" | "assert"
	AssertRef int    `json:"assertRef"` // index into asserts when Kind==assert
}

// AssertFrame is the suspended lookaround.
type AssertFrame struct {
	Negative bool   `json:"negative"`
	Behind   bool   `json:"behind"`
	HomePos  int    `json:"homePos"`
	ResumePc int    `json:"resumePc"` // instruction after OpLook
	MaxLen   int    `json:"maxLen"`
	TryPos   int    `json:"tryPos"` // current candidate start (behind)
	Caps     []int  `json:"caps"`   // outer captures at entry
	Tried    bool   `json:"tried"`  // ahead: body attempted already
	Succeeded bool  `json:"succeeded"`
}

// Guard is the zero-width repeat lineage marker.
type Guard struct {
	Pc       int `json:"pc"`
	Pos      int `json:"pos"`
	Depth    int `json:"depth"` // choice-stack depth at creation
	Consumed bool `json:"consumed"`
}

// Frontier is the resumable state serialized to SQLite.
type Frontier struct {
	Pc       int           `json:"pc"`
	Pos      int           `json:"pos"`
	Caps     []int         `json:"caps"`
	Choices  []ChoiceFrame `json:"choices"`
	Asserts  []AssertFrame `json:"asserts"`
	Guards   []Guard       `json:"guards"`
	Steps    int           `json:"steps"`
	States   int           `json:"states"`
	Snaps    int           `json:"snaps"`
	Status   Status        `json:"status"`
	Reason   LimitReason   `json:"reason,omitempty"`
}

// Capture is one resolved group result.
type Capture struct {
	Index int    `json:"index"`
	Name  string `json:"name,omitempty"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	Text  string `json:"text"`
}

// MatchResult is the final outcome.
type MatchResult struct {
	Status         Status    `json:"status"`
	Reason         LimitReason `json:"reason,omitempty"`
	Start          int       `json:"start"`
	End            int       `json:"end"`
	RuneStart      int       `json:"runeStart"`
	RuneEnd        int       `json:"runeEnd"`
	Captures       []Capture `json:"captures"`
	Steps          int       `json:"steps"`
	States         int       `json:"states"`
	Snapshots      int       `json:"snapshots"`
	SyntaxVersion  string    `json:"syntaxVersion"`
	UnicodeVersion string    `json:"unicodeVersion"`
	Match          bool      `json:"match"`
}

// VM is the explicit-state backtracking executor.
type VM struct {
	prog    *Program
	input   *Input
	budget  Budget
	pc      int
	pos     int
	caps    []int
	choices []ChoiceFrame
	asserts []AssertFrame
	guards  []Guard
	events  []Event
	seq     int
	steps   int
	states  int
	snaps   int
	status  Status
	reason  LimitReason
	evCapped bool
}

// NewVM builds an executor; from is nil for a fresh start or a Frontier to
// resume a previously paused run.
func NewVM(prog *Program, input *Input, budget Budget, from *Frontier) (*VM, error) {
	if budget == (Budget{}) {
		budget = DefaultBudget
	}
	vm := &VM{
		prog:   prog,
		input:  input,
		budget: budget,
		pc:     prog.Start,
		caps:   make([]int, prog.NumSlots),
		status: StatusRunning,
	}
	for i := range vm.caps {
		vm.caps[i] = -1
	}
	if from != nil {
		if err := vm.restore(from); err != nil {
			return nil, err
		}
	} else {
		vm.record(EvStart, prog.Start, 0, "execution started", nil)
	}
	return vm, nil
}

func (vm *VM) restore(f *Frontier) error {
	vm.pc = f.Pc
	vm.pos = f.Pos
	vm.caps = append([]int(nil), f.Caps...)
	vm.choices = append([]ChoiceFrame(nil), f.Choices...)
	vm.asserts = append([]AssertFrame(nil), f.Asserts...)
	vm.guards = append([]Guard(nil), f.Guards...)
	vm.steps, vm.states, vm.snaps = f.Steps, f.States, f.Snaps
	vm.seq = len(vm.events)
	vm.record(EvStart, vm.pc, vm.pos, "resumed from frontier", nil)
	return nil
}

// Frontier returns a serializable snapshot of the resumable state.
func (vm *VM) Frontier() *Frontier {
	return &Frontier{
		Pc: vm.pc, Pos: vm.pos,
		Caps:    append([]int(nil), vm.caps...),
		Choices: append([]ChoiceFrame(nil), vm.choices...),
		Asserts: append([]AssertFrame(nil), vm.asserts...),
		Guards:  append([]Guard(nil), vm.guards...),
		Steps:   vm.steps, States: vm.states, Snaps: vm.snaps,
		Status: vm.status, Reason: vm.reason,
	}
}

// MarshalFrontier encodes a frontier for storage.
func MarshalFrontier(f *Frontier) (string, error) {
	b, err := json.Marshal(f)
	return string(b), err
}

// UnmarshalFrontier decodes a stored frontier.
func UnmarshalFrontier(s string) (*Frontier, error) {
	var f Frontier
	if err := json.Unmarshal([]byte(s), &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// Events returns the trace so far.
func (vm *VM) Events() []Event { return vm.events }

// Status reports the lifecycle state.
func (vm *VM) Status() Status { return vm.status }

func cloneCaps(caps []int) []int {
	out := make([]int, len(caps))
	copy(out, caps)
	return out
}

func (vm *VM) record(kind EventKind, pc, pos int, detail string, caps map[string][2]int) {
	if vm.budget.MaxEvents > 0 && vm.seq >= vm.budget.MaxEvents {
		vm.evCapped = true
		return
	}
	ev := Event{Seq: vm.seq, Kind: kind, Pc: pc, Pos: pos, RunePos: vm.input.RuneIndex(pos), Detail: detail, Captures: caps}
	vm.events = append(vm.events, ev)
	vm.seq++
}

func (vm *VM) chargeState() bool {
	vm.states++
	if vm.budget.MaxStates > 0 && vm.states > vm.budget.MaxStates {
		vm.suspend(LimitStates)
		return false
	}
	return true
}

func (vm *VM) chargeStep() bool {
	vm.steps++
	if vm.budget.MaxSteps > 0 && vm.steps > vm.budget.MaxSteps {
		vm.suspend(LimitSteps)
		return false
	}
	return true
}

func (vm *VM) takeSnapshot() []int {
	vm.snaps++
	if vm.budget.MaxSnapshots > 0 && vm.snaps > vm.budget.MaxSnapshots {
		vm.suspend(LimitSnapshot)
		return nil
	}
	return cloneCaps(vm.caps)
}

func (vm *VM) suspend(reason LimitReason) {
	if vm.status == StatusRunning {
		vm.status = StatusIndeterminate
		vm.reason = reason
		vm.record(EvPruneLimit, vm.pc, vm.pos, "budget exhausted: "+string(reason), nil)
	}
}
