package regex

import (
	"strings"
	"testing"
)

func run(t *testing.T, pattern, input string) *Result {
	t.Helper()
	res, err := Execute(pattern, input, Options{}, nil)
	if err != nil {
		t.Fatalf("Execute(%q, %q): %v", pattern, input, err)
	}
	return res
}

func mustMatch(t *testing.T, pattern, input string) *Result {
	t.Helper()
	res := run(t, pattern, input)
	if res.Status != StatusMatch {
		t.Fatalf("pattern %q on %q: got %s, want match", pattern, input, res.Status)
	}
	return res
}

func mustNoMatch(t *testing.T, pattern, input string) *Result {
	t.Helper()
	res := run(t, pattern, input)
	if res.Status != StatusNoMatch {
		t.Fatalf("pattern %q on %q: got %s, want no-match", pattern, input, res.Status)
	}
	return res
}

func group(res *Result, idx int) Capture {
	for _, c := range res.Captures {
		if c.Index == idx {
			return c
		}
	}
	return Capture{Index: idx}
}

func groupByName(res *Result, name string) Capture {
	for _, c := range res.Captures {
		if c.Name == name {
			return c
		}
	}
	return Capture{Name: name}
}

func TestNestedQuantifiers(t *testing.T) {
	res := mustMatch(t, `(a|aa)+b`, "aaab")
	if res.Start != 0 || res.End != 4 {
		t.Fatalf("span = %d..%d", res.Start, res.End)
	}
	// catastrophic-ish backtracking terminates with a clear verdict
	mustNoMatch(t, `(a+)+b`, strings.Repeat("a", 12)+"c")
	// and is reported indeterminate under a tight step budget
	r, err := Execute(`(a+)+b`, strings.Repeat("a", 20)+"c",
		Options{Budget: Budget{MaxSteps: 500}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusIndeterminate {
		t.Fatalf("got %s, want indeterminate", r.Status)
	}
	if r.Frontier == nil {
		t.Fatal("indeterminate result must carry a frontier")
	}
}

func TestEmptyMatch(t *testing.T) {
	res := mustMatch(t, `a*`, "")
	if res.Start != 0 || res.End != 0 {
		t.Fatalf("empty match span = %d..%d", res.Start, res.End)
	}
	res = mustMatch(t, `(a*)*`, "")
	if res.End != 0 {
		t.Fatalf("span = %d..%d", res.Start, res.End)
	}
	// zero-width repeats must terminate
	mustMatch(t, `(a*)*b`, "aaab")
	mustMatch(t, `(?:x?)*y`, "y")
	res = mustMatch(t, `()*`, "")
	if res.Status != StatusMatch {
		t.Fatal("()* should match empty")
	}
}

func TestAlternationCaptureRollback(t *testing.T) {
	// branch 1 captures "a" then fails; branch 2 wins and the capture
	// from the abandoned branch must not leak.
	res := mustMatch(t, `(a)b|ac`, "ac")
	if g := group(res, 1); g.Set {
		t.Fatalf("group 1 leaked from pruned branch: %+v", g)
	}
	// left-to-right branch order: first branch wins when both match
	res = mustMatch(t, `(a)|a`, "a")
	if g := group(res, 1); !g.Set || g.Text != "a" {
		t.Fatalf("group 1 = %+v", g)
	}
	// capture made inside a repeated alternation rolls back correctly
	res = mustMatch(t, `(?:(a)|b)+c`, "abc")
	if g := group(res, 1); !g.Set || g.Text != "a" {
		t.Fatalf("group 1 = %+v", g)
	}
}

func TestBackreference(t *testing.T) {
	mustMatch(t, `(ab|cd)\1`, "abab")
	mustNoMatch(t, `(ab|cd)\1`, "abcd")
	res := mustMatch(t, `(?<x>foo)bar\k<x>`, "foobarfoo")
	if g := groupByName(res, "x"); !g.Set || g.Text != "foo" {
		t.Fatalf("named group = %+v", g)
	}
	// unset group backreference fails (documented semantics)
	mustNoMatch(t, `(a)?b\1`, "b")
	res = mustMatch(t, `(a)?b\1`, "aba")
	if g := group(res, 1); !g.Set || g.Text != "a" {
		t.Fatalf("group 1 = %+v", g)
	}
}

func TestLookaround(t *testing.T) {
	res := mustMatch(t, `foo(?=bar)`, "foobar")
	if res.End != 3 {
		t.Fatalf("lookahead consumed input: span %d..%d", res.Start, res.End)
	}
	mustNoMatch(t, `foo(?!bar)`, "foobar")
	mustMatch(t, `foo(?!bar)`, "foobaz")
	mustMatch(t, `(?<=foo)bar`, "foobar")
	mustNoMatch(t, `(?<=foo)bar`, "xxbar")
	mustNoMatch(t, `(?<!foo)bar`, "foobar")
	mustMatch(t, `(?<!foo)bar`, "xxbar")
	// captures inside a positive lookahead are kept
	res = mustMatch(t, `a(?=(b))`, "ab")
	if g := group(res, 1); !g.Set || g.Text != "b" {
		t.Fatalf("lookahead capture = %+v", g)
	}
	// captures inside a negative lookahead are discarded
	res = mustMatch(t, `a(?!(b))c`, "ac")
	if g := group(res, 1); g.Set {
		t.Fatalf("negative lookahead capture leaked: %+v", g)
	}
}

func TestCombiningChars(t *testing.T) {
	input := "é" // e + COMBINING ACUTE ACCENT
	res := mustMatch(t, `.`, input)
	if res.Start != 0 || res.End != 1 {
		t.Fatalf("rune span = %d..%d", res.Start, res.End)
	}
	if res.StartByte != 0 || res.EndByte != 1 {
		t.Fatalf("byte span = %d..%d", res.StartByte, res.EndByte)
	}
	res = mustMatch(t, `..$`, input)
	if res.End != 2 || res.EndByte != 3 {
		t.Fatalf("span runes=%d..%d bytes=%d..%d", res.Start, res.End, res.StartByte, res.EndByte)
	}
	// \p{Mn} uses the pinned Unicode tables
	res = mustMatch(t, `\p{Mn}`, input)
	if res.Start != 1 || res.StartByte != 1 {
		t.Fatalf("combining mark at rune %d byte %d", res.Start, res.StartByte)
	}
}

func TestInvalidUTF8(t *testing.T) {
	input := string([]byte{0xff, 'a'})
	res := mustMatch(t, `a`, input)
	if res.Start != 1 || res.StartByte != 1 {
		t.Fatalf("a at rune %d byte %d", res.Start, res.StartByte)
	}
	res = mustMatch(t, `..`, input)
	if res.End != 2 || res.EndByte != 2 {
		t.Fatalf("span runes=%d..%d bytes=%d..%d", res.Start, res.End, res.StartByte, res.EndByte)
	}
}

func TestBudgetPauseResume(t *testing.T) {
	pattern := `(a|aa)+b`
	input := strings.Repeat("a", 18) + "b"
	full := run(t, pattern, input)
	if full.Status != StatusMatch {
		t.Fatalf("uninterrupted run: %s", full.Status)
	}
	// phase 1: tiny budget -> indeterminate with frontier
	p1, err := Execute(pattern, input, Options{Budget: Budget{MaxSteps: 40}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p1.Status != StatusIndeterminate || p1.Frontier == nil {
		t.Fatalf("phase 1 = %s (frontier=%v)", p1.Status, p1.Frontier != nil)
	}
	// phase 2: resume from the frontier with a bigger budget
	p2, err := Execute(pattern, input, Options{Budget: Budget{MaxSteps: 100000}}, p1.Frontier)
	if err != nil {
		t.Fatal(err)
	}
	if p2.Status != full.Status || p2.Start != full.Start || p2.End != full.End {
		t.Fatalf("resumed = %s %d..%d, want %s %d..%d",
			p2.Status, p2.Start, p2.End, full.Status, full.Start, full.End)
	}
	for i := range full.Captures {
		if p2.Captures[i] != full.Captures[i] {
			t.Fatalf("capture %d differs: %+v vs %+v", i, p2.Captures[i], full.Captures[i])
		}
	}
}

func TestStateAndSnapshotBudgets(t *testing.T) {
	r, err := Execute(`(a?)*b`, "aaaaaaaaaaaaaaaaaaaaab",
		Options{Budget: Budget{MaxStates: 3}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusIndeterminate || !strings.Contains(r.Reason, "state budget") {
		t.Fatalf("state budget: %s %q", r.Status, r.Reason)
	}
	r, err = Execute(`(a|b)*c`, "ababababababc",
		Options{Budget: Budget{MaxSnapshots: 4}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusIndeterminate || !strings.Contains(r.Reason, "snapshot budget") {
		t.Fatalf("snapshot budget: %s %q", r.Status, r.Reason)
	}
	// indeterminate must never pretend to be no-match
	if r.Status == StatusNoMatch {
		t.Fatal("indeterminate masqueraded as no-match")
	}
}

func TestOptimizationTraceEquivalence(t *testing.T) {
	patterns := []string{
		`(?:a(?:b|c))(?:d){1,2}e`,
		`(a|b){1,3}c`,
		`(?:x|y)*(?:z)`,
		`(?:(?<n>a+)|b)+c`,
	}
	inputs := []string{"abde", "abdde", "abcde", "abbc", "xyxz", "aac", "bc", "zzz"}
	captureEvents := func(res *Result) []string {
		var out []string
		for _, ev := range res.Trace {
			if ev.Action == "capture" {
				out = append(out, ev.Detail)
			}
		}
		return out
	}
	for _, p := range patterns {
		for _, in := range inputs {
			plain, err := Execute(p, in, Options{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			opt, err := Execute(p, in, Options{Optimize: true}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if plain.Status != opt.Status || plain.Start != opt.Start || plain.End != opt.End {
				t.Fatalf("%q on %q: status/span differ (%s %d..%d vs %s %d..%d)",
					p, in, plain.Status, plain.Start, plain.End, opt.Status, opt.Start, opt.End)
			}
			if len(plain.Captures) != len(opt.Captures) {
				t.Fatalf("%q on %q: capture count differs", p, in)
			}
			for i := range plain.Captures {
				if plain.Captures[i] != opt.Captures[i] {
					t.Fatalf("%q on %q: capture %d differs", p, in, i)
				}
			}
			a, b := captureEvents(plain), captureEvents(opt)
			if strings.Join(a, "|") != strings.Join(b, "|") {
				t.Fatalf("%q on %q: capture history differs\n%v\n%v", p, in, a, b)
			}
		}
	}
}

func TestAnchorsAndClasses(t *testing.T) {
	mustMatch(t, `^abc$`, "abc")
	mustNoMatch(t, `^abc$`, "xabc")
	mustMatch(t, `^abc$`, "abc\n") // $ before trailing newline
	mustNoMatch(t, `\Abc\z`, "abc")
	mustMatch(t, `\Aabc\z`, "abc")
	mustMatch(t, `\bfoo\b`, "a foo b")
	mustNoMatch(t, `\Bfoo\B`, " foo ")
	mustMatch(t, `\d+`, "abc123")
	mustMatch(t, `[^\d]+`, "123abc")
	mustMatch(t, `[\w]+`, "_x9")
	mustMatch(t, `\p{Greek}+`, "αβγ")
	mustNoMatch(t, `\P{Greek}+`, "αβγ")
}

func TestLazyVsGreedyOrder(t *testing.T) {
	res := mustMatch(t, `a+`, "aaa")
	if res.End != 3 {
		t.Fatalf("greedy span = %d..%d", res.Start, res.End)
	}
	res = mustMatch(t, `a+?`, "aaa")
	if res.End != 1 {
		t.Fatalf("lazy span = %d..%d", res.Start, res.End)
	}
	res = mustMatch(t, `(a+?)(a*)`, "aaa")
	if g := group(res, 1); g.Text != "a" {
		t.Fatalf("lazy group = %q", g.Text)
	}
	if g := group(res, 2); g.Text != "aa" {
		t.Fatalf("group 2 = %q", g.Text)
	}
}

func TestVersionsPinned(t *testing.T) {
	res := run(t, `a`, "a")
	if res.SyntaxVersion != SyntaxVersion || res.SyntaxVersion == "" {
		t.Fatalf("syntax version = %q", res.SyntaxVersion)
	}
	if res.UnicodeVersion != UnicodeVersion || res.UnicodeVersion == "" {
		t.Fatalf("unicode version = %q", res.UnicodeVersion)
	}
}

func TestParseErrors(t *testing.T) {
	for _, bad := range []string{`(`, `a)`, `[a`, `a**`} {
		if _, _, err := Parse(bad); err == nil {
			t.Fatalf("pattern %q should fail to parse", bad)
		}
	}
	for _, bad := range []string{`\k<missing>`, `\3`, `(?<n>a)(?<n>b)`, `[z-a]`} {
		if _, _, err := Parse(bad); err == nil {
			t.Fatalf("pattern %q should fail to parse", bad)
		}
	}
}

func TestDiff(t *testing.T) {
	res, err := Diff(`a+`, `aa*`, 3, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Diffs) != 0 {
		t.Fatalf("a+ and aa* should be equivalent, got %v", res.Diffs)
	}
	res, err = Diff(`a|b`, `a`, 2, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Diffs) != 1 || res.Diffs[0].String != "b" || !res.Diffs[0].MatchA || res.Diffs[0].MatchB {
		t.Fatalf("diffs = %+v", res.Diffs)
	}
	// stable ordering: by length then lexicographic
	res, err = Diff(`a|bb|ab`, `a`, 2, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Diffs) != 2 || res.Diffs[0].String != "ab" || res.Diffs[1].String != "bb" {
		t.Fatalf("ordered diffs = %+v", res.Diffs)
	}
	if res.MaxLen != 2 || res.MaxResults != 10 {
		t.Fatalf("budget scope = %+v", res)
	}
	// result budget truncates
	res, err = Diff(`a|b|c`, `a`, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Diffs) != 1 || !res.Truncated {
		t.Fatalf("truncation = %+v", res)
	}
}
