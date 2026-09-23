package regex

import (
	"fmt"
	"strings"
	"unicode"
)

// ItemKind identifies a character-class item kind.
type ItemKind string

const (
	ItemRange   ItemKind = "range"   // Lo..Hi inclusive
	ItemUnicode ItemKind = "unicode" // Unicode property (category/script/property)
)

// ClassItem is one item inside a [...] character class.
type ClassItem struct {
	Kind  ItemKind `json:"kind"`
	Lo    rune     `json:"-"`
	Hi    rune     `json:"-"`
	LoStr string   `json:"lo,omitempty"`
	HiStr string   `json:"hi,omitempty"`
	Prop  string   `json:"prop,omitempty"` // unicode property name
	Neg   bool     `json:"neg,omitempty"`  // \P{...}
	table *unicode.RangeTable
}

// Class is a parsed [...] character class (also used for \d \w \s \p{...}).
type Class struct {
	Negated bool         `json:"negated,omitempty"`
	Items   []*ClassItem `json:"items"`
}

// Match reports whether r is in the class. When fold is true, the simple
// case-folding closure of r is tested as well.
func (c *Class) Match(r rune, fold bool) bool {
	ok := false
	for _, it := range c.Items {
		if it.match(r, fold) {
			ok = true
			break
		}
	}
	if c.Negated {
		return !ok
	}
	return ok
}

func (it *ClassItem) match(r rune, fold bool) bool {
	switch it.Kind {
	case ItemRange:
		if it.Lo <= r && r <= it.Hi {
			return true
		}
		if fold {
			for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
				if it.Lo <= f && f <= it.Hi {
					return true
				}
			}
		}
		return false
	case ItemUnicode:
		ok := unicode.Is(it.table, r)
		if it.Neg {
			return !ok
		}
		return ok
	}
	return false
}

func rangeItem(lo, hi rune) *ClassItem {
	return &ClassItem{Kind: ItemRange, Lo: lo, Hi: hi, LoStr: string(lo), HiStr: string(hi)}
}

// perlClass builds the class for \d \D \s \S \w \W (ASCII definitions).
func perlClass(c rune) *Class {
	cls := &Class{}
	switch unicode.ToLower(c) {
	case 'd':
		cls.Items = []*ClassItem{rangeItem('0', '9')}
	case 'w':
		cls.Items = []*ClassItem{rangeItem('0', '9'), rangeItem('A', 'Z'), rangeItem('_', '_'), rangeItem('a', 'z')}
	case 's':
		cls.Items = []*ClassItem{rangeItem('\t', '\r'), rangeItem(' ', ' ')}
	}
	if c >= 'A' && c <= 'Z' {
		cls.Negated = true
	}
	return cls
}

var anyTable = &unicode.RangeTable{
	R16: []unicode.Range16{{Lo: 0, Hi: 0xffff, Stride: 1}},
	R32: []unicode.Range32{{Lo: 0x10000, Hi: 0x10ffff, Stride: 1}},
}

func unicodeTable(name string) *unicode.RangeTable {
	if name == "Any" {
		return anyTable
	}
	if t, ok := unicode.Categories[name]; ok {
		return t
	}
	if t, ok := unicode.Scripts[name]; ok {
		return t
	}
	if t, ok := unicode.Properties[name]; ok {
		return t
	}
	return nil
}

func isWordRune(r rune) bool {
	return r == '_' || ('0' <= r && r <= '9') || ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z')
}

// foldEqual reports rune equality under Unicode simple case folding.
func foldEqual(a, b rune) bool {
	if a == b {
		return true
	}
	for f := unicode.SimpleFold(a); f != a; f = unicode.SimpleFold(f) {
		if f == b {
			return true
		}
	}
	return false
}

func classFingerprint(c *Class) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%v[", c.Negated)
	for _, it := range c.Items {
		fmt.Fprintf(&sb, "%s:%d:%d:%s:%v;", it.Kind, it.Lo, it.Hi, it.Prop, it.Neg)
	}
	sb.WriteString("]")
	return sb.String()
}
