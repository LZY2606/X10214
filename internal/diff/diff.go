// Package diff computes bounded semantic differences between two
// patterns by enumerating candidate strings and checking membership
// with the project's own engine.
package diff

import (
	"fmt"
	"sort"

	"pairwise-gsb/internal/engine"
	"pairwise-gsb/internal/syntax"
)

// MaxCandidates caps total enumerated strings.
const MaxCandidates = 50000

// Outcome is the result of a bounded diff.
type Outcome struct {
	Counterexamples []string `json:"counterexamples"`
	Truncated       bool     `json:"truncated"`
	Alphabet        string   `json:"alphabet"`
	Note            string   `json:"note"`
}

func compileAnchored(pattern string) (*engine.Program, error) {
	ast, info, err := syntax.Parse(pattern)
	if err != nil {
		return nil, err
	}
	anchored := &syntax.Node{Kind: syntax.Concat, Children: []*syntax.Node{
		{Kind: syntax.Anchor, Anchor: syntax.AnchorTextStart},
		ast,
		{Kind: syntax.Anchor, Anchor: syntax.AnchorTextEnd},
	}}
	return engine.Compile(anchored, info)
}

func member(prog *engine.Program, s string, b engine.Budget) (bool, bool) {
	in := engine.DecodeInput(s)
	v := engine.NewVM(prog, in, b, nil)
	res := v.Run()
	if res.Status == engine.StatusIndeterminate {
		return false, false
	}
	return res.Status == engine.StatusMatch, true
}

func alphabetOf(asts ...*syntax.Node) []rune {
	seen := map[rune]bool{}
	var out []rune
	add := func(r rune) {
		if !seen[r] && len(out) < 6 {
			seen[r] = true
			out = append(out, r)
		}
	}
	var walk func(n *syntax.Node)
	walk = func(n *syntax.Node) {
		for _, r := range n.Lit {
			add(r)
		}
		for _, rg := range n.Ranges {
			add(rg.Lo)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	for _, a := range asts {
		walk(a)
	}
	add('a')
	add('b')
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Counterexamples lists strings (ordered by length, then
// lexicographically) that belong to exactly one of the two patterns,
// within the given length bound and per-candidate budget.
func Counterexamples(patA, patB string, maxLen int, perCandidate engine.Budget) (*Outcome, error) {
	astA, _, err := syntax.Parse(patA)
	if err != nil {
		return nil, fmt.Errorf("pattern A: %w", err)
	}
	astB, _, err := syntax.Parse(patB)
	if err != nil {
		return nil, fmt.Errorf("pattern B: %w", err)
	}
	progA, err := compileAnchored(patA)
	if err != nil {
		return nil, fmt.Errorf("pattern A: %w", err)
	}
	progB, err := compileAnchored(patB)
	if err != nil {
		return nil, fmt.Errorf("pattern B: %w", err)
	}
	alpha := alphabetOf(astA, astB)
	out := &Outcome{Alphabet: string(alpha)}
	if perCandidate.MaxSteps == 0 {
		perCandidate.MaxSteps = 100000
	}
	total := 0
	budgetLimited := false
	indices := make([]int, 0, maxLen)
	for length := 0; length <= maxLen && !out.Truncated; length++ {
		indices = indices[:0]
		for i := 0; i < length; i++ {
			indices = append(indices, 0)
		}
		for {
			var sb []rune
			for _, idx := range indices {
				sb = append(sb, alpha[idx])
			}
			cand := string(sb)
			total++
			if total > MaxCandidates {
				out.Truncated = true
				break
			}
			inA, okA := member(progA, cand, perCandidate)
			inB, okB := member(progB, cand, perCandidate)
			if !okA || !okB {
				budgetLimited = true
				continue
			}
			if inA != inB {
				out.Counterexamples = append(out.Counterexamples, cand)
			}
			// next string in lexicographic order
			i := length - 1
			for ; i >= 0; i-- {
				if indices[i]+1 < len(alpha) {
					indices[i]++
					break
				}
				indices[i] = 0
			}
			if i < 0 {
				break
			}
		}
	}
	out.Note = fmt.Sprintf("枚举长度 0..%d，字母表 %q，每候选步数预算 %d，共测试 %d 个候选",
		maxLen, string(alpha), perCandidate.MaxSteps, total)
	if budgetLimited {
		out.Note += "；部分候选因预算不足被跳过（indeterminate 不计入任何一侧）"
	}
	if out.Truncated {
		out.Note += "；候选总数超出上限，结果已截断"
	}
	return out, nil
}
