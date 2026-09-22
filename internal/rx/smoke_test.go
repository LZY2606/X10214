package rx

import "testing"

func TestSmoke(t *testing.T) {
	cases := []struct{ pat, in, want string }{
		{`abc`, "xabc", "abc"},
		{`a+`, "aaab", "aaa"},
		{`a+?`, "aaab", "a"},
		{`a{2,3}`, "aaaa", "aaa"},
		{`a*`, "b", ""},
		{`(a)(b)`, "ab", "ab"},
		{`(a|ab)(c)`, "abc", "abc"},
		{`(foo|fo)`, "foo", "foo"},
		{`a*(b)`, "b", "b"},
		{`(a)?\1`, "aa", "aa"},
		{`(?P<x>a)b\k<x>`, "aba", "aba"},
		{`a(?=b)`, "ab", "a"},
		{`a(?!b)`, "ac", "a"},
		{`(?<=a)b`, "ab", "b"},
		{`(?<!a)b`, "cb", "b"},
		{`^abc$`, "abc", "abc"},
		{`[A-Z]+`, "AbC", "A"},
		{`\w+`, "hi_1", "hi_1"},
	}
	for _, c := range cases {
		r, err := Run(c.pat, c.in, Options{Mode: ModeSearch})
		if err != nil {
			t.Errorf("%s on %s: parse error %v", c.pat, c.in, err)
			continue
		}
		if r.Status != StatusMatch || r.MatchText != c.want {
			t.Errorf("%s on %s: got %q (%s), want %q", c.pat, c.in, r.MatchText, r.Status, c.want)
		}
	}
}
