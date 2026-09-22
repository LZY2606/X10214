// Package vm is the project-owned regular-expression execution model.
//
// It parses nothing itself (see internal/syntax) and never calls a host
// regular-expression library. Execution is an explicit depth-first backtracker
// with two stacks per level:
//
//	T - success continuations ("terms")
//	A - failure continuations / alternative handlers, in documented order
//
// Captures are immutable snapshots; every alternative frame stores the
// snapshot to restore, so groups that participated in a later-undoed attempt
// are rolled back and can never leak into a sibling branch.
package vm

import (
	"fmt"

	"capturetrace/internal/syntax"
)

// Mode selects the match operator.
type Mode string

const (
	ModeSearch    Mode = "search"
	ModeMatch     Mode = "match"     // anchored at start only
	ModeFullMatch Mode = "fullmatch" // anchored both ends
)

// Status is the terminal (or paused) verdict of a machine.
type Status string

const (
	StatusRunning       Status = "running"
	StatusMatched       Status = "matched"
	StatusNoMatch       Status = "no_match"
	StatusIndeterminate Status = "indeterminate"
)

// Frontier is the resumable state returned together with an indeterminate
// verdict. It captures the full machine (all nested levels) plus budgets.
type Frontier struct {
	Status Status    `json:"status"`
	Reason string    `json:"reason"`
	Levels []Level   `json:"levels"`
	Use    BudgetUse `json:"use"`
}

// Result is a terminal or paused outcome.
type Result struct {
	Status      Status          `json:"status"`
	Match       bool            `json:"match"`
	Start, End  int             `json:"start,omitempty"`
	RuneStart   int             `json:"rune_start,omitempty"`
	RuneEnd     int             `json:"rune_end,omitempty"`
	Groups      []Group         `json:"groups,omitempty"`
	Frontier    *Frontier       `json:"frontier,omitempty"`
	Use         BudgetUse       `json:"use"`
	Versions    syntax.Versions `json:"versions"`
	MatchedText string          `json:"matched_text,omitempty"`
}

// Machine is a paused or running executor.
type Machine struct {
	Pattern string
	Root    *syntax.Node
	Input   *Input
	Mode    Mode

	levels []*Level
	snaps  *snapStore
	use    BudgetUse

	events []Event
	seq    int

	// breakpoints on node IDs; Step stops after an event for one of them
	breakpoints map[int]bool
	// stopAt >= 0 requests a pause after this event sequence number
	stopAt int

	// breakpoint pause latch
	paused   bool
	pausedAt int

	status Status
	reason string

	// terminal answer fields
	matchStart, matchEnd int
	finalSnap            Snapshot

	// bounded event retention for the API (all events are returned in pages
	// by the runner; the machine keeps the last KeepEvents)
	keepEvents  int
	prunedCount int

	// cached backref resolution
	backrefIndex map[int]int
}

// Option configures a machine.
type Option func(*Machine)

// WithBudget sets execution limits.
func WithBudget(b Budget) Option {
	return func(m *Machine) {
		m.use.MaxSteps, m.use.MaxStates, m.use.MaxSnaps = b.MaxSteps, b.MaxStates, b.MaxSnapshots
	}
}

// WithBreakpoints pauses when any listed node ID emits an event.
func WithBreakpoints(ids []int) Option {
	return func(m *Machine) {
		for _, id := range ids {
			m.breakpoints[id] = true
		}
	}
}

// New builds a machine for an already parsed pattern.
func New(pattern string, root *syntax.Node, input *Input, mode Mode, opts ...Option) *Machine {
	m := &Machine{
		Pattern:      pattern,
		Root:         root,
		Input:        input,
		Mode:         mode,
		breakpoints:  map[int]bool{},
		stopAt:       -1,
		status:       StatusRunning,
		keepEvents:   5000,
		backrefIndex: map[int]int{},
	}
	for _, o := range opts {
		o(m)
	}
	m.snaps = newSnapStore(len(syntax.Groups(root)), m.use.MaxSnaps)
	m.initRoot()
	m.emit(EvStart, 0, fmt.Sprintf("mode=%s pattern=%q", mode, pattern))
	return m
}

func (m *Machine) initRoot() {
	l := &Level{Name: "root", Pos: 0, Snap: m.snaps.base.clone()}
	l.Snap.ID = 0
	if m.Mode == ModeSearch {
		l.A = []Alt{{Kind: AltNextStart, Pos: 0, Snap: l.Snap, NextStart: 0}}
	}
	l.pushT(Term{Kind: TermNode, NodeID: m.Root.ID})
	if m.Mode == ModeFullMatch {
		// fullmatch appends an end anchor to the user pattern
		endNode := m.anchorEndNode()
		// anchor runs after the user pattern, so push it first
		l.T = append([]Term{{Kind: TermNode, NodeID: endNode.ID}}, l.T...)
	}
	m.levels = []*Level{l}
}

// anchorEndNode is a synthetic \z node used by ModeFullMatch.
func (m *Machine) anchorEndNode() *syntax.Node {
	return &syntax.Node{ID: -1, Kind: syntax.KindAssertEnd, Src: `\z(synthetic)`}
}

// Events returns retained events (newest first is not used; caller orders).
func (m *Machine) Events() []Event { return m.events }

// Use returns current budget consumption.
func (m *Machine) Use() BudgetUse {
	m.use.Snapshots = m.snaps.count
	return m.use
}

// Status reports the current status.
func (m *Machine) Status() Status { return m.status }
