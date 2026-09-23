package regex

import "unicode/utf8"

type Input struct {
	Text       string `json:"text"`
	Bytes      []byte `json:"-"`
	ByteToCP   []int  `json:"byte_to_cp"`
	CPToByte   []int  `json:"cp_to_byte"`
	CodePoints int    `json:"code_points"`
	ValidUTF8  bool   `json:"valid_utf8"`
}

func NewInput(text string) Input {
	b := []byte(text)
	byteToCP := make([]int, len(b)+1)
	for i := range byteToCP {
		byteToCP[i] = -1
	}
	cpToByte := []int{0}
	cp := 0
	pos := 0
	valid := true
	for pos < len(b) {
		r, size := utf8.DecodeRune(b[pos:])
		if r == utf8.RuneError && size == 1 {
			valid = false
			pos++
			continue
		}
		for j := 0; j < size; j++ {
			byteToCP[pos+j] = cp
		}
		cp++
		pos += size
		cpToByte = append(cpToByte, pos)
	}
	byteToCP[len(b)] = cp
	return Input{Text: text, Bytes: b, ByteToCP: byteToCP, CPToByte: cpToByte, CodePoints: cp, ValidUTF8: valid}
}

func (in Input) Decode(pos int) (rune, int) {
	if pos < 0 || pos >= len(in.Bytes) {
		return utf8.RuneError, 0
	}
	r, size := utf8.DecodeRune(in.Bytes[pos:])
	if r == utf8.RuneError && size == 1 {
		return -1, 1
	}
	return r, size
}

func (in Input) ByteToCodePoint(pos int) int {
	if pos < 0 || pos > len(in.Bytes) {
		return -1
	}
	return in.ByteToCP[pos]
}

func (in Input) CodePointToByte(cp int) int {
	if cp < 0 || cp > len(in.CPToByte) {
		return -1
	}
	return in.CPToByte[cp]
}
