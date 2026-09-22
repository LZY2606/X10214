// Package compile lowers the regex AST into a small backtracking bytecode
// program executed by package engine. The bytecode is deterministic: branch
// priorities are encoded explicitly so every optimizer must preserve them.
package compile

import (
	"traceroom/syntax"
)

// Op enumerates bytecode instructions.
type Op uint8

const (
	OpChar      Op = iota // consume one rune equal to Arg (flags: case-insensitive)
	OpClass               // consume one rune accepted by Program.Classes[Arg]
	OpAny                 // consume one rune (dot; newline depends on flags)
	OpMatch               // terminal success
	OpJmp                 // Arg = target pc
	OpSplit               // Arg = target pc of secondary branch; primary falls through
	OpSave                // Arg = capture slot; sets slot[Arg] = current pos
	OpLoopStart           // Arg = loop id; record iteration anchor
	OpLoopGuard           // Arg = loop id; fail if pos == anchor and Arg2 marks iter>0
	OpLook                // Arg = subprogram id; Arg2 = LookFlags bits
	OpOpenCut             // Arg = cut id; opens an atomic region
	OpCloseCut            // Arg = cut id; closes the atomic region (kept by success path)
	OpAnchor              // Arg = anchor kind (syntax.AnchorKind)
	OpBackref             // Arg = group id; match capture contents
	// SubEnd marks the end of a lookaround subprogram.
	OpSubEnd
)

// LookFlags bits carried by OpLook.
const (
	LookBehind   = 1
	LookNegative = 2
)

// Instr is one bytecode instruction.
type Instr struct {
	Op   Op
	Arg  int
	Arg2 int
	Lo   int // pattern source range for UI mapping
	Hi   int
}

// Program is a compiled program plus its lookaround subprograms.
type Program struct {
	Code      []Instr
	Subs      map[int]*Program // keyed by sub id
	Classes   []*syntax.Class
	NumCaps   int // capture groups (slot count is 2*(NumCaps+1))
	Names     map[string]int
	CapNames  map[int]string
	Pattern   string
	Flags     uint8
	Optimized bool
}

// SlotCount returns the number of capture slots (2 per group incl. group 0).
func (p *Program) SlotCount() int { return 2 * (p.NumCaps + 1) }
