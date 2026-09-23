package syntax

import "testing"

func TestSmoke(t *testing.T) {
	for _, p := range []string{`a(b*)?c`, `(?P<year>\d{4})-\k<year>`, `x(?=y)z|w(?<!q)`, `[[:alpha:]\p{Han}]+?`, `(a*)*`, `\x80.`, `(?i:Ab)c$`, `(a|b)\1`} {
		n, names, err := Parse(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		t.Logf("=== %s groups=%d names=%v", p, CountGroups(n), names)
		t.Log("\n" + n.SExpr())
	}
}
