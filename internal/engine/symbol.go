package engine

import "unicode/utf8"

// Sym is one consumed unit: either a valid Unicode code point or a single
// invalid UTF-8 byte. Positions 0..len(Syms) are slots between symbols.
type Sym struct {
	R       rune
	ByteLo  int // byte offset of the unit start in the original input
	ByteHi  int // byte offset just past the unit
	CPLo    int // number of valid code points before this unit
	Invalid bool
}

// Input is the indexed input. Every matching position carries both a byte
// offset and a Unicode code-point offset.
type Input struct {
	Syms []Sym
	raw  []byte
}

// DecodeInput splits raw bytes into symbols without replacement loss: every
// invalid byte becomes its own Sym flagged Invalid.
func DecodeInput(raw []byte) *Input {
	in := &Input{raw: raw}
	cp := 0
	for i := 0; i < len(raw); {
		r, sz := utf8.DecodeRune(raw[i:])
		if r == utf8.RuneError && sz == 1 {
			in.Syms = append(in.Syms, Sym{R: utf8.RuneError, ByteLo: i, ByteHi: i + 1,
				CPLo: cp, Invalid: true})
			i++
			continue
		}
		in.Syms = append(in.Syms, Sym{R: r, ByteLo: i, ByteHi: i + sz, CPLo: cp})
		cp++
		i += sz
	}
	return in
}

// Len returns the number of symbol positions.
func (in *Input) Len() int { return len(in.Syms) }

// BytePos maps a symbol position to a byte offset.
func (in *Input) BytePos(pos int) int {
	if pos <= 0 {
		return 0
	}
	if pos >= len(in.Syms) {
		if len(in.Syms) == 0 {
			return 0
		}
		return in.Syms[len(in.Syms)-1].ByteHi
	}
	return in.Syms[pos].ByteLo
}

// CPPos maps a symbol position to the count of valid code points before it.
func (in *Input) CPPos(pos int) int {
	if pos <= 0 {
		return 0
	}
	if pos >= len(in.Syms) {
		if len(in.Syms) == 0 {
			return 0
		}
		last := in.Syms[len(in.Syms)-1]
		c := last.CPLo
		if !last.Invalid {
			c++
		}
		return c
	}
	return in.Syms[pos].CPLo
}
