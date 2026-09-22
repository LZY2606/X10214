package rx

import (
	"testing"
)

func TestCombiningChars(t *testing.T) {
	// "é" = e + combining acute accent (U+0301): two code points, 3 bytes.
	in := "e\u0301"
	r := runMatch(t, `\w+`, in, ModeSearch)
	if r.Status != StatusMatch || r.MatchText != "e" {
		t.Fatalf("\\w got %q", r.MatchText)
	}
	ix := NewIdx(in)
	if ix.Len() != 2 || len(ix.Bytes) != 3 {
		t.Fatalf("idx len=%d bytes=%d", ix.Len(), len(ix.Bytes))
	}
	if got := ix.BytePos(1); got != 1 {
		t.Fatalf("byte pos of combining mark = %d, want 1", got)
	}
	if got := ix.RunePos(1); got != 1 {
		t.Fatalf("rune pos at byte 1 = %d, want 1", got)
	}
	if got := ix.RunePos(3); got != 2 {
		t.Fatalf("rune pos at end = %d, want 2", got)
	}
	// Match the full grapheme-transcoded sequence literally.
	r = runMatch(t, "e\u0301", in, ModeSearch)
	if r.Status != StatusMatch || r.MatchText != in {
		t.Fatalf("literal got %q", r.MatchText)
	}
	// CJK code point spans 3 bytes.
	r = runMatch(t, `好+`, "你好好x", ModeSearch)
	if r.Status != StatusMatch || r.MatchText != "好好" {
		t.Fatalf("cjk got %q", r.MatchText)
	}
	if r.StartByte != 3 || r.EndByte != 9 {
		t.Fatalf("cjk bytes %d-%d", r.StartByte, r.EndByte)
	}
}

func TestInvalidUTF8(t *testing.T) {
	// One valid byte, an invalid 0xFF byte, then "a".
	in := "a\xffa"
	ix := NewIdx(in)
	if ix.Len() != 3 || len(ix.Bytes) != 3 {
		t.Fatalf("invalid input idx len=%d", ix.Len())
	}
	if ix.Runes[1] != '\uFFFD' {
		t.Fatalf("invalid byte decoded as %U", ix.Runes[1])
	}
	r := runMatch(t, "a.a", in, ModeSearch)
	if r.Status != StatusMatch || r.MatchText != in {
		t.Fatalf("dot over invalid byte: %s %q", r.Status, r.MatchText)
	}
	// The invalid byte is presented to the executor as one U+FFFD element;
	// its raw byte offset stays queryable through the dual Idx.
	if got := ix.BytePos(1); got != 1 || ix.BytePos(2) != 2 {
		t.Fatalf("byte positions over invalid byte = %d,%d", ix.BytePos(1), ix.BytePos(2))
	}
	r = runMatch(t, `a\x{FFFD}a`, in, ModeSearch)
	if r.Status != StatusMatch || r.MatchText != in {
		t.Fatalf("invalid-element match got %q (%s)", r.MatchText, r.Status)
	}
	r = runMatch(t, `\x{FFFD}`, in, ModeSearch)
	if r.Status != StatusMatch || r.MatchText != "\xff" {
		t.Fatalf("replacement match got %q (%s)", r.MatchText, r.Status)
	}
	r = runMatch(t, `a.c`, in, ModeSearch)
	if r.Status != StatusNoMatch {
		t.Fatalf("should not match, got %q", r.MatchText)
	}
}

func TestEmptyMatchAdvance(t *testing.T) {
	r := runMatch(t, `a*`, "aa", ModeSearch)
	if r.Status != StatusMatch || r.MatchText != "aa" {
		t.Fatalf("a* got %q", r.MatchText)
	}
	// Zero-width loop inside a longer pattern.
	r = runMatch(t, `()*x`, "x", ModeSearch)
	if r.Status != StatusMatch {
		t.Fatalf("()*x status=%s", r.Status)
	}
	r = runMatch(t, `(a|)*b`, "b", ModeSearch)
	if r.Status != StatusMatch {
		t.Fatalf("(a|)*b status=%s", r.Status)
	}
	// Back-to-back quantifiers around empty body must terminate.
	r = runMatch(t, `(x*)*z`, "z", ModeSearch)
	if r.Status != StatusMatch {
		t.Fatalf("nested empty status=%s", r.Status)
	}
}
