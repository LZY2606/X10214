package engine

import (
	"testing"

	"traceroom/compile"
	"traceroom/syntax"
)

func mustProg(t *testing.T, src string) *compile.Program {
	t.Helper()
	p, err := syntax.Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	prog, err := compile.Compile(p, false)
	if err != nil {
		t.Fatalf("compile %q: %v", src, err)
	}
	return prog
}

func runMatch(t *testing.T, prog *compile.Program, in string, cfg Config) *Result {
	t.Helper()
	r := NewVM(prog, in, cfg).Run()
	return r
}

func TestBasicAndGreedy(t *testing.T) {
	prog := mustProg(t, `a(b|c)*d`)
	r := runMatch(t, prog, "xx abcbd d", Config{})
	if !r.Match {
		t.Fatalf("want match, status=%s", r.Status)
	}
	if r.Caps[1].Text != "b" {
		t.Fatalf("group1=%q want b (last iteration)", r.Caps[1].Text)
	}
}

func TestAlternationCaptureRollback(t *testing.T) {
	// branch 1 captures, fails later; branch 2 succeeds. Branch1 capture must leak nowhere.
	prog := mustProg(t, `(a)(z)|(ab)`)
	r := runMatch(t, prog, "ab", Config{})
	if !r.Match {
		t.Fatalf("want match")
	}
	if r.Caps[1].Participated || r.Caps[2].Participated {
		t.Fatalf("rolled-back groups leaked: %+v %+v", r.Caps[1], r.Caps[2])
	}
	if !r.Caps[3].Participated || r.Caps[3].Text != "ab" {
		t.Fatalf("group3=%+v want ab", r.Caps[3])
	}
}

func TestBackref(t *testing.T) {
	prog := mustProg(t, `(?P<y>ab|cd)\d\k<y>`)
	if r := runMatch(t, prog, "ab9ab", Config{}); !r.Match {
		t.Fatal("ab9ab should match")
	}
	if r := runMatch(t, prog, "ab9cd", Config{}); r.Match {
		t.Fatal("ab9cd should not match")
	}
}

func TestLookaround(t *testing.T) {
	prog := mustProg(t, `(?<=abc)(?!def)\w+`)
	if r := runMatch(t, prog, "abcxyz", Config{}); !r.Match {
		t.Fatal("abcxyz should match")
	}
	if r := runMatch(t, prog, "abcdef", Config{}); r.Match {
		t.Fatal("abcdef should not match (negative lookahead)")
	}
}

func TestZeroWidthTerminates(t *testing.T) {
	prog := mustProg(t, `(a*)*b`)
	r := runMatch(t, prog, "aaab", Config{MaxSteps: 100000})
	if r.Status == StatusIndeterminate {
		t.Fatalf("nested empty repetition did not terminate: %s", r.Reason)
	}
	if !r.Match {
		t.Fatalf("want match, got %s", r.Status)
	}
}

func TestEmptyMatch(t *testing.T) {
	prog := mustProg(t, `a*`)
	r := runMatch(t, prog, "b", Config{})
	if !r.Match {
		t.Fatal("a* should match empty at position 0")
	}
	if r.Start.Byte != 0 || r.End.Byte != 0 {
		t.Fatalf("empty match at %d-%d", r.Start.Byte, r.End.Byte)
	}
}

func TestCombiningAndInvalidUTF8(t *testing.T) {
	in := NewInput("e\u0301") // e + combining acute = 2 code points
	if in.NumCP != 2 {
		t.Fatalf("NumCP=%d want 2", in.NumCP)
	}
	invalid := NewInput("a\xffb")
	if invalid.NumCP != 3 {
		t.Fatalf("invalid utf8 NumCP=%d want 3", invalid.NumCP)
	}
	prog := mustProg(t, `.+`)
	r := runMatch(t, prog, "a\xffb", Config{})
	if !r.Match || r.End.CP != 3 {
		t.Fatalf("dot should span invalid byte: match=%v endCP=%d", r.Match, r.End.CP)
	}
}

func TestLazyQuantifier(t *testing.T) {
	prog := mustProg(t, `a+?b`)
	r := runMatch(t, prog, "aaab", Config{})
	if !r.Match || r.Start.Byte != 0 || r.End.Byte != 4 {
		t.Fatalf("lazy match wrong: %+v", r)
	}
}

func TestNamedCaptureRollbackInLoop(t *testing.T) {
	prog := mustProg(t, `(x|xy)*y`)
	r := runMatch(t, prog, "xyy", Config{})
	if !r.Match {
		t.Fatalf("want match status=%s", r.Status)
	}
}
