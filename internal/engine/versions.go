package engine

import "unicode"

// SyntaxVersion pins the exact grammar/execution semantics understood by this
// executor. Every match result records it so that a stored result can never be
// silently interpreted under a later grammar.
const SyntaxVersion = "CT-REGEX-1.0"

// UnicodeVersion pins the Unicode data set driving \w \d \s and \p{} classes.
// It is taken directly from the binary's unicode tables.
var UnicodeVersion = unicode.Version
