package engine

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

func propertyTable(name string) (*unicode.RangeTable, string, error) {
	key := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), "_", "")
	aliases := map[string]string{
		"l": "letter", "lu": "lu", "ll": "ll", "lt": "lt", "lm": "lm", "lo": "lo",
		"m": "mark", "mn": "mn", "mc": "mc", "me": "me",
		"n": "number", "nd": "nd", "nl": "nl", "no": "no",
		"p": "punct", "pc": "pc", "pd": "pd", "ps": "ps", "pe": "pe", "pi": "pi", "pf": "pf", "po": "po",
		"s": "symbol", "sm": "sm", "sc": "sc", "sk": "sk", "so": "so",
		"z": "separator", "zs": "zs", "zl": "zl", "zp": "zp",
		"c": "control", "cc": "cc", "cf": "cf", "cs": "cs", "co": "co", "cn": "cn",
		"any": "any", "ascii": "ascii", "latin": "latin",
	}
	if canonical, ok := aliases[key]; ok {
		key = canonical
	}
	if key == "letter" {
		return unicode.Letter, "Letter", nil
	}
	if key == "mark" {
		return unicode.Mark, "Mark", nil
	}
	if key == "number" {
		return unicode.Number, "Number", nil
	}
	if key == "punct" {
		return unicode.Punct, "Punct", nil
	}
	if key == "symbol" {
		return unicode.Symbol, "Symbol", nil
	}
	if key == "separator" {
		return &unicode.RangeTable{R16: []unicode.Range16{{Lo: 0x20, Hi: 0x20, Stride: 1}, {Lo: 0xa0, Hi: 0xa0, Stride: 1}, {Lo: 0x1680, Hi: 0x1680, Stride: 1}, {Lo: 0x2028, Hi: 0x2029, Stride: 1}, {Lo: 0x202f, Hi: 0x202f, Stride: 1}, {Lo: 0x205f, Hi: 0x205f, Stride: 1}, {Lo: 0x3000, Hi: 0x3000, Stride: 1}}}, "Separator", nil
	}
	if key == "control" {
		return unicode.C, "C", nil
	}
	if key == "any" {
		return &unicode.RangeTable{R16: []unicode.Range16{{Lo: 0, Hi: unicode.MaxLatin1, Stride: 1}}, R32: []unicode.Range32{{Lo: 256, Hi: unicode.MaxRune, Stride: 1}}, LatinOffset: 0}, "Any", nil
	}
	if key == "ascii" {
		return &unicode.RangeTable{R16: []unicode.Range16{{Lo: 0, Hi: 127, Stride: 1}}}, "ASCII", nil
	}
	if tbl, ok := unicode.Categories[canonicalCategory(key)]; ok {
		return tbl, canonicalCategory(key), nil
	}
	if tbl, ok := unicode.Scripts[key]; ok {
		return tbl, key, nil
	}
	if tbl, ok := unicode.Properties[key]; ok {
		return tbl, key, nil
	}
	keys := make([]string, 0)
	for k := range unicode.Categories {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return nil, "", fmt.Errorf("unsupported unicode property %q; examples: Letter, Greek, White_Space, %s", name, strings.Join(keys[:min(5, len(keys))], ", "))
}

func canonicalCategory(key string) string {
	long := map[string]string{
		"lu": "Lu", "ll": "Ll", "lt": "Lt", "lm": "Lm", "lo": "Lo",
		"mn": "Mn", "mc": "Mc", "me": "Me",
		"nd": "Nd", "nl": "Nl", "no": "No",
		"pc": "Pc", "pd": "Pd", "ps": "Ps", "pe": "Pe", "pi": "Pi", "pf": "Pf", "po": "Po",
		"sm": "Sm", "sc": "Sc", "sk": "Sk", "so": "So",
		"zs": "Zs", "zl": "Zl", "zp": "Zp",
		"cc": "Cc", "cf": "Cf", "cs": "Cs", "co": "Co", "cn": "Cn",
	}
	if v, ok := long[key]; ok {
		return v
	}
	if len(key) == 2 {
		return strings.ToUpper(key[:1]) + key[1:]
	}
	return key
}

func invertRangeTable(t *unicode.RangeTable) *unicode.RangeTable {
	marks := map[rune]bool{}
	for _, rr := range t.R16 {
		for r := rune(rr.Lo); r <= rune(rr.Hi); r += rune(rr.Stride) {
			marks[r] = true
		}
	}
	for _, rr := range t.R32 {
		for r := rune(rr.Lo); r <= rune(rr.Hi); r += rune(rr.Stride) {
			marks[r] = true
		}
	}
	var ranges []unicode.Range32
	var start rune = -1
	var last rune = -1
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if !marks[r] {
			if start < 0 {
				start = r
			}
			last = r
		} else if start >= 0 {
			ranges = append(ranges, unicode.Range32{Lo: uint32(start), Hi: uint32(last), Stride: 1})
			start = -1
		}
	}
	if start >= 0 {
		ranges = append(ranges, unicode.Range32{Lo: uint32(start), Hi: uint32(last), Stride: 1})
	}
	return &unicode.RangeTable{R32: ranges}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
