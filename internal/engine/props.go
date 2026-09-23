package engine

import "unicode"

// lookupProperty resolves a Unicode general category ("Lu","P"), script
// ("Greek") or binary property ("White_Space") used by \p{...}.
func lookupProperty(name string) (*unicode.RangeTable, bool) {
	if t, ok := unicode.Categories[name]; ok {
		return t, true
	}
	if t, ok := unicode.Scripts[name]; ok {
		return t, true
	}
	if t, ok := unicode.Properties[canonicalPropName(name)]; ok {
		return t, true
	}
	return nil, false
}

// canonicalPropName covers a few widely used spellings.
func canonicalPropName(name string) string {
	switch name {
	case "whitespace":
		return "White_Space"
	case "alphabetic":
		return "Alphabetic"
	case "any", "Any":
		return "Any"
	}
	return name
}

// allRunes matches every valid code point (invalid UTF-8 bytes are presented as
// RuneError and never belong).
var allRunes = &unicode.RangeTable{
	R16: []unicode.Range16{
		{Lo: 0x0000, Hi: 0xD7FF, Stride: 1},
		{Lo: 0xE000, Hi: 0xFFFF, Stride: 1},
	},
	R32: []unicode.Range32{
		{Lo: 0x10000, Hi: 0x10FFFF, Stride: 1},
	},
	LatinOffset: 0,
}

var asciiRunes = &unicode.RangeTable{
	R16: []unicode.Range16{{Lo: 0x00, Hi: 0x7F, Stride: 1}},
}

func init() {
	unicode.Properties["Any"] = allRunes
	unicode.Properties["ASCII"] = asciiRunes
	unicode.Properties["ascii"] = asciiRunes
}
