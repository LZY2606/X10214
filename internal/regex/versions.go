package regex

import "unicode"

// SyntaxVersion pins the grammar understood by this executor. It is attached to
// every parse/run/diff result so output is never interpreted under an unknown
// grammar.
const SyntaxVersion = "CT/1.0"

// UnicodeVersion pins the Unicode database used for categories, case folding
// and combining sequence handling. It comes from the linked Go standard
// library unicode tables (the project never links a host regexp engine).
const UnicodeVersion = unicode.Version
