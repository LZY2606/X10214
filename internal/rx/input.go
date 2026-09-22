package rx

import "unicode/utf8"

// Idx is a dual-position index over an input byte string.
// Every element is either a decoded code point (size > 1 possible) or a
// single invalid UTF-8 byte represented by utf8.RuneError.
type Idx struct {
	Bytes  []byte
	Start  []int // byte offset of element i
	Runes  []rune
	Sizes  []int // size in bytes; invalid bytes use 1
}

// NewIdx decodes s, never replacing more than one byte per invalid sequence.
func NewIdx(s string) *Idx {
	ix := &Idx{Bytes: []byte(s)}
	b := ix.Bytes
	for i := 0; i < len(b); {
		r, size := utf8.DecodeRune(b[i:])
		ix.Start = append(ix.Start, i)
		ix.Runes = append(ix.Runes, r)
		ix.Sizes = append(ix.Sizes, size)
		i += size
	}
	return ix
}

// Len is the number of code-point elements (invalid bytes count as one each).
func (x *Idx) Len() int { return len(x.Runes) }

// RuneAt returns the element at code-point position p.
func (x *Idx) RuneAt(p int) (rune, bool) {
	if p < 0 || p >= len(x.Runes) {
		return 0, false
	}
	return x.Runes[p], true
}

// BytePos converts a code-point position to a byte offset (Len() => len(Bytes)).
func (x *Idx) BytePos(p int) int {
	if p >= len(x.Start) {
		return len(x.Bytes)
	}
	if p < 0 {
		return 0
	}
	return x.Start[p]
}

// RunePos converts a byte offset to a code-point position (snaps to element start).
func (x *Idx) RunePos(off int) int {
	if off >= len(x.Bytes) {
		return len(x.Runes)
	}
	if off <= 0 || len(x.Start) == 0 {
		return 0
	}
	lo, hi := 0, len(x.Start)
	for lo < hi {
		mid := (lo + hi) / 2
		if x.Start[mid] <= off {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo - 1
}

// Slice returns the substring captured between code-point positions a,b.
// Open spans (b < a) render as the empty string for trail display.
func (x *Idx) Slice(a, b int) string {
	if b < a || a < 0 {
		return ""
	}
	bo := x.BytePos(a)
	eo := x.BytePos(b)
	if eo < bo {
		return ""
	}
	return string(x.Bytes[bo:eo])
}
