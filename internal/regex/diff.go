package regex

import (
	"fmt"
	"sort"
)

// DiffEntry is one counterexample: a string on which the two patterns
// disagree under full-match semantics.
type DiffEntry struct {
	String string `json:"string"`
	Length int    `json:"length"` // in runes
	MatchA bool   `json:"matchA"`
	MatchB bool   `json:"matchB"`
}

// DiffResult lists finite counterexamples ordered by (length,
// lexicographic) together with the budget scope of the search.
type DiffResult struct {
	PatternA       string      `json:"patternA"`
	PatternB       string      `json:"patternB"`
	Alphabet       string      `json:"alphabet"`
	MaxLen         int         `json:"maxLen"`
	MaxResults     int         `json:"maxResults"`
	Truncated      bool        `json:"truncated"`
	Explored       int         `json:"explored"`
	Diffs          []DiffEntry `json:"diffs"`
	SyntaxVersion  string      `json:"syntaxVersion"`
	UnicodeVersion string      `json:"unicodeVersion"`
}

const (
	diffDefaultMaxLen     = 4
	diffDefaultMaxResults = 50
	diffMaxAlphabet       = 6
	diffPerStringSteps    = 20000
)

// Diff compares two patterns under full-match semantics over a finite
// alphabet derived from both patterns, enumerating strings by length
// then lexicographically. The search is bounded by maxLen and
// maxResults; Truncated reports whether more counterexamples may
// exist beyond the budget.
func Diff(patternA, patternB string, maxLen, maxResults int) (*DiffResult, error) {
	rootA, infoA, err := Parse(patternA)
	if err != nil {
		return nil, fmt.Errorf("pattern A: %w", err)
	}
	rootB, infoB, err := Parse(patternB)
	if err != nil {
		return nil, fmt.Errorf("pattern B: %w", err)
	}
	if maxLen <= 0 {
		maxLen = diffDefaultMaxLen
	}
	if maxResults <= 0 {
		maxResults = diffDefaultMaxResults
	}
	alphabet := deriveAlphabet(rootA, rootB)
	progA := Compile(rootA, infoA, true)
	progB := Compile(rootB, infoB, true)

	res := &DiffResult{
		PatternA:       patternA,
		PatternB:       patternB,
		Alphabet:       string(alphabet),
		MaxLen:         maxLen,
		MaxResults:     maxResults,
		SyntaxVersion:  SyntaxVersion,
		UnicodeVersion: UnicodeVersion,
	}
	budget := Budget{MaxSteps: diffPerStringSteps, MaxStates: 4096, MaxSnapshots: 65536}

	buf := make([]rune, 0, maxLen)
	stopped := false
	var gen func(depth, target int) // enumerate strings of exactly target runes
	gen = func(depth, target int) {
		if stopped {
			return
		}
		if depth == target {
			if len(res.Diffs) >= maxResults {
				res.Truncated = true
				stopped = true
				return
			}
			s := string(buf)
			res.Explored++
			ma := fullMatch(progA, s, budget)
			mb := fullMatch(progB, s, budget)
			if ma != mb {
				res.Diffs = append(res.Diffs, DiffEntry{String: s, Length: len(buf), MatchA: ma, MatchB: mb})
			}
			return
		}
		for _, r := range alphabet {
			buf = append(buf, r)
			gen(depth+1, target)
			buf = buf[:len(buf)-1]
			if stopped {
				return
			}
		}
	}
	for l := 0; l <= maxLen && !stopped; l++ {
		gen(0, l)
	}
	return res, nil
}

func fullMatch(prog *Program, s string, b Budget) bool {
	vm := NewVM(prog, s, b, true)
	vm.Run()
	return vm.StatusOf() == StatusMatch
}

// deriveAlphabet collects literal runes and class boundary samples
// from both ASTs, padded with defaults, capped and sorted.
func deriveAlphabet(roots ...Node) []rune {
	set := map[rune]bool{}
	var walk func(n Node)
	walk = func(n Node) {
		switch t := n.(type) {
		case *Literal:
			set[t.R] = true
		case *Class:
			for _, it := range t.Items {
				if it.Table == "" {
					set[it.Lo] = true
					if it.Hi != it.Lo {
						set[it.Hi] = true
					}
				}
			}
		case *Concat:
			for _, s := range t.Nodes {
				walk(s)
			}
		case *Alt:
			for _, b := range t.Branches {
				walk(b)
			}
		case *Repeat:
			walk(t.Child)
		case *Group:
			walk(t.Child)
		case *Look:
			walk(t.Child)
		}
	}
	for _, r := range roots {
		walk(r)
	}
	for _, d := range []rune{'a', 'b', 'c', '0', '1'} {
		if len(set) >= diffMaxAlphabet {
			break
		}
		set[d] = true
	}
	var out []rune
	for r := range set {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	if len(out) > diffMaxAlphabet {
		out = out[:diffMaxAlphabet]
	}
	return out
}
