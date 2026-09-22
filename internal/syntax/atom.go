package syntax

import (
	"fmt"
	"unicode/utf8"
)

// dialectSemVersion is pinned on every match result.
const dialectSemVersion = "1.0.0"

// SemVersion returns the grammar/semantics version of the regex dialect.
func SemVersion() string { return dialectSemVersion }

func (p *parser) parseAtom(fl Flags) Node {
	c := p.src[p.i]
	switch c {
	case '(':
		return p.parseGroup(fl)
	case '[':
		return p.parseClass()
	case '.':
		p.i++
		return &DotNode{Offset: p.i - 1}
	case '^':
		p.i++
		return &AnchorNode{Offset: p.i - 1, Kind: AnchorStart}
	case '$':
		p.i++
		return &AnchorNode{Offset: p.i - 1, Kind: AnchorEnd}
	case ')', '|', '*', '+', '?', '{', '}':
		p.fail(p.i, fmt.Sprintf("unexpected %q", c))
		return nil
	case '\\':
		return p.parseEscape()
	default:
		return p.parseRune(c)
	}
}

func (p *parser) parseRune(first byte) Node {
	pos := p.i
	if first < utf8.RuneSelf {
		p.i++
		return &CharNode{Offset: pos, Rune: rune(first)}
	}
	r, size := utf8.DecodeRuneInString(p.src[p.i:])
	if r == utf8.RuneError && size == 1 {
		p.i++
		return &CharNode{Offset: pos, Rune: utf8.RuneError, Raw: p.src[pos]}
	}
	p.i += size
	return &CharNode{Offset: pos, Rune: r}
}

func (p *parser) parseGroup(fl Flags) Node {
	start := p.i
	p.i++ // consume '('
	if p.i >= len(p.src) {
		p.fail(start, "unterminated group")
		return nil
	}

	// Lookaround.
	if p.src[p.i] == '?' && p.i+1 < len(p.src) &&
		(p.src[p.i+1] == '=' || p.src[p.i+1] == '!' ||
			(p.i+2 < len(p.src) && p.src[p.i+1] == '<' &&
				(p.src[p.i+2] == '=' || p.src[p.i+2] == '!'))) {
		behind, negate := false, false
		if p.src[p.i+1] == '<' {
			behind = true
			negate = p.src[p.i+2] == '!'
			p.i += 3
		} else {
			negate = p.src[p.i+1] == '!'
			p.i += 2
		}
		body := p.parseAlternation(fl)
		if p.err != nil {
			return nil
		}
		if p.i >= len(p.src) || p.src[p.i] != ')' {
			p.fail(p.i, "unterminated lookaround")
			return nil
		}
		p.i++
		return &LookaroundNode{Offset: start, Behind: behind, Negate: negate, Body: body}
	}

	// Named group: (?P<name>...) or (?<name>...)
	if p.src[p.i] == '?' && p.i+2 < len(p.src) &&
		((p.src[p.i+1] == 'P' && p.src[p.i+2] == '<') || p.src[p.i+1] == '<') {
		nameStart := p.i + 3
		if p.src[p.i+1] == '<' {
			nameStart = p.i + 2
		}
		j := nameStart
		for j < len(p.src) && p.src[j] != '>' {
			j++
		}
		if j >= len(p.src) {
			p.fail(p.i, "unterminated group name")
			return nil
		}
		name := p.src[nameStart:j]
		if !validName(name) {
			p.fail(nameStart, "invalid group name")
			return nil
		}
		if _, dup := p.names[name]; dup {
			p.fail(nameStart, "duplicate group name "+name)
			return nil
		}
		p.groups++
		p.names[name] = p.groups
		p.i = j + 1
		body := p.parseAlternation(fl)
		if p.err != nil {
			return nil
		}
		if p.i >= len(p.src) || p.src[p.i] != ')' {
			p.fail(p.i, "unterminated named group")
			return nil
		}
		p.i++
		return &GroupNode{Offset: start, Index: p.groups, Name: name, Capture: true, Body: body}
	}

	// Non-capturing / flag groups.
	if p.src[p.i] == '?' {
		set, clear, mask, scoped, ok := p.parseFlagGroupStart()
		if !ok {
			return nil
		}
		nf := fl
		if mask.I {
			nf.I = set.I
		}
		if mask.S {
			nf.S = set.S
		}
		if mask.M {
			nf.M = set.M
		}
		if scoped {
			body := p.parseAlternation(nf)
			if p.err != nil {
				return nil
			}
			if p.i >= len(p.src) || p.src[p.i] != ')' {
				p.fail(p.i, "unterminated non-capturing group")
				return nil
			}
			p.i++
			return &GroupNode{Offset: start, Set: set, Clear: clear, Mask: mask,
				Capture: false, Body: body}
		}
		// Bare flag modifiers like (?i) are not part of this dialect; use
		// the scoped form (?i:...) instead.
		p.fail(p.i, "bare flag groups are not supported, use (?flags:...)")
		return nil
	}

	// Plain capturing group.
	p.groups++
	idx := p.groups
	body := p.parseAlternation(fl)
	if p.err != nil {
		return nil
	}
	if p.i >= len(p.src) || p.src[p.i] != ')' {
		p.fail(p.i, "unterminated capturing group")
		return nil
	}
	p.i++
	return &GroupNode{Offset: start, Index: idx, Capture: true, Body: body}
}

func validName(name string) bool {
	if name == "" {
		return false
	}
	for i, c := range name {
		if i == 0 && c >= '0' && c <= '9' {
			return false
		}
		if !(c == '_' || c == '-' ||
			(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}
