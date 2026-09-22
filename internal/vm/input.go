package vm

import "unicode/utf8"

// Input is the subject string with parallel byte and code-point coordinates.
// Invalid UTF-8 bytes are each kept as their own width-1 unit and surface as
// RuneError atoms so the execution model never silently skips them.
type Input struct {
	Raw string

	// RuneStart[i] is the byte offset of code point i; an extra final entry
	// equals len(Raw) for end-of-input positions.
	RuneStart []int
	// ByteRune maps every byte offset to its code-point index.
	ByteRune []int
	// Units[b] is 1 for every byte; RuneBytes gives the width of the unit
	// starting at rune index i.
	RuneBytes []int
	// RuneAt[i] is the decoded rune (RuneError for invalid bytes).
	RuneAt []rune
	// InvalidByte[i] is true when rune i is an invalid UTF-8 byte.
	InvalidByte []bool
}

// NewInput decodes s into the dual-coordinate representation.
func NewInput(s string) *Input {
	in := &Input{Raw: s}
	for off := 0; off <= len(s); {
		idx := len(in.RuneAt)
		in.RuneStart = append(in.RuneStart, off)
		if off == len(s) {
			break
		}
		r, size := utf8.DecodeRuneInString(s[off:])
		if r == utf8.RuneError && size == 1 {
			in.RuneAt = append(in.RuneAt, 0xFFFD)
			in.InvalidByte = append(in.InvalidByte, true)
			in.RuneBytes = append(in.RuneBytes, 1)
		} else {
			in.RuneAt = append(in.RuneAt, r)
			in.InvalidByte = append(in.InvalidByte, false)
			in.RuneBytes = append(in.RuneBytes, size)
		}
		for k := 0; k < size; k++ {
			in.ByteRune = append(in.ByteRune, idx)
		}
		off += size
	}
	return in
}

// Bytes is the length in bytes.
func (in *Input) Bytes() int { return len(in.Raw) }

// Runes is the number of code point units (invalid bytes count as one).
func (in *Input) Runes() int { return len(in.RuneAt) }

// ByteToRune converts a byte offset to a code-point index.
func (in *Input) ByteToRune(bytePos int) int {
	if bytePos >= len(in.ByteRune) {
		return len(in.RuneAt)
	}
	return in.ByteRune[bytePos]
}

// RuneToByte converts a code-point index to its byte offset.
func (in *Input) RuneToByte(runePos int) int {
	if runePos >= len(in.RuneStart) {
		return len(in.Raw)
	}
	return in.RuneStart[runePos]
}
