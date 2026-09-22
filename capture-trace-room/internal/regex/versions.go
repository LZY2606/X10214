// Package regex implements the Capture Trace Room regular expression engine.
//
// It is a self-contained executor: parsing, AST construction, compilation and
// matching all run on the project's own explicit execution model. The standard
// library regexp package is never used to derive a match.
package regex

// SyntaxVersion pins the dialect understood by parser + virtual machine.
const SyntaxVersion = "CTR-RegEx-1.0"

// UnicodeDataVersion pins the Unicode tables used for \w \d \s and \p{...}.
const UnicodeDataVersion = "Unicode-16.0.0"
