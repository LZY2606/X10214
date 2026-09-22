package rx

import (
	"strings"
	"testing"
)

func runMatch(t *testing.T, pat, in string, mode MatchMode) *Result {
	t.Helper()
	r, err := Run(pat, in, Options{Mode: mode, Budget: Budget{MaxSteps: 100000, MaxStates: 100000, MaxSnapshots: 100000}})
	if err != nil {
		t.Fatalf("parse %q: %v", pat, err)
	}
	return r
}

func TestGreedyBacktrack(t *testing.T) {
	r := runMatch(t, `a*a`, "aaa", ModeSearch)
	if r.Status != StatusMatch || r.MatchText != "aaa" {
		t.Fatalf("got %s %q", r.Status, r.MatchText)
	}
	r = runMatch(t, `a+?a`, "aaa", ModeSearch)
	if r.MatchText != "aa" {
		t.Fatalf("lazy got %q", r.MatchText)
	}
	r = runMatch(t, `".*"`, `"a"b"c"`, ModeSearch)
	if r.MatchText != `"a"b"c"` {
		t.Fatalf("greedy quote got %q", r.MatchText)
	}
	r = runMatch(t, `".*?"`, `"a"b"c"`, ModeSearch)
	if r.MatchText != `"a"` {
		t.Fatalf("lazy quote got %q", r.MatchText)
	}
}

func TestAlternationCaptureRollback(t *testing.T) {
	// First alternative captures and later gets undone by outer failure.
	r := runMatch(t, `(a)|(b)`, "b", ModeSearch)
	if r.Status != StatusMatch || r.Caps[1].Set || r.Caps[2].Text != "b" {
		t.Fatalf("alt caps: %+v", r.Caps)
	}
	// Failed branch group must not leak into a later successful attempt.
	r = runMatch(t, `(ab)|a`, "a", ModeSearch)
	if r.Status != StatusMatch || r.MatchText != "a" {
		t.Fatalf("got %q", r.MatchText)
	}
	if r.Caps[1].Set {
		t.Fatalf("group leaked: %+v", r.Caps[1])
	}
}

func TestQuantifierCaptureRestore(t *testing.T) {
	// (a)+b: final group should be the last 'a'.
	r := runMatch(t, `(a)+b`, "aaab", ModeSearch)
	if r.Status != StatusMatch || r.Caps[1].Text != "a" {
		t.Fatalf("caps %+v", r.Caps)
	}
	// Empty iteration must not leak prior body alternatives.
	r = runMatch(t, `(a?)*`, "z", ModeSearch)
	if r.Status != StatusMatch || r.MatchText != "" {
		t.Fatalf("got %q (%s)", r.MatchText, r.Status)
	}
	// Nested quantifiers.
	r = runMatch(t, `(a|b)+`, "ababx", ModeSearch)
	if r.MatchText != "abab" || r.Caps[1].Text != "b" {
		t.Fatalf("nested got %q g1=%q", r.MatchText, r.Caps[1].Text)
	}
	r = runMatch(t, `(x?)*`, "xx", ModeSearch)
	if r.Status != StatusMatch {
		t.Fatalf("nested optional status=%s", r.Status)
	}
}

func TestBackref(t *testing.T) {
	r := runMatch(t, `(ab|a)\1`, "abab", ModeSearch)
	if r.MatchText != "abab" {
		t.Fatalf("got %q", r.MatchText)
	}
	r = runMatch(t, `(ab|a)\1`, "aba", ModeSearch)
	if r.Status == StatusMatch && strings.HasPrefix(r.MatchText, "ab") && r.MatchText != "" {
		// "aba": group ab then needs ab -> fails; group a then a at pos1 -> "aa"? no.
	}
	if r.Status != StatusNoMatch {
		t.Fatalf("expected no-match, got %s %q", r.Status, r.MatchText)
	}
	r = runMatch(t, `(?P<w>\w+)-\k<w>`, "abc-abc", ModeSearch)
	if r.MatchText != "abc-abc" || r.Caps[1].Name != "w" {
		t.Fatalf("named backref got %q", r.MatchText)
	}
	r = runMatch(t, `(a)\1`, "Aa", ModeSearch)
	if r.Status != StatusNoMatch {
		t.Fatalf("case-sensitive backref should fail, got %q", r.MatchText)
	}
	r = runMatch(t, `(?i:(a)\1)`, "Aa", ModeSearch)
	if r.Status != StatusMatch {
		t.Fatalf("folded backref should match, got %s", r.Status)
	}
}

func TestLookaround(t *testing.T) {
	r := runMatch(t, `\d+(?= dollars)`, "100 dollars", ModeSearch)
	if r.MatchText != "100" {
		t.Fatalf("got %q", r.MatchText)
	}
	r = runMatch(t, `\d+(?!\d)`, "123abc", ModeSearch)
	if r.MatchText != "123" {
		t.Fatalf("neg looka got %q", r.MatchText)
	}
	r = runMatch(t, `(?<=\$)\d+`, "$42", ModeSearch)
	if r.MatchText != "42" {
		t.Fatalf("lookbehind got %q", r.MatchText)
	}
	r = runMatch(t, `(?<!\$)\d+`, "42", ModeSearch)
	if r.MatchText != "42" {
		t.Fatalf("neg behind got %q", r.MatchText)
	}
	// Lookahead captures are preserved on positive success.
	r = runMatch(t, `a(?=(b))`, "ab", ModeSearch)
	if r.Status != StatusMatch || r.Caps[1].Text != "b" {
		t.Fatalf("lookahead capture: %+v", r.Caps)
	}
	// Negative-lookahead captures that matched are rolled back.
	r = runMatch(t, `q(?!(x))x?`, "qx", ModeSearch)
	if r.Status != StatusMatch || (r.Caps[1].Set && r.Caps[1].Text == "x") {
		t.Fatalf("neg look capture leaked: %+v", r.Caps)
	}
}

func TestAnchors(t *testing.T) {
	if r := runMatch(t, `^\w+$`, "hello", ModeSearch); r.Status != StatusMatch {
		t.Fatal("anchored literal failed")
	}
	if r := runMatch(t, `^abc`, "xabc", ModeSearch); r.Status != StatusNoMatch {
		t.Fatal("^ should not match mid-input")
	}
	r := runMatch(t, `(?m:^b)`, "a\nb", ModeSearch)
	if r.Status != StatusMatch || r.MatchText != "b" {
		t.Fatalf("multiline ^ got %q", r.MatchText)
	}
	if r := runMatch(t, `\bword\b`, "(word)!", ModeSearch); r.Status != StatusMatch {
		t.Fatal("word boundary failed")
	}
	if r := runMatch(t, `foo\b`, "foobar", ModeSearch); r.Status != StatusNoMatch {
		t.Fatal("no boundary expected")
	}
}

func TestFullMatch(t *testing.T) {
	if r := runMatch(t, `a+`, "aaa", ModeFull); r.Status != StatusMatch {
		t.Fatal("full match should succeed")
	}
	if r := runMatch(t, `a+`, "aaab", ModeFull); r.Status != StatusNoMatch {
		t.Fatal("full match should fail with trailing b")
	}
}
