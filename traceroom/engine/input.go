package engine

import (
	"unicode/utf8"
)

// Input is UTF-8 aware indexing over a match subject.
//
// Every position used by the VM is a *byte offset*. In addition, CPAt maps
// each byte offset to its code-point index so the UI can show both. Invalid
// UTF-8 bytes decode to utf8.RuneError and consume one byte (and one code
// point), matching the documented replacement semantics.
type Input struct {
	Bytes []byte
	// CPAt[i] is the code point index of the rune starting at byte i; offsets
	// inside a multi-byte rune repeat the index of that rune. len == Bytes+1.
	CPAt []int
	// RuneStart[i] reports whether byte i begins a rune boundary.
	RuneStart []bool
	NumCP     int
}

// NewInput builds indices over s.
func NewInput(s string) *Input {
	b := []byte(s)
	n := len(b)
	in := &Input{
		Bytes:     b,
		CPAt:      make([]int, n+1),
		RuneStart: make([]bool, n+1),
	}
	if n == 0 {
		return in
	}
	cp := 0
	for i := 0; i < n; {
		in.RuneStart[i] = true
		r, sz := utf8.DecodeRune(b[i:])
		if r == utf8.RuneError && sz == 1 {
			sz = 1
		}
		for k := 0; k < sz; k++ {
			in.CPAt[i+k] = cp
		}
		i += sz
		cp++
	}
	in.CPAt[n] = cp
	in.RuneStart[n] = true
	in.NumCP = cp
	return in
}

// DecodeRune returns the rune starting at byte pos and its width in bytes.
// At end it returns (-1, 0). Invalid byte yields RuneError width 1.
func (in *Input) DecodeRune(pos int) (rune, int) {
	if pos >= len(in.Bytes) {
		return -1, 0
	}
	r, sz := utf8.DecodeRune(in.Bytes[pos:])
	return r, sz
}

// CP returns the code-point index for a byte offset.
func (in *Input) CP(bytePos int) int {
	if bytePos >= len(in.CPAt) {
		return in.NumCP
	}
	return in.CPAt[bytePos]
}

// Pos is a dual byte / code-point position for API payloads.
type Pos struct {
	Byte int `json:"byte"`
	CP   int `json:"cp"`
}

// PosOf builds a UI position.
func (in *Input) PosOf(bytePos int) Pos {
	return Pos{Byte: bytePos, CP: in.CP(bytePos)}
}
