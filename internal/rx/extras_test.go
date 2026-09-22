package rx

import "testing"

func TestFlags(t *testing.T) {
	if r := runMatch(t, `(?i:abc)`, "ABC", ModeSearch); r.Status != StatusMatch {
		t.Fatal("fold group failed")
	}
	if r := runMatch(t, `(?i)abc`, "ABC", ModeSearch); r.Status != StatusMatch {
		t.Fatal("global fold failed")
	}
	if r := runMatch(t, `a.c`, "a\nc", ModeSearch); r.Status != StatusNoMatch {
		t.Fatal("dot should not cross \\n")
	}
	if r := runMatch(t, `(?s:a.c)`, "a\nc", ModeSearch); r.Status != StatusMatch {
		t.Fatal("dotall failed")
	}
	if r := runMatch(t, `a[B-C]`, "ab", ModeSearch); r.Status != StatusNoMatch {
		t.Fatal("class case sensitivity")
	}
	if r := runMatch(t, `(?i:a[b-c])`, "aC", ModeSearch); r.Status != StatusMatch {
		t.Fatal("folded class failed")
	}
}

func TestUnicodeProps(t *testing.T) {
	r := runMatch(t, `\p{Han}+`, "你好!", ModeSearch)
	if r.Status != StatusMatch || r.MatchText != "你好" {
		t.Fatalf("Han got %q", r.MatchText)
	}
	r = runMatch(t, `\p{L}+`, "naïve", ModeSearch)
	if r.MatchText != "naïve" {
		t.Fatalf("L got %q", r.MatchText)
	}
	r = runMatch(t, `\P{L}+`, "123!", ModeSearch)
	if r.MatchText != "123!" {
		t.Fatalf("P{L} got %q", r.MatchText)
	}
	r = runMatch(t, `[\p{Greek}\d]+`, "αβ3", ModeSearch)
	if r.MatchText != "αβ3" {
		t.Fatalf("greek class got %q", r.MatchText)
	}
	r = runMatch(t, `\p{Alphabetic}+`, "abc", ModeSearch)
	if r.Status != StatusMatch {
		t.Fatal("binary prop failed")
	}
}

func TestPOSIXClasses(t *testing.T) {
	r := runMatch(t, `[[:alpha:]]+`, "Ab12", ModeSearch)
	if r.MatchText != "Ab" {
		t.Fatalf("alpha got %q", r.MatchText)
	}
	r = runMatch(t, `[[:^digit:]]+`, "ab1", ModeSearch)
	if r.MatchText != "ab" {
		t.Fatalf("neg posix got %q", r.MatchText)
	}
	r = runMatch(t, `[[:space:]]+`, " \t", ModeSearch)
	if r.Status != StatusMatch {
		t.Fatal("space posix")
	}
}

func TestVariableLookbehind(t *testing.T) {
	// variable-length lookbehind chooses nearest start
	r := runMatch(t, `(?<=ab|a)x`, "ax", ModeSearch)
	if r.Status != StatusMatch || r.MatchText != "x" {
		t.Fatalf("var behind short got %q %s", r.MatchText, r.Status)
	}
	r = runMatch(t, `(?<=ab|a)x`, "abx", ModeSearch)
	if r.Status != StatusMatch {
		t.Fatalf("var behind long: %s", r.Status)
	}
	r = runMatch(t, `(?<!a)x`, "bx", ModeSearch)
	if r.Status != StatusMatch {
		t.Fatal("neg var behind")
	}
	// lookbehind empty alternative at pos itself
	r = runMatch(t, `(?<=|a)x`, "x", ModeSearch)
	if r.Status != StatusMatch {
		t.Fatal("empty behind alternative")
	}
}

func TestBoundedRepeatAndQuoted(t *testing.T) {
	r := runMatch(t, `a{2}`, "aaa", ModeSearch)
	if r.MatchText != "aa" {
		t.Fatalf("a{2} got %q", r.MatchText)
	}
	r = runMatch(t, `a{2,}?a`, "aaaa", ModeSearch)
	if r.MatchText != "aaa" {
		t.Fatalf("lazy bounded got %q", r.MatchText)
	}
	r = runMatch(t, `\Q.*+\E`, ".*+!", ModeSearch)
	if r.MatchText != ".*+" {
		t.Fatalf("quoted got %q", r.MatchText)
	}
}

func TestSyntaxErrors(t *testing.T) {
	bad := []string{`(`, `)`, `[a`, `a**`, `(?<x>a)`, `(\d)\5`, `(?z)`, `*`, `a{3,2}`, `\Qabc`}
	for _, b := range bad {
		if _, err := Parse(b); err == nil {
			t.Errorf("expected error for %q", b)
		}
	}
}

func TestNestedQuantDeep(t *testing.T) {
	r := runMatch(t, `(a*b*)*c`, "aababc", ModeSearch)
	if r.Status != StatusMatch || r.MatchText != "aababc" {
		t.Fatalf("nested quant got %q %s", r.MatchText, r.Status)
	}
}
