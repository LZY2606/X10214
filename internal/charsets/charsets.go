// Package charsets centralizes character-class semantics and pins the Unicode
// data edition used by the engine.
package charsets

import (
	"fmt"
	"unicode"
)

// GrammarVersion is the pinned regex grammar implemented by this module.
const GrammarVersion = "trajex-regex-grammar/v1"

// PinnedUnicodeVersion is the Unicode database edition the grammar was written
// against. Every result emitted by the engine records this value.
const PinnedUnicodeVersion = "15.0.0"

func init() {
	if unicode.Version != PinnedUnicodeVersion {
		panic(fmt.Sprintf(
			"charsets: toolchain Unicode version %q != pinned %q; regenerate internal/charsets tables",
			unicode.Version, PinnedUnicodeVersion))
	}
}

// UnicodeVersion returns the pinned Unicode data version.
func UnicodeVersion() string { return PinnedUnicodeVersion }

// IsASCIIDigit implements \d in grammar v1 (ASCII scoped).
func IsASCIIDigit(r rune) bool { return r >= '0' && r <= '9' }

// IsASCIIWord implements \w in grammar v1 (ASCII scoped).
func IsASCIIWord(r rune) bool {
	return r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '_'
}

// IsASCIISpace implements \s in grammar v1 (ASCII scoped):
// [\t\n\f\r ].
func IsASCIISpace(r rune) bool {
	switch r {
	case '\t', '\n', '\f', '\r', ' ':
		return true
	}
	return false
}

// Perl reports whether r belongs to a Perl class. kind is one of d,w,s (and
// their uppercase negations D,W,S are handled by the caller via negation).
func Perl(kind byte, negated bool, r rune) bool {
	var ok bool
	switch kind {
	case 'd':
		ok = IsASCIIDigit(r)
	case 'w':
		ok = IsASCIIWord(r)
	case 's':
		ok = IsASCIISpace(r)
	default:
		return false
	}
	return ok != negated
}

// UnicodeProperty reports membership in a Unicode general category (e.g. "L",
// "Lu", "Nd") or script (e.g. "Han", "Hiragana"). Unknown names return false
// and are rejected earlier by the parser via LookupUnicodeProperty.
func UnicodeProperty(name string, r rune) bool {
	if table := unicode.Categories[name]; table != nil {
		return unicode.Is(table, r)
	}
	if table := unicode.Scripts[name]; table != nil {
		return unicode.Is(table, r)
	}
	return false
}

// LookupUnicodeProperty reports whether name names a known category/script.
func LookupUnicodeProperty(name string) bool {
	return unicode.Categories[name] != nil || unicode.Scripts[name] != nil
}

// FoldRunes returns the ASCII/Unicode simple-fold closure containing r.
func FoldRunes(r rune) []rune {
	out := []rune{r}
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		out = append(out, f)
	}
	return out
}
