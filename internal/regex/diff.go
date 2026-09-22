package regex

import (
	"sort"
	"unicode/utf8"
)

// DiffBudget bounds the finite counterexample search.
type DiffBudget struct {
	MaxLength int    `json:"max_length"`
	Alphabet  string `json:"alphabet"`
	MaxCases  int    `json:"max_cases"`
	PerMatch  Budget `json:"per_match"`
}

// DefaultDiffBudget is the documented envelope used by the API.
func DefaultDiffBudget() DiffBudget {
	return DiffBudget{
		MaxLength: 4,
		Alphabet:  "abc|()*?[]^$.\\ \t\n01",
		MaxCases:  50,
		PerMatch:  Budget{MaxSteps: 20_000, MaxStates: 5_000, MaxSnapshots: 5_000, MaxEvents: 200},
	}
}

// Counterexample is one input on which the two patterns disagree.
type Counterexample struct {
	Input      string `json:"input"`
	RuneLength int    `json:"rune_length"`
	ByteLength int    `json:"byte_length"`
	VerdictA   Status `json:"verdict_a"`
	VerdictB   Status `json:"verdict_b"`
}

// DiffResult summarizes a differential run.
type DiffResult struct {
	SyntaxVersion       string           `json:"syntax_version"`
	UnicodeVersion      string           `json:"unicode_version"`
	Cases               []Counterexample `json:"cases"`
	Budget              DiffBudget       `json:"budget"`
	Exhaustive          bool             `json:"exhaustive"`
	Enumerated          int              `json:"enumerated"`
	IndeterminateInputs []string         `json:"indeterminate_inputs,omitempty"`
	PatternA            string           `json:"pattern_a"`
	PatternB            string           `json:"pattern_b"`
}

// Diff searches for inputs (fullmatch semantics) where two patterns disagree,
// listing counterexamples stably: first by code point length, then by the
// documented alphabet order (dictionary order over rune indices).
func Diff(patternA, patternB string, budget DiffBudget) (*DiffResult, error) {
	pa, err := Build(patternA, false)
	if err != nil {
		return nil, &DiffError{Side: "A", Err: err}
	}
	pb, err := Build(patternB, false)
	if err != nil {
		return nil, &DiffError{Side: "B", Err: err}
	}
	return DiffPrograms(pa, pb, budget), nil
}

// DiffPrograms runs the differential search over compiled programs.
func DiffPrograms(pa, pb *Program, budget DiffBudget) *DiffResult {
	alpha := dedupeRunes([]rune(budget.Alphabet))
	sort.Slice(alpha, func(i, j int) bool { return alpha[i] < alpha[j] })
	res := &DiffResult{
		SyntaxVersion: SyntaxVersion, UnicodeVersion: UnicodeDataVersion,
		Budget: budget, PatternA: pa.Pattern, PatternB: pb.Pattern,
	}
	res.Exhaustive = true
	for length := 0; length <= budget.MaxLength; length++ {
		buf := make([]rune, length)
		var stop bool
		var enum func(pos int)
		enum = func(pos int) {
			if stop {
				return
			}
			if pos == length {
				res.Enumerated++
				s := string(buf)
				in := []byte(s)
				// Fullmatch: anchor with \A...\z wrapping is overkill; test the
				// match span instead.
				va := fullmatch(pa, in, budget.PerMatch)
				vb := fullmatch(pb, in, budget.PerMatch)
				if va == StatusIndeterminate || vb == StatusIndeterminate {
					res.Exhaustive = false
					if len(res.IndeterminateInputs) < 20 {
						res.IndeterminateInputs = append(res.IndeterminateInputs, s)
					}
					return
				}
				if va != vb {
					res.Cases = append(res.Cases, Counterexample{
						Input: s, RuneLength: length, ByteLength: len(in),
						VerdictA: va, VerdictB: vb,
					})
					if len(res.Cases) >= budget.MaxCases {
						stop = true
						res.Exhaustive = false
					}
				}
				return
			}
			for _, r := range alpha {
				buf[pos] = r
				enum(pos + 1)
				if stop {
					return
				}
			}
		}
		enum(0)
		if stop {
			break
		}
	}
	return res
}

// fullmatch reports match/no-match only when a match spans the whole input.
func fullmatch(prog *Program, in []byte, b Budget) Status {
	r := Search(prog, in, Options{Budget: b, Anchored: false})
	if r.Status == StatusMatch && r.Match.ByteLo == 0 && r.Match.ByteHi == len(in) {
		return StatusMatch
	}
	if r.Status == StatusMatch {
		return StatusNoMatch
	}
	return r.Status
}

func dedupeRunes(rs []rune) []rune {
	seen := map[rune]bool{}
	out := rs[:0]
	for _, r := range rs {
		if !utf8.ValidRune(r) || seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	return out
}

// DiffError tags which side failed to compile.
type DiffError struct {
	Side string
	Err  error
}

func (e *DiffError) Error() string { return "pattern " + e.Side + ": " + e.Err.Error() }
