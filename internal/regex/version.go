// Package regex implements a self-contained regular expression engine:
// parser, AST, compiler and a backtracking executor with capture tracing.
// It never delegates matching to the host `regexp` package.
package regex

// SyntaxVersion pins the pattern syntax revision accepted by the parser.
const SyntaxVersion = "gsb-syntax/1.0"

// UnicodeVersion pins the Unicode data revision used for character
// classification (\w, \d, \s and case handling).
const UnicodeVersion = "Unicode-15.0.0"
