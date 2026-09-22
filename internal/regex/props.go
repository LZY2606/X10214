package regex

import (
	"strings"
	"unicode"
)

var unicodeCategories map[string]func(rune) bool
var unicodeScripts map[string]*unicode.RangeTable

func init() {
	unicodeCategories = map[string]func(rune) bool{}
	// General categories: L, Lu, Nd, ... plus long aliases.
	for name, t := range unicode.Categories {
		tab := t
		unicodeCategories[strings.ToLower(name)] = func(r rune) bool { return unicode.Is(tab, r) }
	}
	long := map[string]string{
		"letter":             "l",
		"casedletter":        "lc",
		"lowercaseletter":    "ll",
		"uppercaseletter":    "lu",
		"titlecaseletter":    "lt",
		"modifierletter":     "lm",
		"otherletter":        "lo",
		"mark":               "m",
		"nonspacingmark":     "mn",
		"spacingmark":        "mc",
		"enclosingmark":      "me",
		"number":             "n",
		"decimalnumber":      "nd",
		"letternumber":       "nl",
		"othernumber":        "no",
		"punctuation":        "p",
		"connectorpunct":     "pc",
		"dashpunct":          "pd",
		"openpunct":          "ps",
		"closepunct":         "pe",
		"initialpunct":       "pi",
		"finalpunct":         "pf",
		"otherpunct":         "po",
		"symbol":             "s",
		"mathsymbol":         "sm",
		"currencysymbol":     "sc",
		"modifiersymbol":     "sk",
		"othersymbol":        "so",
		"separator":          "z",
		"spaceseparator":     "zs",
		"lineseparator":      "zl",
		"paragraphseparator": "zp",
		"other":              "c",
		"control":            "cc",
		"format":             "cf",
		"surrogate":          "cs",
		"privateuse":         "co",
		"unassigned":         "cn",
	}
	for l, short := range long {
		if t, ok := unicode.Categories[strings.ToUpper(short)]; ok {
			tab := t
			unicodeCategories[l] = func(r rune) bool { return unicode.Is(tab, r) }
		}
	}
	// Property aliases.
	props := map[string]func(rune) bool{
		"white_space":     unicode.IsSpace,
		"alphabetic":      unicode.IsLetter,
		"lowercase":       unicode.IsLower,
		"uppercase":       unicode.IsUpper,
		"hex_digit":       isHexDigit,
		"ascii_hex_digit": isHexDigit,
	}
	for name, fn := range props {
		unicodeCategories[strings.ReplaceAll(name, "_", "")] = fn
	}
	unicodeScripts = map[string]*unicode.RangeTable{}
	for name, t := range unicode.Scripts {
		unicodeScripts[strings.ToLower(strings.ReplaceAll(name, "_", ""))] = t
	}
}

func isHexDigit(r rune) bool {
	return r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F'
}
