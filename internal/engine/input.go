package engine

import (
	"sort"
	"unicode/utf8"
)

// Input is the subject string with an explicit mapping between byte offsets
// (positions used by the VM) and Unicode code-point indices. Invalid UTF-8 is
// not rejected: each malformed byte is exposed as RuneError of length 1 so
// that byte and code-point positions stay consistent and inspectable.
type Input struct {
	Text        string
	byteToRune  []int // len == len(Text)+1; code-point index at byte offset
	runeToByte  []int // byte offset of each code point, plus terminal offset
	runeCount   int
}

// NewInput indexes s. Byte positions used everywhere in the VM are byte
// offsets into Text.
func NewInput(s string) *Input {
	in := &Input{Text: s}
	in.byteToRune = make([]int, len(s)+1)
	in.runeToByte = []int{0}
	ri := 0
	for i := 0; i < len(s); {
		_, size := utf8.DecodeRuneInString(s[i:])
		if size == 0 {
			size = 1
		}
		for k := 0; k < size; k++ {
			in.byteToRune[i+k] = ri
		}
		i += size
		ri++
		in.runeToByte = append(in.runeToByte, i)
	}
	in.byteToRune[len(s)] = ri
	in.runeCount = ri
	return in
}

// RuneIndex converts a VM byte position to a Unicode code-point index.
func (in *Input) RuneIndex(bytePos int) int {
	if bytePos < 0 {
		return 0
	}
	if bytePos > len(in.Text) {
		return in.runeCount
	}
	return in.byteToRune[bytePos]
}

// ByteOffset converts a code-point index back to a byte position.
func (in *Input) ByteOffset(runeIdx int) int {
	if runeIdx < 0 {
		return 0
	}
	if runeIdx >= len(in.runeToByte) {
		return len(in.Text)
	}
	return in.runeToByte[runeIdx]
}

// RuneCount returns the number of code points (invalid bytes count as one).
func (in *Input) RuneCount() int { return in.runeCount }

// Len returns the length in bytes.
func (in *Input) Len() int { return len(in.Text) }

// RuneAt returns the code point at byte position pos and its byte length.
// Invalid UTF-8 yields RuneError with size 1.
func (in *Input) RuneAt(pos int) (r rune, size int) {
	if pos < 0 || pos >= len(in.Text) {
		return utf8.RuneError, 0
	}
	r, size = utf8.DecodeRuneInString(in.Text[pos:])
	return r, size
}

// Slice returns the substring for a byte range.
func (in *Input) Slice(lo, hi int) string {
	if lo < 0 {
		lo = 0
	}
	if hi > len(in.Text) {
		hi = len(in.Text)
	}
	if lo > hi {
		return ""
	}
	return in.Text[lo:hi]
}

// Context marks the text around a byte position for anchor evaluation.
func (in *Input) contextRune(pos int, before bool) (rune, bool) {
	p := pos
	if before {
		// rune ending at pos
		_, sz := utf8.DecodeLastRuneInString(in.Text[:p])
		if sz == 0 {
			return 0, false
		}
		p -= sz
	}
	r, sz := utf8.DecodeRuneInString(in.Text[p:])
	if sz == 0 {
		return 0, false
	}
	return r, true
}

// SortedInts is a small helper used by the snapshot code.
func SortedInts(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}
