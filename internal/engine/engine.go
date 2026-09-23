package engine

import (
	"encoding/json"
	"fmt"
	"unicode"
	"unicode/utf8"
)

const UnicodeDataVersion = unicode.Version

type Pos struct {
	Byte int `json:"byte"`
	Rune int `json:"rune"`
}

type Span struct {
	Start Pos    `json:"start"`
	End   Pos    `json:"end"`
	Text  string `json:"text"`
}

type Budget struct {
	MaxSteps      int `json:"maxSteps"`
	MaxStates     int `json:"maxStates"`
	MaxSnapshots  int `json:"maxSnapshots"`
	MaxLookbehind int `json:"maxLookbehind"`
}

func DefaultBudget() Budget {
	return Budget{MaxSteps: 5000, MaxStates: 1000, MaxSnapshots: 2000, MaxLookbehind: 256}
}

type Usage struct {
	Steps     int `json:"steps"`
	States    int `json:"states"`
	Snapshots int `json:"snapshots"`
}

type Event struct {
	Seq    int             `json:"seq"`
	Kind   string          `json:"kind"`
	NodeID string          `json:"nodeId,omitempty"`
	Type   string          `json:"nodeType,omitempty"`
	Pos    *Pos            `json:"pos,omitempty"`
	End    *Pos            `json:"end,omitempty"`
	Group  int             `json:"group,omitempty"`
	Name   string          `json:"name,omitempty"`
	Detail string          `json:"detail,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
}

type FrontierPoint struct {
	NodeID string `json:"nodeId"`
	Type   string `json:"type"`
	Detail string `json:"detail"`
	Byte   int    `json:"byte"`
	Rune   int    `json:"rune"`
}

type MatchResult struct {
	Matched       bool            `json:"matched"`
	Status        string          `json:"status"`
	Syntax        string          `json:"syntaxVersion"`
	Unicode       string          `json:"unicodeVersion"`
	MatchStart    *Pos            `json:"matchStart,omitempty"`
	MatchEnd      *Pos            `json:"matchEnd,omitempty"`
	Captures      map[string]Span `json:"captures,omitempty"`
	GroupOrder    []GroupName     `json:"groupOrder"`
	Events        []Event         `json:"events"`
	Frontier      []FrontierPoint `json:"frontier,omitempty"`
	Budget        Budget          `json:"budget"`
	Usage         Usage           `json:"usage"`
	InputLength   Pos             `json:"inputLength"`
	ByteToRune    []int           `json:"byteToRune,omitempty"`
	Pruned        int             `json:"pruned"`
	Backtracks    int             `json:"backtracks"`
	Indeterminate bool            `json:"indeterminate"`
	Reason        string          `json:"reason,omitempty"`
}

type Input struct {
	Bytes      []byte
	RuneStarts []int
}

func NewInput(s string) Input {
	b := []byte(s)
	in := Input{Bytes: b, RuneStarts: []int{0}}
	for off := 0; off < len(b); {
		_, size := utf8.DecodeRune(b[off:])
		off += size
		in.RuneStarts = append(in.RuneStarts, off)
	}
	return in
}

func (in Input) PosAt(bytePos int) Pos {
	rp := 0
	for i, start := range in.RuneStarts {
		if start == bytePos {
			rp = i
			break
		}
		if start < bytePos {
			rp = i
		}
	}
	return Pos{Byte: bytePos, Rune: rp}
}

func (in Input) Elem(pos Pos) (rune, int, bool) {
	if pos.Byte >= len(in.Bytes) {
		return 0, 0, false
	}
	r, size := utf8.DecodeRune(in.Bytes[pos.Byte:])
	return r, size, r == utf8.RuneError && size == 1
}

func (in Input) advance(pos Pos) Pos {
	if pos.Byte >= len(in.Bytes) {
		return pos
	}
	_, size := utf8.DecodeRune(in.Bytes[pos.Byte:])
	return Pos{Byte: pos.Byte + size, Rune: pos.Rune + 1}
}

func (in Input) end() Pos { return Pos{Byte: len(in.Bytes), Rune: len(in.RuneStarts) - 1} }

func (in Input) slice(s, e Pos) string { return string(in.Bytes[s.Byte:e.Byte]) }

type Command struct {
	Action string
	Budget Budget
}

type Controller interface {
	BeforeStep(kind string, node Node, pos Pos, caps []*Span, usage Usage, budget Budget, frontier []FrontierPoint) (Command, bool)
}

type engine struct {
	pat         *Pattern
	in          Input
	budget      Budget
	usage       Usage
	events      []Event
	pruned      int
	backtracks  int
	frontier    []*frontierFrame
	controller  Controller
	stopped     bool
	stepCommand string
	groupNames  map[string]int
}

type frontierFrame struct {
	node   Node
	pos    Pos
	detail string
}

type outcome int

const (
	outcomeFailed outcome = iota
	outcomePaused
	outcomeAborted
	outcomeCut
	outcomeSuccess
)

type continuation func(Pos, []*Span) outcome

func Run(pat *Pattern, input string, budget Budget, ctrl Controller) *MatchResult {
	if budget.MaxSteps <= 0 {
		budget.MaxSteps = DefaultBudget().MaxSteps
	}
	if budget.MaxStates <= 0 {
		budget.MaxStates = DefaultBudget().MaxStates
	}
	if budget.MaxSnapshots <= 0 {
		budget.MaxSnapshots = DefaultBudget().MaxSnapshots
	}
	if budget.MaxLookbehind <= 0 {
		budget.MaxLookbehind = DefaultBudget().MaxLookbehind
	}
	e := &engine{pat: pat, in: NewInput(input), budget: budget, controller: ctrl, groupNames: pat.Names}
	caps := make([]*Span, pat.GroupCount+1)
	var found bool
	var foundEnd Pos
	var foundCaps []*Span
	for startByte := 0; !found && startByte <= len(e.in.Bytes); {
		start := e.in.PosAt(startByte)
		save := cloneCaps(caps)
		st := e.match(e.pat.Root, start, caps, func(end Pos, c []*Span) outcome {
			c2 := cloneCaps(c)
			c2[0] = &Span{Start: start, End: end, Text: e.in.slice(start, end)}
			found, foundEnd, foundCaps = true, end, c2
			e.emit("match-root", e.pat.Root, end, 0, "", "leftmost match accepted", nil)
			return outcomeSuccess
		})
		if st == outcomeSuccess {
			break
		}
		if st == outcomePaused {
			return e.snapshotResult(false, "paused", "paused before next bounded step", caps)
		}
		if st == outcomeAborted {
			return e.snapshotResult(false, "indeterminate", "resource budget exhausted; frontier remains resumable", caps)
		}
		caps = save
		if startByte == len(e.in.Bytes) {
			break
		}
		_, size := utf8.DecodeRune(e.in.Bytes[startByte:])
		startByte += size
	}
	if found {
		caps = foundCaps
		return e.finishMatch(foundCaps, foundEnd)
	}
	return e.finishNoMatch()
}

func foundStart(caps []*Span) Pos {
	if caps[0] != nil {
		return caps[0].Start
	}
	return Pos{}
}

func (e *engine) finishMatch(caps []*Span, end Pos) *MatchResult {
	r := e.baseResult()
	r.Matched, r.Status = true, "match"
	r.MatchEnd = &end
	if len(caps) > 0 && caps[0] != nil {
		start := caps[0].Start
		r.MatchStart = &start
	}
	r.Captures = captureMap(e.pat, caps)
	return r
}

func (e *engine) finishNoMatch() *MatchResult {
	r := e.baseResult()
	r.Status = "no-match"
	return r
}

func (e *engine) snapshotResult(matched bool, status, reason string, caps []*Span) *MatchResult {
	r := e.baseResult()
	r.Matched, r.Status, r.Indeterminate, r.Reason = matched, status, true, reason
	r.Captures = captureMap(e.pat, caps)
	return r
}

func (e *engine) baseResult() *MatchResult {
	return &MatchResult{
		Matched: false, Syntax: SyntaxVersion, Unicode: UnicodeDataVersion,
		Events: append([]Event(nil), e.events...), Frontier: e.snapshotFrontier(),
		Budget: e.budget, Usage: e.usage, GroupOrder: e.pat.Groups,
		InputLength: e.in.end(), Pruned: e.pruned, Backtracks: e.backtracks,
		Captures: map[string]Span{},
	}
}

func captureMap(p *Pattern, caps []*Span) map[string]Span {
	out := map[string]Span{}
	for _, g := range p.Groups {
		if g.Index < len(caps) && caps[g.Index] != nil {
			key := fmt.Sprintf("%d", g.Index)
			if g.Name != "" {
				key = g.Name
			}
			out[key] = *caps[g.Index]
		}
	}
	return out
}
