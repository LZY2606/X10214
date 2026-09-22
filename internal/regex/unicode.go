package regex

import "unicode"

// UnicodeVersion is pinned into every result next to SyntaxVersion.
const UnicodeVersion = unicode.Version

// unicodeTables maps property names usable in \p{...} to range tables.
// "Word" is a derived table: letters, marks, decimal numbers and '_'.
var unicodeTables = map[string]*unicode.RangeTable{
	"L":           unicode.L,
	"Lu":          unicode.Lu,
	"Ll":          unicode.Ll,
	"Lt":          unicode.Lt,
	"Lm":          unicode.Lm,
	"Lo":          unicode.Lo,
	"M":           unicode.M,
	"Mn":          unicode.Mn,
	"Mc":          unicode.Mc,
	"Me":          unicode.Me,
	"N":           unicode.N,
	"Nd":          unicode.Nd,
	"Nl":          unicode.Nl,
	"No":          unicode.No,
	"P":           unicode.P,
	"S":           unicode.S,
	"Z":           unicode.Z,
	"C":           unicode.C,
	"White_Space": unicode.White_Space,
	"Greek":       unicode.Greek,
	"Latin":       unicode.Latin,
	"Han":         unicode.Han,
	"Hiragana":    unicode.Hiragana,
	"Katakana":    unicode.Katakana,
	"Cyrillic":    unicode.Cyrillic,
	"Word":        nil, // special-cased below
}

func isKnownTable(name string) bool {
	_, ok := unicodeTables[name]
	return ok
}

func inUnicodeTable(name string, r rune) bool {
	if name == "Word" {
		return unicode.Is(unicode.L, r) || unicode.Is(unicode.M, r) ||
			unicode.Is(unicode.N, r) || r == '_'
	}
	t, ok := unicodeTables[name]
	if !ok || t == nil {
		return false
	}
	return unicode.Is(t, r)
}

// isWordRune implements the \w definition used by \b and \B.
func isWordRune(r rune) bool { return inUnicodeTable("Word", r) }
