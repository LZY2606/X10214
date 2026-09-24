package engine

import "unicode/utf8"

// Slot is one input position element. A valid UTF-8 sequence maps to one
// slot; each invalid byte maps to its own slot carrying U+FFFD. Both byte
// and rune (code point) positions are recorded.
type Slot struct {
	R         rune `json:"r"`
	ByteStart int  `json:"byteStart"`
	ByteEnd   int  `json:"byteEnd"`
	RuneIdx   int  `json:"runeIdx"`
	Invalid   bool `json:"invalid"`
}

// Input is a decoded input string with dual position information.
type Input struct {
	Src   string `json:"src"`
	Slots []Slot `json:"slots"`
}

// DecodeInput decodes a (possibly invalid) UTF-8 string into slots.
func DecodeInput(s string) *Input {
	in := &Input{Src: s}
	bytePos := 0
	runePos := 0
	for bytePos < len(s) {
		r, sz := utf8.DecodeRuneInString(s[bytePos:])
		if r == utf8.RuneError && sz == 1 {
			in.Slots = append(in.Slots, Slot{R: utf8.RuneError, ByteStart: bytePos, ByteEnd: bytePos + 1, RuneIdx: runePos, Invalid: true})
		} else {
			in.Slots = append(in.Slots, Slot{R: r, ByteStart: bytePos, ByteEnd: bytePos + sz, RuneIdx: runePos})
		}
		bytePos += sz
		runePos++
	}
	return in
}

// BytePos converts a rune position (0..len(Slots)) to a byte position.
func (in *Input) BytePos(runePos int) int {
	if runePos <= 0 {
		return 0
	}
	if runePos >= len(in.Slots) {
		return len(in.Src)
	}
	return in.Slots[runePos].ByteStart
}
