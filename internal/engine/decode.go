package engine

import "unicode/utf8"

func decodeRune(s string) (rune, int) {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError && size == 1 {
		// Distinguish real U+FFFD from malformed input.
		if len(s) >= 3 && s[0] == 0xEF && s[1] == 0xBF && s[2] == 0xBD {
			return utf8.RuneError, 3
		}
		return utf8.RuneError, 1
	}
	return r, size
}
