package regex

// SyntaxVersion pins the pattern language implemented by this engine.
// Every parse / execution / diff result records it.
const SyntaxVersion = "tracer-regex/v1.0.0"

// UnicodeVersion pins the Unicode character database used for \p, \w (u flag),
// case folding and word boundaries. The tables are generated from the UCD
// files in internal/regex/ucd and frozen in uni_tables.go.
const UnicodeVersion = uniVersion
