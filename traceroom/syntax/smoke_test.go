package syntax

import "testing"

func TestSmokeParse(t *testing.T) {
	cases := []struct {
		src  string
		caps int
	}{
		{`a(b|c)*d`, 1},
		{`(?P<year>\d{4})-\k<year>`, 1},
		{`(?<=abc)(?!def)[\w-]+`, 0},
		{`a*?b+?c{2,4}?`, 0},
		{`[[:alpha:]\d]+`, 0},
		{`\Qa+b*\E`, 0},
		{`(?ims-x:foo)`, 0},
	}
	for _, c := range cases {
		p, err := Parse(c.src)
		if err != nil {
			t.Fatalf("Parse(%q): %v", c.src, err)
		}
		if p.NumCaps != c.caps {
			t.Fatalf("Parse(%q): caps=%d want %d", c.src, p.NumCaps, c.caps)
		}
		if _, err := ASTJSON(p); err != nil {
			t.Fatal(err)
		}
	}
}
