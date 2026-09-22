package vm

import "unicode/utf8"

// Input holds the string to search, indexed by both rune and byte positions.
// Invalid UTF-8 sequences become U+FFFD, each consuming the actual bytes,
// while rune positions still advance by one.
type Input struct {
	Runes    []rune `json:"runes"`
	RuneByte []int  `json:"runeByte"` // byte offset of each rune
	ByteLen  int    `json:"byteLen"`
	Raw      string `json:"-"`
}

// DecodeInput parses bytes tolerantly; invalid encoding is preserved enough
// to report byte offsets while matching proceeds rune by rune.
func DecodeInput(s string) *Input {
	in := &Input{Raw: s, ByteLen: len(s)}
	off := 0
	for off < len(s) {
		r, size := utf8.DecodeRuneInString(s[off:])
		in.RuneByte = append(in.RuneByte, off)
		in.Runes = append(in.Runes, r)
		off += size
	}
	in.RuneByte = append(in.RuneByte, len(s))
	return in
}

// ByteRange converts a rune interval to byte [lo,hi).
func (in *Input) ByteRange(runeLo, runeHi int) (int, int) {
	if runeLo < 0 || runeLo > len(in.Runes) {
		return 0, 0
	}
	if runeHi < 0 || runeHi > len(in.Runes) {
		return in.RuneByte[runeLo], in.ByteLen
	}
	return in.RuneByte[runeLo], in.RuneByte[runeHi]
}

func (in *Input) at(pos int) (rune, bool) {
	if pos < 0 || pos >= len(in.Runes) {
		return 0, false
	}
	return in.Runes[pos], true
}

func (in *Input) lenRunes() int { return len(in.Runes) }
