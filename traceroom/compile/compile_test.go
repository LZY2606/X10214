package compile

import (
	"testing"

	"traceroom/syntax"
)

func compileProg(t *testing.T, src string, opt bool) *Program {
	t.Helper()
	pat, err := syntax.Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	prog, err := Compile(pat, opt)
	if err != nil {
		t.Fatalf("compile %q: %v", src, err)
	}
	return prog
}

func TestCompileSmoke(t *testing.T) {
	for _, src := range []string{`a(b|c)*d`, `(?P<y>\d+)-\k<y>`, `(?<=x)(?!y)\w+?`, `a{2,4}?b+`} {
		p := compileProg(t, src, false)
		op := compileProg(t, src, true)
		if len(op.Code) > len(p.Code) {
			t.Fatalf("%q: optimizer grew code %d -> %d", src, len(p.Code), len(op.Code))
		}
	}
}
