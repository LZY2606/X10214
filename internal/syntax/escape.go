package syntax

import (
	"fmt"
	"strconv"
	"unicode/utf8"
)

func (p *parser) parseEscape() Node {
	pos := p.i
	p.i++ // consume backslash
	if p.i >= len(p.src) {
		p.fail(pos, "trailing backslash")
		return nil
	}
	c := p.src[p.i]
	switch c {
	case 'd', 'D', 'w', 'W', 's', 'S':
		p.i++
		return &ShorthandNode{Offset: pos, Kind: c}
	case 'p', 'P':
		neg := c == 'P'
		p.i++
		name, ok := p.readPropertyName(pos)
		if !ok {
			return nil
		}
		return &PropNode{Offset: pos, Name: name, Negate: neg}
	case 'b', 'B':
		p.i++
		k := AnchorWord
		if c == 'B' {
			k = AnchorNotWord
		}
		return &AnchorNode{Offset: pos, Kind: k}
	case 'k':
		p.i++
		name, ok := p.readAngleName(pos)
		if !ok {
			return nil
		}
		return &BackrefNode{Offset: pos, Name: name}
	case 'a':
		p.i++
		return &CharNode{Offset: pos, Rune: 7}
	case 'f':
		p.i++
		return &CharNode{Offset: pos, Rune: 12}
	case 'n':
		p.i++
		return &CharNode{Offset: pos, Rune: 10}
	case 'r':
		p.i++
		return &CharNode{Offset: pos, Rune: 13}
	case 't':
		p.i++
		return &CharNode{Offset: pos, Rune: 9}
	case 'v':
		p.i++
		return &CharNode{Offset: pos, Rune: 11}
	case '0':
		p.i++
		return &CharNode{Offset: pos, Rune: 0}
	case 'x':
		p.i++
		r, ok := p.readHex(pos, 2)
		if !ok {
			return nil
		}
		return &CharNode{Offset: pos, Rune: r}
	case 'u':
		p.i++
		r, ok := p.readHex(pos, 4)
		if !ok {
			return nil
		}
		return &CharNode{Offset: pos, Rune: r}
	case 'Q':
		return p.readQuoted(pos)
	default:
		if c >= '1' && c <= '9' {
			start := p.i
			end := start
			for end < len(p.src) && p.src[end] >= '0' && p.src[end] <= '9' {
				end++
			}
			n, _ := strconv.Atoi(p.src[start:end])
			p.i = end
			return &BackrefNode{Offset: pos, Index: n}
		}
		if c == '\\' || c == '.' || c == '+' || c == '*' || c == '?' ||
			c == '(' || c == ')' || c == '|' || c == '[' || c == ']' ||
			c == '{' || c == '}' || c == '^' || c == '$' || c == '/' ||
			c == '-' || c == '#' || c == ' ' || c == '"' || c == '\'' {
			p.i++
			return &CharNode{Offset: pos, Rune: rune(c)}
		}
		// Escaped non-ASCII byte: treat as literal invalid byte or rune.
		if c >= utf8.RuneSelf {
			r, size := utf8.DecodeRuneInString(p.src[p.i:])
			if r == utf8.RuneError && size == 1 {
				p.i++
				return &CharNode{Offset: pos, Rune: utf8.RuneError, Raw: c}
			}
			p.i += size
			return &CharNode{Offset: pos, Rune: r}
		}
		p.fail(pos, fmt.Sprintf("unknown escape \\%c", c))
		return nil
	}
}

func (p *parser) readHex(pos int, maxDigits int) (rune, bool) {
	n, digits := 0, 0
	for digits < maxDigits && p.i < len(p.src) && isHex(p.src[p.i]) {
		n = n*16 + hexVal(p.src[p.i])
		p.i++
		digits++
	}
	if digits == 0 {
		p.fail(pos, "hex escape requires digits")
		return 0, false
	}
	return rune(n), true
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	default:
		return int(c-'A') + 10
	}
}

func (p *parser) readPropertyName(pos int) (string, bool) {
	if p.i >= len(p.src) || p.src[p.i] != '{' {
		p.fail(pos, "property name must be braced: \\p{...}")
		return "", false
	}
	j := p.i + 1
	for j < len(p.src) && p.src[j] != '}' {
		j++
	}
	if j >= len(p.src) {
		p.fail(pos, "unterminated property name")
		return "", false
	}
	name := p.src[p.i+1 : j]
	if name == "" {
		p.fail(pos, "empty property name")
		return "", false
	}
	p.i = j + 1
	return name, true
}

func (p *parser) readAngleName(pos int) (string, bool) {
	if p.i >= len(p.src) || p.src[p.i] != '<' {
		p.fail(pos, "expected \\k<name>")
		return "", false
	}
	j := p.i + 1
	for j < len(p.src) && p.src[j] != '>' {
		j++
	}
	if j >= len(p.src) || j == p.i+1 {
		p.fail(pos, "expected \\k<name>")
		return "", false
	}
	name := p.src[p.i+1 : j]
	if !validName(name) {
		p.fail(p.i+1, "invalid group name in \\k<...>")
		return "", false
	}
	p.i = j + 1
	return name, true
}

// readQuoted consumes \Q ... \E (or to end of pattern) as literal chars.
func (p *parser) readQuoted(pos int) Node {
	p.i++ // consume Q
	start := p.i
	var runes []Node
	for p.i < len(p.src) {
		if p.src[p.i] == '\\' && p.i+1 < len(p.src) && p.src[p.i+1] == 'E' {
			p.i += 2
			break
		}
		runes = append(runes, p.parseRune(p.src[p.i]))
	}
	if len(runes) == 0 {
		return &ConcatNode{Offset: pos}
	}
	if len(runes) == 1 {
		return runes[0]
	}
	return &ConcatNode{Offset: start, Items: runes}
}
