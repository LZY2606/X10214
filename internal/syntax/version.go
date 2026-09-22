// Package syntax implements the versioned grammar of the capture-trace
// executor: its own parser and AST, independent of any host regex library.
package syntax

import "unicode"

// SyntaxVersion pins the grammar accepted by this parser. Every matching
// result is reported together with this pin so that future grammar changes
// never silently reinterpret stored patterns or traces.
const SyntaxVersion = "capture-trace-syntax/1.0.0"

// UnicodeDataVersion pins the Unicode tables used by character classes.
// The tables are the ones shipped with the Go toolchain used to build the
// binary (unicode package); we surface the upstream Unicode release they
// correspond to as the data-version pin.
const UnicodeDataVersion = "Unicode/" + unicode.Version

// Versions returns the grammar and Unicode pins as a structured value.
type Versions struct {
	SyntaxVersion  string `json:"syntax_version"`
	UnicodeVersion string `json:"unicode_version"`
}

// VersionPins returns the current version pins.
func VersionPins() Versions {
	return Versions{SyntaxVersion: SyntaxVersion, UnicodeVersion: UnicodeDataVersion}
}
