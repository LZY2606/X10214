package vm

import "unicode/utf8"

// Input holds raw bytes together with bidirectional byte <-> code-point cell
// indices. An invalid UTF-8 byte occupies one cell decoded as U+FFFD but
// retains width 1, so byte and cell positions stay independently queryable.
type Input struct {
	Bytes      []byte
	ByteToCell []int // byte offset -> cell index; len == len(Bytes)+1
	CellToByte []int // cell index -> start byte offset
	Widths     []int // width in bytes of each cell
	Runes      []rune
}

// NewInput indexes raw bytes.
func NewInput(b []byte) *Input {
	in := &Input{Bytes: b}
	in.ByteToCell = make([]int, len(b)+1)
	cell := 0
	for pos := 0; pos < len(b); {
		r, size := utf8.DecodeRune(b[pos:])
		in.ByteToCell[pos] = cell
		in.CellToByte = append(in.CellToByte, pos)
		in.Widths = append(in.Widths, size)
		in.Runes = append(in.Runes, r)
		for k := 1; k < size; k++ {
			in.ByteToCell[pos+k] = cell
		}
		cell++
		pos += size
	}
	in.ByteToCell[len(b)] = cell
	return in
}

// Cell returns the code-point cell index for a byte offset.
func (in *Input) Cell(bytePos int) int { return in.ByteToCell[bytePos] }

// Byte returns the byte offset where cell starts.
func (in *Input) Byte(cell int) int {
	if cell >= len(in.CellToByte) {
		return len(in.Bytes)
	}
	return in.CellToByte[cell]
}

// CellCount is the number of code-point cells.
func (in *Input) CellCount() int { return len(in.Runes) }

// RuneAt returns the rune starting at bytePos and its byte width.
func (in *Input) RuneAt(bytePos int) (rune, int) {
	if bytePos >= len(in.Bytes) {
		return 0, 0
	}
	r, size := utf8.DecodeRune(in.Bytes[bytePos:])
	return r, size
}

// IsBoundary reports whether bytePos is a cell start.
func (in *Input) IsBoundary(bytePos int) bool {
	if bytePos == 0 || bytePos == len(in.Bytes) {
		return true
	}
	return in.ByteToCell[bytePos] != in.ByteToCell[bytePos-1]
}

// NextBoundary returns byte offsets of all cell starts up to and including
// len(Bytes), in ascending order.
func (in *Input) cellBoundaries() []int {
	return in.CellToByte
}
