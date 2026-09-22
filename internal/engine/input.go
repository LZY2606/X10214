package engine

import "unicode/utf8"

// Input is the subject string decoded into code points. Invalid UTF-8
// bytes become single U+FFFD replacement units whose original byte is
// preserved in Raw so that byte-exact patterns can still match them.
type Input struct {
	Runes   []rune
	Raw     []byte // Raw[i] != 0 marks an invalid-byte replacement unit
	ByteOff []int  // len(Runes)+1 byte offsets, ByteOff[i] = offset of rune i
	Src     string
}

// Decode converts s into an Input.
func Decode(s string) *Input {
	in := &Input{Src: s}
	in.ByteOff = append(in.ByteOff, 0)
	for i := 0; i < len(s); {
		b := s[i]
		if b < utf8.RuneSelf {
			in.Runes = append(in.Runes, rune(b))
			in.Raw = append(in.Raw, 0)
			i++
		} else {
			r, size := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError && size == 1 {
				in.Runes = append(in.Runes, utf8.RuneError)
				in.Raw = append(in.Raw, b)
				i++
			} else {
				in.Runes = append(in.Runes, r)
				in.Raw = append(in.Raw, 0)
				i += size
			}
		}
		in.ByteOff = append(in.ByteOff, i)
	}
	return in
}

// Len returns the length in code points.
func (in *Input) Len() int { return len(in.Runes) }

// ByteAt maps a code-point index to a byte offset.
func (in *Input) ByteAt(pos int) int {
	if pos < 0 {
		return 0
	}
	if pos > len(in.Runes) {
		return len(in.Src)
	}
	return in.ByteOff[pos]
}

// Slice returns the source bytes between code-point positions.
func (in *Input) Slice(lo, hi int) string {
	if lo < 0 {
		lo = 0
	}
	if hi > len(in.Runes) {
		hi = len(in.Runes)
	}
	if lo >= hi {
		return ""
	}
	return in.Src[in.ByteOff[lo]:in.ByteOff[hi]]
}
