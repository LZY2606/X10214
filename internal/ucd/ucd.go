// Package ucd loads the pinned Unicode Character Database used by the
// capture-trace executor. No host regexp package is involved.
package ucd

import (
	_ "embed"
	"strconv"
	"strings"
)

// Version is the exact UCD release every result is pinned to.
const Version = "Unicode 16.0.0 (https://www.unicode.org/Public/16.0.0/ucd/)"

// Ranges is a sorted, disjoint list of closed rune intervals.
type Ranges [][2]rune

func (rs Ranges) Has(r rune) bool {
	lo, hi := 0, len(rs)
	for lo < hi {
		mid := (lo + hi) / 2
		if r < rs[mid][0] {
			hi = mid
		} else if r > rs[mid][1] {
			lo = mid + 1
		} else {
			return true
		}
	}
	return false
}

//go:embed ucd16/UnicodeData.txt
var unicodeData string

//go:embed ucd16/PropList.txt
var propList string

//go:embed ucd16/DerivedCoreProperties.txt
var derivedCore string

type table struct {
	cats  map[string]Ranges
	props map[string]Ranges
}

var db = func() *table {
	t := &table{cats: map[string]Ranges{}, props: map[string]Ranges{}}
	parseUnicodeData(unicodeData, t.cats)
	parsePropFile(propList, t.props)
	parsePropFile(derivedCore, t.props)
	return t
}()

func parsePropFile(content string, out map[string]Ranges) {
	for _, line := range strings.Split(content, "\n") {
		i := strings.IndexByte(line, '#')
		if i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, ";")
		if len(fields) < 2 {
			continue
		}
		rng := parseRange(strings.TrimSpace(fields[0]))
		name := strings.TrimSpace(fields[1])
		out[name] = append(out[name], rng)
	}
	for k := range out {
		out[k] = mergeRanges(out[k])
	}
}

func parseRange(s string) [2]rune {
	if i := strings.Index(s, ".."); i >= 0 {
		a, _ := strconv.ParseUint(s[:i], 16, 32)
		b, _ := strconv.ParseUint(s[i+2:], 16, 32)
		return [2]rune{rune(a), rune(b)}
	}
	v, _ := strconv.ParseUint(s, 16, 32)
	return [2]rune{rune(v), rune(v)}
}

func parseUnicodeData(content string, out map[string]Ranges) {
	type seg struct {
		start rune
		end   rune
		gc    string
	}
	var segs []seg
	for _, line := range strings.Split(content, "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, ";")
		if len(f) < 3 {
			continue
		}
		cp, _ := strconv.ParseUint(f[0], 16, 32)
		name, gc := f[1], f[2]
		r := rune(cp)
		if strings.HasPrefix(name, "<") && strings.HasSuffix(name, "First>") {
			segs = append(segs, seg{start: r, gc: gc})
		} else if strings.HasPrefix(name, "<") && strings.HasSuffix(name, "Last>") {
			if n := len(segs); n > 0 {
				segs[n-1].end = r
			}
		} else {
			segs = append(segs, seg{start: r, end: r, gc: gc})
		}
	}
	add := func(gc string, a, b rune) {
		out[gc] = append(out[gc], [2]rune{a, b})
	}
	for _, s := range segs {
		// Major class bucket: L, M, N, P, S, Z, C.
		if len(s.gc) == 2 {
			add(s.gc, s.start, s.end)
			add(string(s.gc[0]), s.start, s.end)
		}
	}
	for k := range out {
		out[k] = mergeRanges(out[k])
	}
	// LC = Lu | Ll | Lt.
	var lc Ranges
	lc = append(lc, out["Lu"]...)
	lc = append(lc, out["Ll"]...)
	lc = append(lc, out["Lt"]...)
	out["LC"] = mergeRanges(lc)
}

func mergeRanges(in Ranges) Ranges {
	if len(in) < 2 {
		return in
	}
	// Interval lists are already ordered by codepoint for every parsed file,
	// but sort defensively without pulling in sort.
	for i := 1; i < len(in); i++ {
		for j := i; j > 0 && in[j-1][0] > in[j][0]; j-- {
			in[j-1], in[j] = in[j], in[j-1]
		}
	}
	out := in[:1]
	for _, iv := range in[1:] {
		last := &out[len(out)-1]
		if iv[0] <= last[1]+1 {
			if iv[1] > last[1] {
				last[1] = iv[1]
			}
		} else {
			out = append(out, iv)
		}
	}
	return out
}

var groupAliases = map[string]string{
	"Letter": "L", "Mark": "M", "Number": "N", "Punctuation": "P",
	"Symbol": "S", "Separator": "Z", "Other": "C",
}

// LookupProperty resolves \p{Name} / \P{Name}. General categories
// (L, Lu, LC, Nd, ...) and every property carried by the embedded files
// (White_Space, Alphabetic, ...) are supported.
func LookupProperty(name string) (Ranges, bool) {
	if len(name) == 1 {
		if r, ok := db.cats[name]; ok {
			return r, true
		}
	}
	if r, ok := db.cats[name]; ok {
		return r, true
	}
	if g, ok := groupAliases[name]; ok {
		r, ok := db.cats[g]
		return r, ok
	}
	if r, ok := db.props[name]; ok {
		return r, true
	}
	// Tolerate ASCII/WhiteSpace-style spellings.
	compact := strings.NewReplacer("_", "", "-", "", " ", "").Replace(name)
	for pname, r := range db.props {
		x := strings.NewReplacer("_", "", "-", "", " ", "").Replace(pname)
		if strings.EqualFold(x, compact) {
			return r, true
		}
	}
	return nil, false
}

// IsDecimalDigit reports a decimal-number (Nd) symbol.
func IsDecimalDigit(r rune) bool { return db.cats["Nd"].Has(r) }

// IsWhiteSpace reports the Unicode White_Space property.
func IsWhiteSpace(r rune) bool { return db.props["White_Space"].Has(r) }

// IsWordChar follows the common Unicode \w rule: Alphabetic, mark, decimal
// digit, letter number, connector punctuation, or join control.
func IsWordChar(r rune) bool {
	switch {
	case db.props["Alphabetic"].Has(r):
		return true
	case db.cats["M"].Has(r), db.cats["Nd"].Has(r), db.cats["Nl"].Has(r), db.cats["Pc"].Has(r):
		return true
	case r == 0x200C || r == 0x200D:
		return true
	}
	return false
}
