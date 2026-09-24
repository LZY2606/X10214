package engine

import "unicode/utf8"

// Input decodes UTF-8 text into runes while tracking byte offsets.
// Invalid bytes decode as U+FFFD occupying exactly one byte, so byte
// positions and code-point positions stay simultaneously queryable.
type Input struct {
	Text    string
	Runes   []rune
	ByteOff []int // byte offset of each rune
}

func DecodeInput(s string) *Input {
	in := &Input{Text: s}
	b := []byte(s)
	for i := 0; i < len(b); {
		r, w := utf8.DecodeRune(b[i:])
		in.Runes = append(in.Runes, r)
		in.ByteOff = append(in.ByteOff, i)
		i += w
	}
	return in
}

// Len returns the length in code points.
func (in *Input) Len() int { return len(in.Runes) }

// BytePos maps a code-point position to a byte offset.
func (in *Input) BytePos(cp int) int {
	if cp >= len(in.Runes) {
		return len(in.Text)
	}
	return in.ByteOff[cp]
}

// Slice returns the text between code-point positions [s, e).
func (in *Input) Slice(s, e int) string {
	if s < 0 || e < 0 || s > e || e > len(in.Runes) {
		return ""
	}
	return string(in.Runes[s:e])
}
