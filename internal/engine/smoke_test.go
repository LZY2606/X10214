package engine

import "testing"

func TestSmoke(t *testing.T) {
	cases := []struct {
		re, s string
		want  bool
	}{
		{"a", "a", true}, {"a", "b", false}, {"a*", "aaa", true}, {"a*?", "aaa", true}, {"a+?b", "aaab", true},
		{"(a)|(ab)", "ab", true}, {"(a*)b", "b", true}, {"(a?)", "", true}, {"(a*)a", "aaa", true},
		{"(\\w+)\\1", "abab", true}, {"(?=a)a", "a", true}, {"(?!a)b", "b", true}, {"(?<=a)b", "ab", true}, {"(?<!a)b", "b", true},
		{"a.b", "a\xc3b", true}, {"[a-キ]", "b", true}, {"\\pL+", "Ab", true}, {"a$", "a", true}, {"^a$", "a", true},
	}
	for _, tc := range cases {
		p, err := Parse(tc.re)
		if err != nil {
			t.Fatal(tc.re, err)
		}
		r := Run(p, tc.s, DefaultBudget(), nil)
		if r.Matched != tc.want {
			t.Errorf("%s on %q=%v(%s),want %v", tc.re, tc.s, r.Status, r.Reason, tc.want)
		}
	}
}
