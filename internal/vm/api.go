package vm

import (
	"trajectoryroom/internal/syntax"
)

// MatchPattern parses, compiles and runs a pattern to a terminal result.
func MatchPattern(pattern, input string, b Budgets) (*Run, error) {
	p, err := syntax.Parse(pattern)
	if err != nil {
		return nil, err
	}
	prog := Compile(p)
	in := DecodeInput(input)
	run := NewRun(prog, in, b)
	run.RunToEnd()
	return run, nil
}

// MustProg parses and compiles, panicking on syntax errors (tests only).
func MustProg(pattern string) (*syntax.Pattern, *Program) {
	p, err := syntax.Parse(pattern)
	if err != nil {
		panic(err)
	}
	return p, Compile(p)
}

// Continue resumes an indeterminate run with a fresh budget allowance.
func (r *Run) Continue(b Budgets) Status {
	r.Budgets = b
	r.Status = "running"
	r.Frontier = nil
	return r.RunToEnd()
}
