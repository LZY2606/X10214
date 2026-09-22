package regex

import "testing"

func TestAnchors(t *testing.T) {
	cases := []mc{
		{`\Aabc\z`, "abc", true},
		{`\Aabc`, "xabc", false},
		{`abc\z`, "abcd", false},
		{`^abc$`, "abc\n", true},
		{`(?m)^b$`, "a\nb\nc", true},
		{`a\b`, "a b", true},
		{`a\Bb`, "ab", true},
		{`\bword\b`, "word", true},
	}
	for _, c := range cases {
		r := runOne(t, c.pat, c.in)
		if (r.Status == StatusMatch) != c.want {
			t.Fatalf("%q vs %q: %s", c.pat, c.in, r.Status)
		}
	}
}

func TestGreedyLazyOrder(t *testing.T) {
	// Greedy takes longest; lazy takes shortest, both leftmost.
	r := runOne(t, `a+`, "aaa")
	if r.Match.ByteHi-r.Match.ByteLo != 3 {
		t.Fatalf("greedy length = %d", r.Match.ByteHi-r.Match.ByteLo)
	}
	r = runOne(t, `a+?`, "aaa")
	if r.Match.ByteHi-r.Match.ByteLo != 1 {
		t.Fatalf("lazy length = %d", r.Match.ByteHi-r.Match.ByteLo)
	}
	// Preference ordering visible in trace: first split prefers repeat for
	// greedy and exit for lazy.
	prog := mustBuild(t, `a*`, false)
	res := Search(prog, []byte("aa"), Options{Budget: DefaultBudget()})
	var firstBranch *Event
	for i := range res.Events {
		if res.Events[i].Kind == EvBranch {
			firstBranch = &res.Events[i]
			break
		}
	}
	if firstBranch == nil || firstBranch.Detail == "" {
		t.Fatalf("missing branch event")
	}
}

func TestNamedGroupRoundtrip(t *testing.T) {
	r := runOne(t, `(?P<city>\w+),(?P<year>\d+)`, "Tokyo,2026")
	if r.Status != StatusMatch {
		t.Fatal(r.Status)
	}
	if string([]byte("Tokyo,2026")[r.Named["city"].ByteLo:r.Named["city"].ByteHi]) != "Tokyo" {
		t.Fatalf("city = %+v", r.Named["city"])
	}
	if string([]byte("Tokyo,2026")[r.Named["year"].ByteLo:r.Named["year"].ByteHi]) != "2026" {
		t.Fatalf("year = %+v", r.Named["year"])
	}
}

func TestFrontierResumeKeepsCapture(t *testing.T) {
	prog := mustBuild(t, `(a+)(b+)`, false)
	in := []byte("aaaaabbbbb")
	small := Budget{MaxSteps: 3, MaxStates: 1000, MaxSnapshots: 1000, MaxEvents: 100}
	r := Search(prog, in, Options{Budget: small})
	if r.Status != StatusPaused && r.Status != StatusIndeterminate {
		t.Fatalf("unexpected %s", r.Status)
	}
	r2 := Resume(prog, in, r.Frontier, Options{Budget: DefaultBudget()})
	if r2.Status != StatusMatch {
		t.Fatalf("resume = %s", r2.Status)
	}
	if string(in[r2.Captures[0].ByteLo:r2.Captures[0].ByteHi]) != "aaaaa" ||
		string(in[r2.Captures[1].ByteLo:r2.Captures[1].ByteHi]) != "bbbbb" {
		t.Fatalf("captures after resume: %+v", r2.Captures)
	}
}

func TestInvalidUTF8Positions(t *testing.T) {
	in := []byte{0x41, 0xc3, 0x28, 0x42} // invalid lead 0xc3 followed by '('
	pm := MapPositions(in)
	if !pm.HasInvalid || len(pm.Runes) != 4 {
		t.Fatalf("runes=%v invalid=%v", pm.Runes, pm.HasInvalid)
	}
	// byte_to_rune must be monotonic and distinguish each byte boundary.
	for i := 1; i < len(pm.ByteToRune); i++ {
		if pm.ByteToRune[i] < pm.ByteToRune[i-1] {
			t.Fatalf("byte_to_rune not monotonic: %v", pm.ByteToRune)
		}
	}
}

func TestParseErrors(t *testing.T) {
	bad := []string{`(`, `[`, `*`, `(?<x>`, `(?P<x>a)\k<y>`, `a{2,1}`, `(?:a)++`}
	for _, p := range bad {
		if _, err := Parse(p); err == nil {
			t.Fatalf("expected parse error for %q", p)
		}
	}
}
