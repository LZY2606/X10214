package regex

// ElemInvalidBase encodes an invalid UTF-8 byte as a synthetic element code:
// code == ElemInvalidBase + b (0x110000..0x1100FF). Valid runes stay in
// 0..0x10FFFF, so they can never collide.
const ElemInvalidBase = 0x110000

// Input is the decoded subject. Positions used by the VM are *element*
// indices; byte and code-point offsets are both recoverable:
//
//	byte offset of element i : bytePos[i]
//	code point offset of i   : cpPos[i]
//
// Each invalid UTF-8 byte occupies one element and is also counted as one
// code-point slot (documented choice: the slot has scalar value U+FFFD-like
// synthetic code, never equal to any pattern rune).
type Input struct {
	Data      []byte
	Code      []uint32 // per element code
	ByteStart []int    // element index -> byte offset, len = n+1
	CpStart   []int    // element index -> code point offset, len = n+1
	ByteToIdx []int    // byte offset -> element index, len = len(data)+1
}

// NewInput decodes raw bytes without replacing invalid sequences.
func NewInput(data []byte) *Input {
	in := &Input{Data: append([]byte(nil), data...)}
	n := len(data)
	in.ByteToIdx = make([]int, n+1)
	bytePos, cpPos := 0, 0
	for i := 0; i < n; {
		in.ByteToIdx[i] = len(in.Code)
		in.ByteStart = append(in.ByteStart, bytePos)
		in.CpStart = append(in.CpStart, cpPos)
		r, size := decodeRune(data[i:])
		if r == 0xFFFD && size == 1 {
			in.Code = append(in.Code, ElemInvalidBase+uint32(data[i]))
			size = 1
		} else {
			in.Code = append(in.Code, uint32(r))
		}
		i += size
		bytePos += size
		cpPos++
	}
	in.ByteToIdx[n] = len(in.Code)
	in.ByteStart = append(in.ByteStart, bytePos)
	in.CpStart = append(in.CpStart, cpPos)
	return in
}

// Len returns the number of elements (runes plus invalid-byte slots).
func (in *Input) Len() int { return len(in.Code) }

// ByteOffset converts an element position to a UTF-8 byte offset.
func (in *Input) ByteOffset(pos int) int {
	if pos < 0 {
		return 0
	}
	if pos >= len(in.ByteStart) {
		return len(in.Data)
	}
	return in.ByteStart[pos]
}

// CpOffset converts an element position to a Unicode code-point offset.
func (in *Input) CpOffset(pos int) int {
	if pos < 0 {
		return 0
	}
	if pos >= len(in.CpStart) {
		return in.CpStart[len(in.CpStart)-1]
	}
	return in.CpStart[pos]
}

// PosFromByte maps a UTF-8 byte offset to the element position at that byte.
// Offsets inside a multibyte sequence are rounded back to its first byte.
func (in *Input) PosFromByte(b int) int {
	if b <= 0 {
		return 0
	}
	if b >= len(in.Data) {
		return len(in.Code)
	}
	p := in.ByteToIdx[b]
	if p < 0 {
		p = 0
	}
	return p
}

func decodeRune(b []byte) (r rune, size int) {
	switch {
	case b[0] < 0x80:
		return rune(b[0]), 1
	case b[0]&0xE0 == 0xC0:
		if len(b) < 2 || b[1]&0xC0 != 0x80 || b[0] < 0xC2 {
			return 0xFFFD, 1
		}
		return rune(b[0]&0x1F)<<6 | rune(b[1]&0x3F), 2
	case b[0]&0xF0 == 0xE0:
		if len(b) < 3 || b[1]&0xC0 != 0x80 || b[2]&0xC0 != 0x80 {
			return 0xFFFD, 1
		}
		r = rune(b[0]&0x0F)<<12 | rune(b[1]&0x3F)<<6 | rune(b[2]&0x3F)
		if (r >= 0xD800 && r <= 0xDFFF) || (b[0] == 0xE0 && b[1] < 0xA0) || (b[0] == 0xED && b[1] >= 0xA0) {
			return 0xFFFD, 1
		}
		return r, 3
	case b[0]&0xF8 == 0xF0:
		if len(b) < 4 || b[1]&0xC0 != 0x80 || b[2]&0xC0 != 0x80 || b[3]&0xC0 != 0x80 {
			return 0xFFFD, 1
		}
		r = rune(b[0]&0x07)<<18 | rune(b[1]&0x3F)<<12 | rune(b[2]&0x3F)<<6 | rune(b[3]&0x3F)
		if r > 0x10FFFF || (b[0] == 0xF0 && b[1] < 0x90) || (b[0] == 0xF4 && b[1] >= 0x90) {
			return 0xFFFD, 1
		}
		return r, 4
	}
	return 0xFFFD, 1
}
