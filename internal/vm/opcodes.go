// Package vm implements the grammar v1 backtracking execution model. Nothing
// in this package asks the host regular-expression library for results.
package vm

import "trajex/internal/syntax"

// Op identifies a bytecode instruction.
type Op byte

const (
	OpChar Op = iota + 1 // consume one rune equal to Arg (fold aware via Flags)
	OpClass              // consume one rune accepted by Class
	OpAny                // consume any rune except \n (unless dotall)
	OpSave               // capture slot update: slot=Arg, value=thread pos
	OpSplit              // ordered fork: Arg=greedy(0)/lazy(1), Targets=[a,b]
	OpGuard              // zero-width repeat guard: Arg=guard id; jump Target if held at pos
	OpJmp
	OpRef // backreference: Arg=group index
	OpCheck
	OpWord
	OpFail      // die immediately
	OpMatch     // accept
	OpLBegin    // start lookaround frame: Arg encodes flags; Targets list candidate starts
	OpLEnd      // close lookaround frame: Arg=behind, Arg2=negative
	OpBeginText // anchor A
	OpEndText   // anchor z / Z / $
	OpBeginLine // ^
	OpEndLine   // $
)

// Instr is one bytecode instruction.
type Instr struct {
	Op       Op
	Arg      int
	Arg2     int
	Targets  []int
	Class    *syntax.Class
	Flags    byte // bit0: fold(case-insensitive), bit1: multiline, bit2: dotall
	Source   syntax.Node
	GuardID  int
}

const (
	FlagFold      = 1
	FlagMultiline = 2
	FlagDotall    = 4
)

// Program is a compiled pattern.
type Program struct {
	Code       []Instr
	NumGroups  int
	NumGuards  int
	Named      map[string]int
	Pattern    string
	Optimized  bool
	OptimNote  string
}

// slotCount is 2*(groups+1): even=start, odd=end.
func (p *Program) slotCount() int { return 2 * (p.NumGroups + 1) }

// Caps is a capture snapshot: slot -> byte position; -1 means unset.
type Caps []int

func newCaps(n int) Caps {
	c := make(Caps, n)
	for i := range c {
		c[i] = -1
	}
	c[0] = 0
	return c
}

func (c Caps) clone() Caps {
	out := make(Caps, len(c))
	copy(out, c)
	return out
}
