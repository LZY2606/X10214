package diff

import (
	"testing"

	"pairwise-gsb/internal/engine"
)

func TestCounterexamplesOrdering(t *testing.T) {
	out, err := Counterexamples(`a+`, `a*`, 3, engine.Budget{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Counterexamples) != 1 || out.Counterexamples[0] != "" {
		t.Fatalf("got %v, want [\"\"]", out.Counterexamples)
	}
	out, err = Counterexamples(`a|bb`, `ab`, 3, engine.Budget{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a", "ab", "bb"}
	if len(out.Counterexamples) != len(want) {
		t.Fatalf("got %v, want %v", out.Counterexamples, want)
	}
	for i := range want {
		if out.Counterexamples[i] != want[i] {
			t.Fatalf("got %v, want %v (length then lexicographic)", out.Counterexamples, want)
		}
	}
	if out.Note == "" {
		t.Fatalf("missing budget note")
	}
}

func TestEquivalentPatterns(t *testing.T) {
	out, err := Counterexamples(`a|ab`, `ab|a`, 4, engine.Budget{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Counterexamples) != 0 {
		t.Fatalf("got %v, want none", out.Counterexamples)
	}
}

func TestDiffNeverConfusesIndeterminate(t *testing.T) {
	// A tiny per-candidate budget forces indeterminate outcomes; those
	// candidates must be skipped, not counted as differences.
	out, err := Counterexamples(`(a|aa)+`, `(a|aa)+`, 3, engine.Budget{MaxSteps: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Counterexamples) != 0 {
		t.Fatalf("identical patterns produced %v", out.Counterexamples)
	}
}
