// Package unicodex ships the project-owned Unicode data used by the capture
// tracer room executor. Tables are generated from a pinned Unicode release
// (see tables_generated.go and gen_main.go); the matching engine never reaches
// into the host toolchain's unicode package at run time.
package unicodex

// Version pins the Unicode database release that the generated tables encode.
const Version = "15.0.0"

// GeneralCategory reports whether r belongs to the Unicode general category
// named by name. name is the canonical two-letter code (for example "Nd",
// "Lu") or a one-letter aggregate ("L", "N", ...). "Cn" is derived by
// exclusion: every non-surrogate code point not covered by another category.
func GeneralCategory(r rune, name string) bool {
	return inCategory(r, name)
}

// WhiteSpace reports the Unicode White_Space property.
func WhiteSpace(r rune) bool {
	return inRanges(r, whiteSpace)
}

// Word reports the "word character" predicate used by \w and word boundaries.
// It follows the Unicode-aware definition: general categories L, N, Mn, Mc,
// U+005F LOW LINE, U+200C ZERO WIDTH NON-JOINER and U+200D ZERO WIDTH JOINER.
func Word(r rune) bool {
	if r == '_' || r == '\u200c' || r == '\u200d' {
		return true
	}
	if inCategory(r, "L") || inCategory(r, "N") {
		return true
	}
	return inRanges(r, tables["Mn"]) || inRanges(r, tables["Mc"])
}

// FoldRunes returns the full case-folding equivalence cycle containing r
// (including r itself), or nil when r participates in no fold set.
func FoldRunes(r rune) []rune {
	idx, ok := foldIndex[r]
	if !ok {
		return nil
	}
	return foldSets[idx]
}

func inRanges(r rune, rs [][2]rune) bool {
	lo, hi := 0, len(rs)
	for lo < hi {
		mid := (lo + hi) / 2
		p := rs[mid]
		if r < p[0] {
			hi = mid
		} else if r > p[1] {
			lo = mid + 1
		} else {
			return true
		}
	}
	return false
}

func inCategory(r rune, name string) bool {
	switch name {
	case "L":
		return inCategory(r, "Lu") || inCategory(r, "Ll") || inCategory(r, "Lt") ||
			inCategory(r, "Lm") || inCategory(r, "Lo")
	case "M":
		return inCategory(r, "Mn") || inCategory(r, "Mc") || inCategory(r, "Me")
	case "N":
		return inCategory(r, "Nd") || inCategory(r, "Nl") || inCategory(r, "No")
	case "P":
		return inCategory(r, "Pc") || inCategory(r, "Pd") || inCategory(r, "Ps") ||
			inCategory(r, "Pe") || inCategory(r, "Pi") || inCategory(r, "Pf") ||
			inCategory(r, "Po")
	case "S":
		return inCategory(r, "Sm") || inCategory(r, "Sc") || inCategory(r, "Sk") ||
			inCategory(r, "So")
	case "Z":
		return inCategory(r, "Zs") || inCategory(r, "Zl") || inCategory(r, "Zp")
	case "C":
		return inCategory(r, "Cc") || inCategory(r, "Cf") || inCategory(r, "Cs") ||
			inCategory(r, "Co") || inCategory(r, "Cn")
	case "Cn":
		// Derived by exclusion from every other general category.
		return r <= 0x10FFFF && r != 0xFFFE && r != 0xFFFF &&
			!(inCategory(r, "L") || inCategory(r, "M") || inCategory(r, "N") ||
				inCategory(r, "P") || inCategory(r, "S") || inCategory(r, "Z") ||
				inCategory(r, "Cc") || inCategory(r, "Cf") || inCategory(r, "Cs") ||
				inCategory(r, "Co"))
	}
	if rs, ok := tables[name]; ok {
		return inRanges(r, rs)
	}
	return false
}
