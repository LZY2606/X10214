package regex

import "testing"

func TestDiffFindsCounterexamples(t *testing.T) {
	b := DefaultDiffBudget()
	b.Alphabet = "ab"
	b.MaxLength = 3
	r, err := Diff(`ab*`, `ab`, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Cases) == 0 {
		t.Fatalf("expected counterexamples")
	}
	// Stable ordering: by length then alphabet/dictionary order.
	for i := 1; i < len(r.Cases); i++ {
		a, c := r.Cases[i-1], r.Cases[i]
		if a.RuneLength > c.RuneLength {
			t.Fatalf("length order violated")
		}
		if a.RuneLength == c.RuneLength && a.Input > c.Input {
			t.Fatalf("lexicographic order violated: %q before %q", a.Input, c.Input)
		}
	}
	// "a" matches ab* (b*) but not ab; it is the shortest counterexample.
	if r.Cases[0].Input != "a" {
		t.Fatalf("first counterexample = %q want a", r.Cases[0].Input)
	}
	if r.SyntaxVersion == "" || r.UnicodeVersion == "" {
		t.Fatalf("versions missing")
	}
}

func TestDiffEquivalentPatterns(t *testing.T) {
	b := DefaultDiffBudget()
	b.Alphabet = "ab"
	b.MaxLength = 4
	r, err := Diff(`a|b`, `[ab]`, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Cases) != 0 {
		t.Fatalf("unexpected counterexamples: %+v", r.Cases[:min(3, len(r.Cases))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
