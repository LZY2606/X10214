package engine

import (
	"strings"
	"unicode"
)

// UnicodeDataVersion pins the Unicode character database used by class
// matching. The tables come from the Go standard library "unicode" package;
// this constant is taken from unicode.Version at build time and stamped on
// every result.
const UnicodeDataVersion = "unicode/" + unicode.Version

var categoryAlias = map[string]string{
	"L": "L", "LETTER": "L",
	"LC": "LC", "CASED_LETTER": "LC",
	"LU": "Lu", "UPPERCASE_LETTER": "Lu",
	"LL": "Ll", "LOWERCASE_LETTER": "Ll",
	"LT": "Lt", "TITLECASE_LETTER": "Lt",
	"LM": "Lm", "MODIFIER_LETTER": "Lm",
	"LO": "Lo", "OTHER_LETTER": "Lo",
	"M": "M", "MARK": "M", "COMBINING_MARK": "M",
	"MN": "Mn", "NONSPACING_MARK": "Mn",
	"MC": "Mc", "SPACING_MARK": "Mc",
	"ME": "Me", "ENCLOSING_MARK": "Me",
	"N": "N", "NUMBER": "N",
	"ND": "Nd", "DECIMAL_NUMBER": "Nd", "DECIMAL_DIGIT_NUMBER": "Nd",
	"NL": "Nl", "LETTER_NUMBER": "Nl",
	"NO": "No", "OTHER_NUMBER": "No",
	"P": "P", "PUNCT": "P", "PUNCTUATION": "P",
	"PC": "Pc", "CONNECTOR_PUNCTUATION": "Pc",
	"PD": "Pd", "DASH_PUNCTUATION": "Pd",
	"PS": "Ps", "OPEN_PUNCTUATION": "Ps", "START_PUNCTUATION": "Ps",
	"PE": "Pe", "CLOSE_PUNCTUATION": "Pe", "END_PUNCTUATION": "Pe",
	"PI": "Pi", "INITIAL_PUNCTUATION": "Pi",
	"PF": "Pf", "FINAL_PUNCTUATION": "Pf",
	"PO": "Po", "OTHER_PUNCTUATION": "Po",
	"S": "S", "SYMBOL": "S",
	"SM": "Sm", "MATH_SYMBOL": "Sm",
	"SC": "Sc", "CURRENCY_SYMBOL": "Sc",
	"SK": "Sk", "MODIFIER_SYMBOL": "Sk",
	"SO": "So", "OTHER_SYMBOL": "So",
	"Z": "Z", "SEPARATOR": "Z",
	"ZS": "Zs", "SPACE_SEPARATOR": "Zs",
	"ZL": "Zl", "LINE_SEPARATOR": "Zl",
	"ZP": "Zp", "PARAGRAPH_SEPARATOR": "Zp",
	"C": "C", "OTHER": "C",
	"CC": "Cc", "CONTROL": "Cc", "CNTRL": "Cc",
	"CF": "Cf", "FORMAT": "Cf",
	"CS": "Cs", "SURROGATE": "Cs",
	"CO": "Co", "PRIVATE_USE": "Co",
	"CN": "Cn", "UNASSIGNED": "Cn",
}

// lookupRangeTable resolves a \p{...} property name to a Unicode range
// table, searching general categories, scripts and properties.
func lookupRangeTable(name string) *unicode.RangeTable {
	key := strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(name, " ", "_"), "-", "_"))
	if cat, ok := categoryAlias[key]; ok {
		if rt, ok := unicode.Categories[cat]; ok {
			return rt
		}
	}
	if rt, ok := unicode.Categories[name]; ok {
		return rt
	}
	if rt, ok := unicode.Scripts[name]; ok {
		return rt
	}
	if rt, ok := unicode.Properties[name]; ok {
		return rt
	}
	return nil
}

func isASCIIWord(r rune) bool {
	return r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isASCIISpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', 0x0b, 0x0c:
		return true
	}
	return false
}
