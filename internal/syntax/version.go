package syntax

import (
	"runtime"
	"unicode"
)

// GrammarVersion pins the pattern grammar understood by this engine.
const GrammarVersion = "ctre-regex/1.0.0"

// UnicodeVersion is the Unicode database version the engine classes use.
// It is taken from the Go standard library's unicode tables; the engine never
// calls regexp, so this is the single source of Unicode truth.
func UnicodeVersion() string { return unicode.Version }

// GoVersion reports the runtime that executed the build.
func GoVersion() string { return runtime.Version() }
