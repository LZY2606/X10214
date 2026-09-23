package regex

import (
	"strings"
	"unicode"
)

func unicodeToTable(name string) *unicode.RangeTable {
	name = strings.ReplaceAll(name, "-", "_")
	switch strings.ToLower(name) {
	case "letter", "l":
		return unicode.Letter
	case "lowercase_letter", "ll", "lower":
		return unicode.Lower
	case "uppercase_letter", "lu", "upper":
		return unicode.Upper
	case "titlecase_letter", "lt", "title":
		return unicode.Title
	case "number", "n":
		return unicode.Number
	case "decimal_number", "nd", "digit":
		return unicode.Digit
	case "punctuation", "p":
		return unicode.Punct
	case "symbol", "s":
		return unicode.Symbol
	case "separator", "z":
		return unicode.Space
	case "mark", "m":
		return unicode.Mark
	case "control", "cc", "cntrl":
		return unicode.Cc
	case "white_space", "whitespace", "ws":
		return unicode.White_Space
	default:
		return nil
	}
}

func runeInClass(r rune, cr ClassRange) bool {
	if cr.Property != "" {
		switch cr.Property {
		case "digit":
			return r >= '0' && r <= '9'
		case "not_digit":
			return !(r >= '0' && r <= '9')
		case "word":
			return r == '_' || unicode.In(r, unicode.Letter, unicode.Digit)
		case "not_word":
			return !(r == '_' || unicode.In(r, unicode.Letter, unicode.Digit))
		case "space":
			return unicode.IsSpace(r)
		case "not_space":
			return !unicode.IsSpace(r)
		case "ascii":
			return r >= 0 && r <= 127
		}
		table := unicodeToTable(cr.Property)
		if table != nil {
			return unicode.Is(table, r)
		}
	}
	if cr.Negated {
		return !(r >= cr.Lo && r <= cr.Hi)
	}
	return r >= cr.Lo && r <= cr.Hi
}

func classMatches(r rune, class *CharClass) bool {
	matched := false
	for _, cr := range class.Ranges {
		if cr.Property != "" {
			if runeInClass(r, cr) {
				matched = true
			}
		} else if r >= cr.Lo && r <= cr.Hi {
			matched = true
		}
	}
	if class.Negated {
		if r < 0 {
			return true
		}
		return !matched
	}
	return matched
}

func isWordRune(r rune) bool {
	return r == '_' || (r >= 0 && unicode.In(r, unicode.Letter, unicode.Digit))
}
