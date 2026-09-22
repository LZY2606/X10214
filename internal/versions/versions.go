// Package versions pins the grammar version and the Unicode data version
// that every result produced by the engine is associated with.
package versions

import "unicode"

const (
	// SyntaxVersion is the grammar dialect implemented by traceroom.
	SyntaxVersion = "traceroom-regex/1.0"

	// UnicodeVersion is the Unicode data version shipped with the Go toolchain
	// that compiled this binary (data tables of the stdlib unicode package).
	UnicodeVersion = unicode.Version
)

// Versions is embedded verbatim in every run result.
type Versions struct {
	Syntax  string `json:"syntax_version"`
	Unicode string `json:"unicode_version"`
}

// Current returns the pinned version pair.
func Current() Versions {
	return Versions{Syntax: SyntaxVersion, Unicode: UnicodeVersion}
}
