package syntax

import "testing"

func TestParseShapes(t *testing.T) {
	cases := []struct {
		pattern string
		kind    Kind
	}{
		{`abc`, Concat},
		{`a|b`, Alternate},
		{`a*`, Repeat},
		{`(a)`, Group},
		{`[a-z]`, CharClass},
		{`.`, AnyChar},
		{`^a$`, Concat},
		{`(?=x)x`, Concat},
	}
	for _, tc := range cases {
		n, _, err := Parse(tc.pattern)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.pattern, err)
		}
		if n.Kind != tc.kind {
			t.Fatalf("parse %q: kind %v, want %v", tc.pattern, n.Kind, tc.kind)
		}
	}
}

func TestParseGroups(t *testing.T) {
	_, info, err := Parse(`(?P<a>x)(y)(?<z>w)`)
	if err != nil {
		t.Fatal(err)
	}
	if info.Groups != 3 {
		t.Fatalf("groups = %d, want 3", info.Groups)
	}
	if info.Names["a"] != 1 || info.Names["z"] != 3 {
		t.Fatalf("names = %v", info.Names)
	}
	if _, _, err := Parse(`(?P<a>x)(?P<a>y)`); err == nil {
		t.Fatalf("duplicate name accepted")
	}
}

func TestParseErrors(t *testing.T) {
	for _, p := range []string{`(`, `)`, `*a`, `[a`, `a{2,1}`, `\`, `(?P<>x)`, `a**`} {
		if _, _, err := Parse(p); err == nil {
			t.Fatalf("pattern %q: expected error", p)
		}
	}
}

func TestParseEscapes(t *testing.T) {
	n, _, err := Parse(`\d+\p{L}\x41\u0042\1`)
	if err != nil {
		t.Fatal(err)
	}
	if n.Kind != Concat || len(n.Children) != 5 {
		t.Fatalf("got %v with %d children", n.Kind, len(n.Children))
	}
	if n.Children[4].Kind != Backref || n.Children[4].Index != 1 {
		t.Fatalf("backref node = %+v", n.Children[4])
	}
}
