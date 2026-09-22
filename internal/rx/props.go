package rx

import "unicode"

// binaryProps maps selected Unicode binary property aliases to stdlib tables.
// UnicodeVersion pinned in version.go documents the database revision.
var binaryProps = map[string]*unicode.RangeTable{
	"Alphabetic":                   unicode.Letter,
	"White_Space":                  unicode.White_Space,
	"ASCII":                        rangeFrom(0x00, 0x7F),
	"ASCII_Hex_Digit":              asciiHexTable(),
	"Hex_Digit":                    asciiHexTable(),
	"Lowercase":                    unicode.Lower,
	"Uppercase":                    unicode.Upper,
	"Noncharacter_Code_Point": unicode.Noncharacter_Code_Point,
}

func rangeFrom(lo, hi rune) *unicode.RangeTable {
	return &unicode.RangeTable{R16: []unicode.Range16{{Lo: uint16(lo), Hi: uint16(hi), Stride: 1}}}
}

func asciiHexTable() *unicode.RangeTable {
	return &unicode.RangeTable{R16: []unicode.Range16{
		{Lo: '0', Hi: '9', Stride: 1},
		{Lo: 'a', Hi: 'f', Stride: 1},
		{Lo: 'A', Hi: 'F', Stride: 1},
	}}
}
