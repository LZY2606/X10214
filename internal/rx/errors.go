package rx

import "fmt"

// SyntaxError describes a parse failure with a byte offset in the pattern.
type SyntaxError struct {
	Msg    string
	Offset int
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("syntax error at offset %d: %s", e.Offset, e.Msg)
}

// SemanticError describes a pattern that parses but is invalid (dangling
// backreference, duplicate group name, ...).
type SemanticError struct {
	Msg    string
	Offset int
}

func (e *SemanticError) Error() string {
	return fmt.Sprintf("semantic error at offset %d: %s", e.Offset, e.Msg)
}
