package regex

import (
	"testing"
)

func runOne(t *testing.T, pat, in string) *Result {
	t.Helper()
	ast, err := Parse(pat)
	if err != nil {
		t.Fatalf("parse %q: %v", pat, err)
	}
	prog, err := Compile(ast, pat)
	if err != nil {
		t.Fatalf("compile %q: %v", pat, err)
	}
	return Search(prog, []byte(in), Options{Budget: DefaultBudget()})
}

func TestSmoke(t *testing.T) {
	cases := []struct{ pat, in string }{
		{"abc", "xabcx"},
		{"a*", "aaab"},
		{"a+?", "aaa"},
		{"(ab)|(cd)", "cd"},
		{"(a)(b)?\\2", "aa"},
		{"(?P<word>\\w+)", "hello"},
		{"(?=abc)abc", "abc"},
		{"(?!abc)abd", "abd"},
		{"(?<=ab)c", "abc"},
		{"(?<!ab)x", "ax"},
		{"a.c", "axc"},
		{"[\\p{L}]+", "héllo"},
		{"\\bword\\b", "a word here"},
		{"^(ab)$", "ab"},
	}
	for _, c := range cases {
		r := runOne(t, c.pat, c.in)
		if r.Status != StatusMatch {
			t.Errorf("pat %q in %q: status=%s reason=%s", c.pat, c.in, r.Status, r.Reason)
		}
	}
}

func TestNoMatch(t *testing.T) {
	r := runOne(t, "xyz", "abc")
	if r.Status != StatusNoMatch {
		t.Fatalf("want no_match got %s", r.Status)
	}
}
