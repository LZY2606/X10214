package regex

import "unicode/utf8"

// Atom is one matchable input unit: either a valid code point or a single
// raw invalid byte. Byte offsets and code point indices are both retained.
type Atom struct {
	R        rune
	Invalid  bool
	ByteLo   int // inclusive byte offset
	ByteHi   int // exclusive byte offset
	CP       int // code point index (invalid bytes each occupy a slot)
}

// Input is the decoded view used by the execution model.
type Input struct {
	Atoms []Atom
	Bytes []byte
	// CPStart[i] is the code point index of the atom at input index i.
}

// DecodeInput splits data into atoms without ever treating an invalid byte
// as a rune silently; every invalid byte becomes its own Atom.
func DecodeInput(data string) *Input {
	in := &Input{Bytes: []byte(data)}
	cp := 0
	for i := 0; i < len(data); {
		r, size := utf8.DecodeRuneInString(data[i:])
		if r == utf8.RuneError && size == 1 {
			in.Atoms = append(in.Atoms, Atom{
				R:       rune(data[i]),
				Invalid: true,
				ByteLo:  i,
				ByteHi:  i + 1,
				CP:      cp,
			})
			i++
			cp++
			continue
		}
		in.Atoms = append(in.Atoms, Atom{
			R:      r,
			ByteLo: i,
			ByteHi: i + size,
			CP:     cp,
		})
		i += size
		cp++
	}
	return in
}

func (in *Input) Len() int { return len(in.Atoms) }

// Loc is a zero-width or consuming position expressed in both coordinates.
type Loc struct {
	Index int
	Byte  int
	CP    int
}

func (in *Input) locAt(i int) Loc {
	if i >= len(in.Atoms) {
		b := len(in.Bytes)
		c := len(in.Atoms)
		return Loc{Index: i, Byte: b, CP: c}
	}
	a := in.Atoms[i]
	return Loc{Index: i, Byte: a.ByteLo, CP: a.CP}
}
