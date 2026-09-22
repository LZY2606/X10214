package vm

import "testing"

func TestSmoke(t *testing.T) {
	cases := []struct {
		pat, in string
		want    Status
	}{
		{`a`, "a", StatusMatch},
		{`a`, "b", StatusNoMatch},
		{`abc`, "xabc", StatusMatch},
		{`a*`, "", StatusMatch},
		{`(a|b)*c`, "ababc", StatusMatch},
		{`(a|ab)c`, "abc", StatusMatch},
		{`a+?a`, "aaa", StatusMatch},
		{`a{2,3}`, "aaaa", StatusMatch},
		{`^abc$`, "abc", StatusMatch},
		{`a(b)c\1`, "abc", StatusNoMatch},
		{`(a+)(b+)`, "aaabb", StatusMatch},
		{`(?=a)a`, "a", StatusMatch},
		{`(?!a)a`, "a", StatusNoMatch},
		{`(?<=ab)c`, "abc", StatusMatch},
		{`(?<!ab)c`, "abc", StatusNoMatch},
		{`(?<word>\w+)`, "hi", StatusMatch},
		{`(\w+) \1`, "abc abc", StatusMatch},
		{`a*?`, "", StatusMatch},
		{`(?:ab)+`, "abab", StatusMatch},
		{`(?i)abc`, "ABC", StatusMatch},
		{`a.c`, "axc", StatusMatch},
		{`a.c`, "a\nc", StatusNoMatch},
		{`(?s)a.c`, "a\nc", StatusMatch},
		{`(?m)^b`, "a\nb", StatusMatch},
		{`\d+`, "abc123", StatusMatch},
		{`[^\d]+`, "abc", StatusMatch},
	}
	for _, tc := range cases {
		run, err := MatchPattern(tc.pat, tc.in, DefaultBudgets())
		if err != nil {
			t.Fatalf("parse %q: %v", tc.pat, err)
		}
		if run.Status != tc.want {
			t.Errorf("pat=%q in=%q: got %s want %s", tc.pat, tc.in, run.Status, tc.want)
		}
	}
}
