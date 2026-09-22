// Package unicodex wraps the Unicode tables the regex engine matches
// against. The data version is pinned on every result so that behaviour
// is reproducible across runs.
package unicodex

import "unicode"

// DataVersion is the Unicode edition backing all property lookups.
const DataVersion = unicode.Version

// InProperty reports whether r belongs to the named Unicode property.
// Supported names are general categories ("L", "Lu", "Nd", ...), scripts
// ("Greek", "Han", ...) and a handful of binary properties ("White_Space").
// Unknown names match nothing.
func InProperty(r rune, name string) bool {
	if table, ok := unicode.Categories[name]; ok {
		return unicode.Is(table, r)
	}
	if table, ok := unicode.Scripts[name]; ok {
		return unicode.Is(table, r)
	}
	if table, ok := unicode.Properties[name]; ok {
		return unicode.Is(table, r)
	}
	return false
}

// KnownProperty reports whether name resolves to a table.
func KnownProperty(name string) bool {
	if _, ok := unicode.Categories[name]; ok {
		return true
	}
	if _, ok := unicode.Scripts[name]; ok {
		return true
	}
	if _, ok := unicode.Properties[name]; ok {
		return true
	}
	return false
}

// IsWord reports whether r is a word character for \w and \b:
// ASCII letters, digits, underscore, or any Unicode letter/digit.
func IsWord(r rune) bool {
	if r < 128 {
		return r == '_' ||
			(r >= '0' && r <= '9') ||
			(r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z')
	}
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// IsDigit reports whether r is an ASCII digit (the \d of this dialect).
func IsDigit(r rune) bool { return r >= '0' && r <= '9' }

// IsSpace reports whether r is whitespace for \s.
func IsSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ':
		return true
	}
	return false
}

// FoldASCII maps ASCII letters to lowercase; other runes are unchanged.
// Case-insensitive matching in this dialect is ASCII-only by design.
func FoldASCII(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + ('a' - 'A')
	}
	return r
}
