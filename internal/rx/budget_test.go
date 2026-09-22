package rx

import "testing"

func TestBudgetIndeterminateAndResume(t *testing.T) {
	// Catastrophic-ish pattern against a long non-matching suffix.
	pat := `(a+)+b`
	in := "aaaaaaaaaaaaaaaaaaaa"
	r, err := Run(pat, in, Options{Mode: ModeSearch, Budget: Budget{MaxSteps: 50, MaxStates: 100000, MaxSnapshots: 100000}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusIndeterminate {
		t.Fatalf("expected indeterminate, got %s", r.Status)
	}
	if len(r.Frontier) == 0 {
		t.Fatal("frontier payload missing")
	}
	// Restore from frontier and give enough budget; input truly has no b.
	_, prog, err := ParseAndCompile(pat)
	if err != nil {
		t.Fatal(err)
	}
	m2, err := UnmarshalMachine(r.Frontier)
	if err != nil {
		t.Fatal(err)
	}
	res := ResumeMachine(prog, m2, Budget{MaxSteps: 1000000, MaxStates: 1000000, MaxSnapshots: 1000000})
	if res.Status != StatusNoMatch {
		t.Fatalf("after resume expected no-match, got %s", res.Status)
	}

	// Same pattern with terminating b should eventually match on resume.
	in2 := "aaaaaaaaaaaab"
	r3, _ := Run(pat, in2, Options{Mode: ModeSearch, Budget: Budget{MaxSteps: 30, MaxStates: 100000, MaxSnapshots: 100000}})
	if r3.Status != StatusIndeterminate {
		t.Fatalf("expected indeterminate, got %s", r3.Status)
	}
	m3, _ := UnmarshalMachine(r3.Frontier)
	res3 := ResumeMachine(prog, m3, Budget{MaxSteps: 1000000, MaxStates: 1000000, MaxSnapshots: 1000000})
	if res3.Status != StatusMatch || res3.MatchText != in2 {
		t.Fatalf("resume match got %s %q", res3.Status, res3.MatchText)
	}
}

func TestSnapshotBudget(t *testing.T) {
	pat := `(a|aa|aaa)(b|bb)(c|cc)`
	in := "aaabbcc"
	r, err := Run(pat, in, Options{Mode: ModeFull, Budget: Budget{MaxSteps: 100000, MaxStates: 100000, MaxSnapshots: 5}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusIndeterminate || r.Reason != "budget:snapshots" {
		t.Fatalf("got %s reason=%s", r.Status, r.Reason)
	}
}

func TestBreakpointSingleStep(t *testing.T) {
	_, prog, err := ParseAndCompile(`a+b`)
	if err != nil {
		t.Fatal(err)
	}
	m := NewMachine(prog, "aaab", Config{Mode: ModeSearch, Breakpoints: []int{3}, MaxBehind: 256, TrailLimit: 500}, defaultBudget())
	ex := NewExecutor(prog, m)
	status := ex.RunUntil(0)
	if status != StatusPausedBreak || m.StopReason != "breakpoint" {
		t.Fatalf("expected paused, got %s/%s", status, m.StopReason)
	}
	// Resume clears the one-shot pause and finishes.
	res := ResumeMachine(prog, m, Budget{})
	if res.Status != StatusMatch || res.MatchText != "aaab" {
		t.Fatalf("after breakpoint got %s %q", res.Status, res.MatchText)
	}
}

func TestPinnedVersions(t *testing.T) {
	r, _ := Run(`a`, "a", Options{Mode: ModeSearch, Budget: defaultBudget()})
	if r.Grammar != SyntaxVersion || r.Unicode != UnicodeVersion {
		t.Fatalf("versions not pinned: %s %s", r.Grammar, r.Unicode)
	}
	if r.Machine != nil {
		t.Fatal("machine should not be retained by default")
	}
}
