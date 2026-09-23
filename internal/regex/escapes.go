package regex

import (
	"strconv"
	"unicode"
)

func simpleEscape(c byte) (rune, bool, bool) {
	switch c {
	case 'n':
		return '\n', false, false
	case 'r':
		return '\r', false, false
	case 't':
		return '\t', false, false
	case 'f':
		return '\f', false, false
	case 'v':
		return '\v', false, false
	case '0':
		return 0, false, false
	case 'a':
		return 0x07, false, false
	case 'd':
		return 0, true, true
	case 'D':
		return 0, true, true
	case 'w':
		return 0, true, true
	case 'W':
		return 0, true, true
	case 's':
		return 0, true, true
	case 'S':
		return 0, true, true
	default:
		return rune(c), false, false
	}
}

func (p *parser) parseHexEscape(max int) (rune, error) {
	start := p.pos
	end := p.pos + max
	if p.peek() == '{' {
		p.pos++
		start = p.pos
		for !p.eof() && p.peek() != '}' {
			p.pos++
		}
		if p.peek() != '}' {
			return 0, p.error("unterminated hex escape")
		}
		text := p.pattern[start:p.pos]
		p.pos++
		v, err := strconv.ParseInt(text, 16, 32)
		if err != nil || v > unicode.MaxRune {
			return 0, p.error("invalid Unicode scalar")
		}
		return rune(v), nil
	}
	if p.pos+max > len(p.pattern) {
		return 0, p.error("short hex escape")
	}
	text := p.pattern[p.pos : p.pos+max]
	p.pos += max
	v, err := strconv.ParseInt(text, 16, 32)
	if err != nil || v > unicode.MaxRune {
		return 0, p.error("invalid Unicode scalar")
	}
	_ = end
	return rune(v), nil
}

func isKnownProperty(name string) bool {
	if rt := unicodeToTable(name); rt != nil {
		return true
	}
	switch name {
	case "word", "not_word", "space", "not_space", "digit", "not_digit", "ascii":
		return true
	}
	return false
}
