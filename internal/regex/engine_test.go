package regex

import (
	"reflect"
	"strings"
	"testing"
)

type mc struct {
	pat, in string
	want    bool
}

func TestNestedQuantifiers(t *testing.T) {
	cases := []mc{
		{`(a*)*b`, "aaab", true},
		{`(a+)+b`, "aaab", true},
		{`(a*)*`, "aaa", true},
		{`(a*)*b`, "c", false},
		{`(x?)*y`, "y", true},
		{`(a|aa)+b`, "aaab", true},
		{`(a?){3}a`, "aaaa", true},
	}
	for _, c := range cases {
		r := runOne(t, c.pat, c.in)
		got := r.Status == StatusMatch
		if got != c.want {
			t.Fatalf("%q vs %q: got %v want %v (%s)", c.pat, c.in, got, c.want, r.Status)
		}
	}
}

func TestEmptyMatch(t *testing.T) {
	cases := []mc{
		{``, "abc", true},
		{`a*`, "", true},
		{`()x`, "x", true},
		{`|a`, "z", true},
		{`(a|)*`, "aa", true},
	}
	for _, c := range cases {
		r := runOne(t, c.pat, c.in)
		if (r.Status == StatusMatch) != c.want {
			t.Fatalf("%q vs %q: %s", c.pat, c.in, r.Status)
		}
	}
	r := runOne(t, `a*`, "baa")
	if r.Match.ByteLo != 0 || r.Match.ByteHi != 0 || r.Match.RuneLo != 0 {
		t.Fatalf("empty first match wrong: %+v", r.Match)
	}
}

func TestAlternationCaptureRollback(t *testing.T) {
	r := runOne(t, `(abc)|(abd)`, "abd")
	if r.Status != StatusMatch {
		t.Fatalf("status %s", r.Status)
	}
	if r.Captures[0].Matched {
		t.Fatalf("branch-1 capture leaked: %+v", r.Captures[0])
	}
	got2 := string([]byte("abd")[r.Captures[1].ByteLo:r.Captures[1].ByteHi])
	if got2 != "abd" {
		t.Fatalf("branch2 capture = %q", got2)
	}
	r = runOne(t, `(x)(y)?\2z`, "xz")
	if r.Status != StatusMatch {
		t.Fatalf("(x)(y)?\\2z on xz: %s", r.Status)
	}
}

func TestBackreference(t *testing.T) {
	cases := []mc{
		{`(abc|def)=\1`, "abc=abc", true},
		{`(abc|def)=\1`, "abc=def", false},
		{`(?P<w>\w+)-\k<w>`, "ab-ab", true},
		{`(a)(b)\2\1`, "abba", true},
		{`(a)(b)\2\1`, "abab", false},
		{`(?i)(abc)\1`, "abcABC", true},
	}
	for _, c := range cases {
		r := runOne(t, c.pat, c.in)
		if (r.Status == StatusMatch) != c.want {
			t.Fatalf("%q vs %q: got %s want %v", c.pat, c.in, r.Status, c.want)
		}
	}
}

func TestLookaround(t *testing.T) {
	cases := []mc{
		{`a(?=b)`, "ab", true},
		{`a(?!b)`, "ac", true},
		{`a(?!b)`, "ab", false},
		{`(?<=ab)c`, "abc", true},
		{`(?<!ab)c`, "xbc", true},
		{`(?<!ab)c`, "abc", false},
		{`\d+(?= dollars)`, "100 dollars", true},
		{`(?=(a))a\1`, "aa", true},
	}
	for _, c := range cases {
		r := runOne(t, c.pat, c.in)
		if (r.Status == StatusMatch) != c.want {
			t.Fatalf("%q vs %q: got %s want %v", c.pat, c.in, r.Status, c.want)
		}
	}
	r := runOne(t, `(?=(a))b|a`, "a")
	if r.Status != StatusMatch {
		t.Fatalf("status %s", r.Status)
	}
	if r.Captures[0].Matched {
		t.Fatalf("lookahead capture leaked into winning branch: %+v", r.Captures[0])
	}
}

func TestCombiningCharacters(t *testing.T) {
	in := []byte("e\u0301")
	r := runOne(t, `\w+`, string(in))
	if r.Status != StatusMatch || r.Match.RuneHi-r.Match.RuneLo != 2 ||
		r.Match.ByteHi-r.Match.ByteLo != len("e\u0301") {
		t.Fatalf("combining match: %+v", r.Match)
	}
	r = runOne(t, `(?<=e\u0301)x`, string(append(append([]byte{}, in...), 'x')))
	if r.Status != StatusMatch {
		t.Fatalf("lookbehind across combining marks: %s", r.Status)
	}
	pm := MapPositions(in)
	if len(pm.Runes) != 2 || pm.ByteToRune[1] != 1 {
		t.Fatalf("position map: %+v", pm)
	}
}

func TestInvalidUTF8(t *testing.T) {
	in := []byte{'a', 0xff, 'b'}
	r := runOne(t, `a.b`, string(in))
	if r.Status == StatusMatch {
		t.Fatalf("dot must not match invalid byte")
	}
	pm := MapPositions(in)
	if !pm.HasInvalid || len(pm.Runes) != 3 {
		t.Fatalf("invalid utf8 mapping: %+v", pm)
	}
	r = runOne(t, "a[\xff]b", string(in))
	if r.Status != StatusMatch {
		t.Fatalf("literal invalid byte class: %s", r.Status)
	}
	r = runOne(t, `a[^z]b`, string(in))
	if r.Status != StatusMatch {
		t.Fatalf("negated class vs invalid byte: %s", r.Status)
	}
}

func TestBudgetPauseResume(t *testing.T) {
	prog := mustBuild(t, `a*b`, false)
	in := []byte(strings.Repeat("a", 50000) + "b")
	small := Budget{MaxSteps: 1000, MaxStates: 100000, MaxSnapshots: 100000, MaxEvents: 200000}
	r := Search(prog, in, Options{Budget: small})
	if r.Status != StatusIndeterminate || r.Frontier == nil {
		t.Fatalf("want indeterminate with frontier, got %s", r.Status)
	}
	r2 := Resume(prog, in, r.Frontier, Options{Budget: Budget{
		MaxSteps: 5_000_000, MaxStates: 1_000_000, MaxSnapshots: 1_000_000, MaxEvents: 20}})
	if r2.Status != StatusMatch {
		t.Fatalf("resumed run: %s %s", r2.Status, r2.Reason)
	}
}

func TestStateSnapshotBudget(t *testing.T) {
	prog := mustBuild(t, `(a+)+b`, false)
	in := []byte(strings.Repeat("a", 300) + "c")
	small := Budget{MaxSteps: 1_000_000, MaxStates: 50, MaxSnapshots: 50, MaxEvents: 100000}
	r := Search(prog, in, Options{Budget: small})
	if r.Status != StatusIndeterminate {
		t.Fatalf("want indeterminate got %s", r.Status)
	}
	foundPrune := false
	for _, ev := range r.Events {
		if ev.Kind == EvPrune {
			foundPrune = true
		}
	}
	if !foundPrune {
		t.Fatalf("expected prune event")
	}
}

func TestStepAndBreakpoint(t *testing.T) {
	prog := mustBuild(t, `abc`, false)
	in := []byte("abc")
	r := Search(prog, in, Options{Budget: DefaultBudget(), StepLimit: 2})
	if r.Status != StatusPaused || r.Frontier == nil {
		t.Fatalf("want paused, got %s", r.Status)
	}
	r2 := Resume(prog, in, r.Frontier, Options{Budget: DefaultBudget(), StepLimit: 2})
	if r2.Status != StatusPaused {
		t.Fatalf("second pause: %s", r2.Status)
	}
	r3 := Resume(prog, in, r2.Frontier, Options{Budget: DefaultBudget()})
	if r3.Status != StatusMatch {
		t.Fatalf("final: %s", r3.Status)
	}
}

func TestOptimizerTrajectoryEquivalence(t *testing.T) {
	pats := []string{
		`(?:(ab))+|(?:x*)*c`, `((a)|(b))?\d+`, `(?:(?:(?:q)))`,
		`a(?:b|c)*d`, `(x|y)(?=z)\1`, `(?:||a)b?`,
	}
	inputs := []string{"", "a", "ab", "abab", "q", "xz", "abbd", "12", "yz", "aaab"}
	for _, pat := range pats {
		p0 := mustBuild(t, pat, false)
		p1 := mustBuild(t, pat, true)
		for _, in := range inputs {
			t0 := Search(p0, []byte(in), Options{Budget: DefaultBudget()})
			t1 := Search(p1, []byte(in), Options{Budget: DefaultBudget()})
			if t0.Status != t1.Status {
				t.Fatalf("status divergence %q on %q: %s vs %s", pat, in, t0.Status, t1.Status)
			}
			if t0.Status == StatusMatch && !reflect.DeepEqual(matchSig(t0), matchSig(t1)) {
				t.Fatalf("capture divergence %q on %q: %+v vs %+v", pat, in, matchSig(t0), matchSig(t1))
			}
			if !sameDecisionTrace(t0.Events, t1.Events) {
				t.Fatalf("trace divergence %q on %q", pat, in)
			}
		}
	}
}

func matchSig(r *Result) [][2]int {
	var out [][2]int
	out = append(out, [2]int{r.Match.ByteLo, r.Match.ByteHi})
	for _, c := range r.Captures {
		if c.Matched {
			out = append(out, [2]int{c.ByteLo, c.ByteHi})
		}
	}
	return out
}

func sameDecisionTrace(a, b []Event) bool {
	var fa, fb []string
	for _, e := range a {
		switch e.Kind {
		case EvBranch, EvFail, EvZeroStop, EvCapture, EvTry, EvMatch, EvNoMatch:
			fa = append(fa, string(e.Kind)+":"+e.Detail+":"+slotName(e))
		}
	}
	for _, e := range b {
		switch e.Kind {
		case EvBranch, EvFail, EvZeroStop, EvCapture, EvTry, EvMatch, EvNoMatch:
			fb = append(fb, string(e.Kind)+":"+e.Detail+":"+slotName(e))
		}
	}
	return reflect.DeepEqual(fa, fb)
}

func slotName(e Event) string {
	if e.Kind == EvCapture {
		return itoa(e.Slot)
	}
	return ""
}

func mustBuild(t *testing.T, pat string, opt bool) *Program {
	t.Helper()
	p, err := Build(pat, opt)
	if err != nil {
		t.Fatalf("build %q: %v", pat, err)
	}
	return p
}

func TestVersionsPinned(t *testing.T) {
	r := runOne(t, `a`, "a")
	if r.SyntaxVersion == "" || r.UnicodeVersion == "" {
		t.Fatalf("versions missing: %+v", r)
	}
}
