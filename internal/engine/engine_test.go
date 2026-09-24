package engine

import (
	"strings"
	"testing"

	"pairwise-gsb/internal/syntax"
)

func compile(t *testing.T, pattern string, optimize bool) *Program {
	t.Helper()
	ast, info, err := syntax.Parse(pattern)
	if err != nil {
		t.Fatalf("parse %q: %v", pattern, err)
	}
	prog, err := Compile(ast, info)
	if err != nil {
		t.Fatalf("compile %q: %v", pattern, err)
	}
	if optimize {
		prog = Optimize(prog)
	}
	return prog
}

func run(t *testing.T, pattern, input string) (Result, []Event) {
	t.Helper()
	prog := compile(t, pattern, false)
	v := NewVM(prog, DecodeInput(input), Budget{}, nil)
	res := v.Run()
	return res, v.Events()
}

func mustMatch(t *testing.T, pattern, input string) Result {
	t.Helper()
	res, _ := run(t, pattern, input)
	if res.Status != StatusMatch {
		t.Fatalf("pattern %q on %q: got %s, want match", pattern, input, res.Status)
	}
	return res
}

func mustNoMatch(t *testing.T, pattern, input string) {
	t.Helper()
	res, _ := run(t, pattern, input)
	if res.Status != StatusNoMatch {
		t.Fatalf("pattern %q on %q: got %s, want no-match", pattern, input, res.Status)
	}
}

func TestNestedQuantifiersTerminate(t *testing.T) {
	// Zero-width-guarded nested loops must terminate.
	res, _ := run(t, `(a*)*b`, "aaab")
	if res.Status != StatusMatch {
		t.Fatalf("got %s", res.Status)
	}
	mustNoMatch(t, `(a*)*b`, "aaaaaaaaaac")
	mustNoMatch(t, `(a|a)*b`, "aaaaaaaaac")
	mustMatch(t, `(a+)+c`, "aaaac")
	mustNoMatch(t, `(a+)+$`, "aaab")
}

func TestEmptyMatch(t *testing.T) {
	res := mustMatch(t, `a*`, "bbb")
	if res.CapsCP[0] != 0 || res.CapsCP[1] != 0 {
		t.Fatalf("empty match caps = %v", res.CapsCP[:2])
	}
	res = mustMatch(t, `(a?)*`, "aa")
	if res.CapsCP[1] != 2 {
		t.Fatalf("(a?)* on aa: end = %d, want 2", res.CapsCP[1])
	}
	mustMatch(t, `()`, "x")
}

func TestAlternationCaptureRollback(t *testing.T) {
	// First branch sets group 2, fails later; the second branch must
	// not inherit the revoked capture.
	res, events := run(t, `(a(b)|ac)`, "ac")
	if res.Status != StatusMatch {
		t.Fatalf("got %s", res.Status)
	}
	if res.CapsCP[2] != 0 || res.CapsCP[3] != 2 {
		t.Fatalf("group1 = [%d,%d), want [0,2)", res.CapsCP[2], res.CapsCP[3])
	}
	if res.CapsCP[4] != -1 || res.CapsCP[5] != -1 {
		t.Fatalf("group2 leaked: [%d,%d)", res.CapsCP[4], res.CapsCP[5])
	}
	var sawSet, sawRollback bool
	for _, ev := range events {
		if ev.Kind == "capture-set" && strings.Contains(ev.Detail, "g2") {
			sawSet = true
		}
		if ev.Kind == "capture-rollback" && strings.Contains(ev.Detail, "g2") {
			sawRollback = true
		}
	}
	if !sawSet || !sawRollback {
		t.Fatalf("trace missing g2 set/rollback: set=%v rollback=%v", sawSet, sawRollback)
	}
	// Documented order: left branch preferred.
	res = mustMatch(t, `a|ab`, "ab")
	if res.CapsCP[1] != 1 {
		t.Fatalf("left-to-right preference violated: end = %d, want 1", res.CapsCP[1])
	}
	res = mustMatch(t, `(a)|(b)`, "b")
	if res.CapsCP[2] != -1 {
		t.Fatalf("group1 should be unset, got %d", res.CapsCP[2])
	}
	if res.CapsCP[4] != 0 || res.CapsCP[5] != 1 {
		t.Fatalf("group2 = [%d,%d), want [0,1)", res.CapsCP[4], res.CapsCP[5])
	}
}

func TestBackreference(t *testing.T) {
	mustMatch(t, `(ab)\1`, "abab")
	mustNoMatch(t, `(ab)\1`, "abac")
	res := mustMatch(t, `(?P<w>\w+) \k<w>`, "hey hey")
	if res.CapsCP[1] != 7 {
		t.Fatalf("named backref end = %d, want 7", res.CapsCP[1])
	}
	mustNoMatch(t, `^(?P<w>\w+) \k<w>$`, "hey you")
}

func TestLookaround(t *testing.T) {
	res := mustMatch(t, `a(?=b)`, "ab")
	if res.CapsCP[1] != 1 {
		t.Fatalf("lookahead consumed input: end = %d", res.CapsCP[1])
	}
	mustNoMatch(t, `a(?=b)`, "ac")
	mustMatch(t, `a(?!b)`, "ac")
	mustNoMatch(t, `a(?!b)`, "ab")
	res = mustMatch(t, `(?<=a)b`, "ab")
	if res.CapsCP[0] != 1 || res.CapsCP[1] != 2 {
		t.Fatalf("lookbehind caps = [%d,%d), want [1,2)", res.CapsCP[0], res.CapsCP[1])
	}
	mustMatch(t, `(?<!a)b`, "cb")
	mustNoMatch(t, `(?<!a)b`, "ab")
	mustMatch(t, `\d+(?=\D)`, "123abc")
	mustMatch(t, `foo(?=bar)(?!\d)`, "foobar")
}

func TestCombiningCharacters(t *testing.T) {
	// "é" as e + U+0301: dot matches only the base char.
	res := mustMatch(t, `.`, "é")
	if res.CapsCP[1] != 1 || res.CapsByte[1] != 1 {
		t.Fatalf("dot caps cp=%d byte=%d, want 1/1", res.CapsCP[1], res.CapsByte[1])
	}
	res = mustMatch(t, `\p{M}`, "é")
	if res.CapsCP[0] != 1 || res.CapsByte[0] != 1 {
		t.Fatalf("mark at cp=%d byte=%d, want 1/1", res.CapsCP[0], res.CapsByte[0])
	}
	mustMatch(t, `é`, "é")
	mustNoMatch(t, `é`, "é")
}

func TestInvalidUTF8(t *testing.T) {
	input := "a\xffb"
	in := DecodeInput(input)
	if in.Len() != 3 {
		t.Fatalf("decoded %d runes, want 3", in.Len())
	}
	if in.Runes[1] != '�' {
		t.Fatalf("rune 1 = %U, want U+FFFD", in.Runes[1])
	}
	prog := compile(t, `.`, false)
	v := NewVM(prog, in, Budget{}, nil)
	res := v.Run()
	if res.Status != StatusMatch || res.CapsByte[0] != 0 || res.CapsByte[1] != 1 {
		t.Fatalf("dot on invalid utf8: %v caps=%v", res.Status, res.CapsByte[:2])
	}
	// Byte and code-point positions must both be queryable around the
	// invalid byte.
	prog = compile(t, `b`, false)
	v = NewVM(prog, in, Budget{}, nil)
	res = v.Run()
	if res.Status != StatusMatch || res.CapsCP[0] != 2 || res.CapsByte[0] != 2 {
		t.Fatalf("b position cp=%d byte=%d, want 2/2", res.CapsCP[0], res.CapsByte[0])
	}
}

func TestBudgetPauseResume(t *testing.T) {
	pattern := `(a|b)*c`
	input := strings.Repeat("ab", 20) + "c"
	prog := compile(t, pattern, false)
	in := DecodeInput(input)

	full := NewVM(prog, in, Budget{}, nil)
	want := full.Run()
	if want.Status != StatusMatch {
		t.Fatalf("one-shot run: %s", want.Status)
	}

	v := NewVM(prog, in, Budget{MaxSteps: 25}, nil)
	res := v.Run()
	if res.Status != StatusIndeterminate {
		t.Fatalf("budgeted run: %s, want indeterminate", res.Status)
	}
	if res.Frontier == nil || res.Frontier.Depth() == 0 {
		t.Fatalf("indeterminate without frontier")
	}
	// Resume repeatedly with small step grants.
	events := v.Events()
	counters := v.Counters()
	frontier := res.Frontier
	for i := 0; i < 50 && res.Status == StatusIndeterminate; i++ {
		budget := Budget{MaxSteps: counters.Steps + 100}
		v = ResumeVM(prog, in, budget, counters, frontier, nil, events)
		res = v.Run()
		events = v.Events()
		frontier = res.Frontier
	}
	if res.Status != StatusMatch {
		t.Fatalf("resumed run: %s", res.Status)
	}
	if len(res.CapsCP) != len(want.CapsCP) {
		t.Fatalf("caps length mismatch")
	}
	for i := range want.CapsCP {
		if res.CapsCP[i] != want.CapsCP[i] {
			t.Fatalf("resumed caps %v != one-shot caps %v", res.CapsCP, want.CapsCP)
		}
	}
}

func TestIndeterminateIsNotNoMatch(t *testing.T) {
	prog := compile(t, `(a|aa)+b`, false)
	in := DecodeInput(strings.Repeat("a", 30))
	v := NewVM(prog, in, Budget{MaxSteps: 40}, nil)
	res := v.Run()
	if res.Status == StatusNoMatch {
		t.Fatalf("budget exhaustion masqueraded as no-match")
	}
	if res.Status != StatusIndeterminate || res.Frontier == nil {
		t.Fatalf("got %s, want indeterminate with frontier", res.Status)
	}
}

func TestStateAndSaveBudgets(t *testing.T) {
	prog := compile(t, `(a)(b)(c)`, false)
	in := DecodeInput("abc")
	v := NewVM(prog, in, Budget{MaxSaves: 3}, nil)
	if res := v.Run(); res.Status != StatusIndeterminate {
		t.Fatalf("save budget: %s", res.Status)
	}
	v = NewVM(prog, in, Budget{MaxStates: 1}, nil)
	if res := v.Run(); res.Status != StatusMatch {
		t.Fatalf("no splits needed, got %s", res.Status)
	}
	prog = compile(t, `a?a?a?b`, false)
	v = NewVM(prog, in, Budget{MaxStates: 2}, nil)
	if res := v.Run(); res.Status != StatusIndeterminate {
		t.Fatalf("state budget: %s", res.Status)
	}
}

func TestOptimizationTraceEquivalence(t *testing.T) {
	cases := []struct{ pattern, input string }{
		{`(a|ab)(c|cd)x`, "acdx"},
		{`a*b+c?`, "aabbc"},
		{`(x|y)(1|2)?`, "y2"},
		{`(?:foo|bar)+baz`, "barfoobaz"},
		{`(a(b)|ac)`, "ac"},
	}
	for _, tc := range cases {
		raw := compile(t, tc.pattern, false)
		opt := Optimize(raw)
		in := DecodeInput(tc.input)
		v1 := NewVM(raw, in, Budget{}, nil)
		r1 := v1.Run()
		v2 := NewVM(opt, in, Budget{}, nil)
		r2 := v2.Run()
		if r1.Status != r2.Status {
			t.Fatalf("%q: status %s vs %s", tc.pattern, r1.Status, r2.Status)
		}
		for i := range r1.CapsCP {
			if r1.CapsCP[i] != r2.CapsCP[i] {
				t.Fatalf("%q: caps %v vs %v", tc.pattern, r1.CapsCP, r2.CapsCP)
			}
		}
		e1 := observable(v1.Events())
		e2 := observable(v2.Events())
		if len(e1) != len(e2) {
			t.Fatalf("%q: trace length %d vs %d", tc.pattern, len(e1), len(e2))
		}
		for i := range e1 {
			if e1[i] != e2[i] {
				t.Fatalf("%q: trace[%d] %q vs %q", tc.pattern, i, e1[i], e2[i])
			}
		}
	}
}

// observable projects events to the fields an optimization must
// preserve: kind, input position, and detail (raw pcs excluded).
func observable(events []Event) []string {
	out := make([]string, len(events))
	for i, ev := range events {
		out[i] = ev.Kind + "|" + itoa(ev.CP) + "|" + ev.Detail
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func TestNamedGroupsAndAnchors(t *testing.T) {
	res := mustMatch(t, `(?P<word>\w+)`, "abc 123")
	if res.CapsCP[2] != 0 || res.CapsCP[3] != 3 {
		t.Fatalf("named group caps = [%d,%d)", res.CapsCP[2], res.CapsCP[3])
	}
	mustMatch(t, `^abc$`, "abc")
	mustNoMatch(t, `^abc$`, "xabc")
	mustMatch(t, `\Aabc\z`, "abc")
	mustMatch(t, `\bbar\b`, "foo bar baz")
	mustNoMatch(t, `\bbar\b`, "foobar")
	mustMatch(t, `\Boo`, "foo")
}

func TestLazyQuantifiers(t *testing.T) {
	res := mustMatch(t, `a+?`, "aaa")
	if res.CapsCP[1] != 1 {
		t.Fatalf("lazy plus end = %d, want 1", res.CapsCP[1])
	}
	res = mustMatch(t, `<(.+?)>`, "<a><b>")
	if res.CapsCP[3] != 2 {
		t.Fatalf("lazy group end = %d, want 2", res.CapsCP[3])
	}
	res = mustMatch(t, `<(.+)>`, "<a><b>")
	if res.CapsCP[3] != 5 {
		t.Fatalf("greedy group end = %d, want 5", res.CapsCP[3])
	}
}

func TestBoundedRepeats(t *testing.T) {
	mustMatch(t, `a{2,3}`, "aa")
	mustMatch(t, `a{2,3}`, "aaa")
	mustNoMatch(t, `^a{2,3}$`, "a")
	mustNoMatch(t, `^a{2,3}$`, "aaaa")
	mustMatch(t, `(ab){2}`, "abab")
	mustMatch(t, `a{0,2}b`, "b")
}
