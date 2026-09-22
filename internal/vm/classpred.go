package vm

import (
	"strings"
	"unicode"
)

// RuneRange is an inclusive code-point interval.
type RuneRange struct {
	Lo rune `json:"lo"`
	Hi rune `json:"hi"`
}

// ClassPred is a compiled, immutable character-class predicate.
type ClassPred struct {
	Ranges []RuneRange `json:"ranges"`
	// Classes holds Perl shortcuts ("d","D","s","S","w","W") and unicode
	// property tokens ("p:Letter", "P:Letter").
	Classes []string `json:"classes"`
	Negate  bool     `json:"negate"`
	Fold    bool     `json:"fold"`
}

// Match reports whether r belongs to the class.
func (c *ClassPred) Match(r rune) bool {
	ok := c.matchPositive(r)
	if c.Fold && !ok {

		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			if c.matchPositive(f) {
				ok = true
				break
			}
		}
	}
	if c.Negate {
		return !ok
	}
	return ok
}

func (c *ClassPred) matchPositive(r rune) bool {
	for _, rg := range c.Ranges {
		if r >= rg.Lo && r <= rg.Hi {
			return true
		}
	}
	for _, name := range c.Classes {
		if classTokenMatch(name, r) {
			return true
		}
	}
	return false
}

func classTokenMatch(tok string, r rune) bool {
	neg := false
	if strings.HasPrefix(tok, "P:") {
		neg = true
		tok = strings.TrimPrefix(tok, "P:")
	} else {
		tok = strings.TrimPrefix(tok, "p:")
	}
	var ok bool
	switch tok {
	case "d":
		ok = unicode.IsDigit(r)
	case "D":
		ok = !unicode.IsDigit(r)
	case "s":
		ok = isSpace(r)
	case "S":
		ok = !isSpace(r)
	case "w":
		ok = isWord(r)
	case "W":
		ok = !isWord(r)
	default:
		ok = unicodePropMatch(tok, r)
	}
	if neg {
		return !ok
	}
	return ok
}

func isWord(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func isSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x85, 0xA0:
		return true
	}
	return unicode.Is(unicode.White_Space, r)
}

func unicodePropMatch(name string, r rune) bool {
	switch name {
	case "L", "Letter":
		return unicode.IsLetter(r)
	case "Ll":
		return unicode.IsOneOf([]*unicode.RangeTable{unicode.Ll}, r)
	case "Lu":
		return unicode.IsOneOf([]*unicode.RangeTable{unicode.Lu}, r)
	case "Lt":
		return unicode.IsOneOf([]*unicode.RangeTable{unicode.Lt}, r)
	case "Lo":
		return unicode.IsOneOf([]*unicode.RangeTable{unicode.Lo}, r)
	case "Nd", "Number, Decimal Digit", "Decimal_Number":
		return unicode.IsDigit(r)
	case "N", "Number":
		return unicode.IsNumber(r)
	case "Z", "Separator":
		return unicode.Is(unicode.Z, r)
	case "Zs", "Space_Separator":
		return unicode.IsOneOf([]*unicode.RangeTable{unicode.Zs}, r)
	case "White_Space":
		return isSpace(r)
	case "P", "Punctuation":
		return unicode.IsPunct(r)
	case "C", "Other":
		return unicode.Is(unicode.C, r)
	case "S", "Symbol":
		return unicode.IsSymbol(r)
	case "Latin":
		return r <= 0x024F
	case "ASCII":
		return r < 0x80
	case "Any":
		return true
	default:
		return false
	}
}
