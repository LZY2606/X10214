// Package unicodex owns the fixed Unicode data release used by the engine.
// Every match result pins UnicodeDataVersion so a result is never interpreted
// against a different character database.
package unicodex

import (
	"embed"
	"strconv"
	"strings"
	"sync"
)

// SyntaxVersion is the documented dialect ("Capture Trace Room regex", CTR).
const SyntaxVersion = "CTR-1.0"

// UnicodeDataVersion pins the UCD release embedded below.
const UnicodeDataVersion = "16.0.0"

//go:embed data/UnicodeData.txt data/CaseFolding.txt data/PropList.txt data/DerivedCoreProperties.txt data/GraphemeBreakProperty.txt
var dataFS embed.FS

// Range is a contiguous inclusive rune interval with a shared property.
type Range struct {
	Lo, Hi rune
}

// DB is the parsed Unicode database used by the regex engine.
type DB struct {
	// General categories by canonical two-letter name, e.g. "Lu", "Nd".
	Categories map[string][]Range
	// Aggregate categories: L, Lu.. Lo, M, N, P, S, Z, C and "LC".
	Groups map[string][]Range
	// Binary properties from PropList and DerivedCoreProperties.
	Properties map[string][]Range
	// Simple case fold target for code points with one-letter simple folds.
	Fold map[rune]rune
	// Precomputed fold equivalence lists keyed by lowercase representative.
	foldGroups map[rune][]rune
}

var (
	defaultDB *DB
	once      sync.Once
)

// Default returns the lazily parsed embedded UCD.
func Default() *DB {
	once.Do(func() {
		db, err := parse()
		if err != nil {
			panic("unicodex: " + err.Error())
		}
		defaultDB = db
	})
	return defaultDB
}

func mustRead(name string) string {
	b, err := dataFS.ReadFile("data/" + name)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func parseHex(s string) (rune, bool) {
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, false
	}
	return rune(v), true
}

// stripComment removes the # comment tail of a UCD line.
func stripComment(line string) string {
	if i := strings.IndexByte(line, '#'); i >= 0 {
		return line[:i]
	}
	return line
}

func fields(line string) []string {
	line = strings.TrimSpace(stripComment(line))
	if line == "" {
		return nil
	}
	return strings.Fields(line)
}

// rangeCollector turns a sequence of UCD range records into sorted, merged
// inclusive ranges.
type rangeCollector struct{ r []Range }

func (c *rangeCollector) addPoint(r rune) {
	c.add(r, r)
}

func (c *rangeCollector) add(lo, hi rune) {
	if n := len(c.r); n > 0 && c.r[n-1].Hi+1 == lo {
		c.r[n-1].Hi = hi
		return
	}
	c.r = append(c.r, Range{Lo: lo, Hi: hi})
}

func (c *rangeCollector) sorted() []Range { return c.r }

func parse() (*DB, error) {
	db := &DB{
		Categories: map[string][]Range{},
		Groups:     map[string][]Range{},
		Properties: map[string][]Range{},
		Fold:       map[rune]rune{},
	}

	catCol := map[string]*rangeCollector{}
	catGroupCol := map[byte]*rangeCollector{}
	addCat := func(gc string, lo, hi rune) {
		col := catCol[gc]
		if col == nil {
			col = &rangeCollector{}
			catCol[gc] = col
		}
		col.add(lo, hi)
		if len(gc) == 2 {
			g := catGroupCol[gc[0]]
			if g == nil {
				g = &rangeCollector{}
				catGroupCol[gc[0]] = g
			}
			g.add(lo, hi)
		}
	}

	// UnicodeData.txt: codepoint;Name;General_Category;...
	// A "<..., First>" line is immediately followed by its "Last" twin.
	var pendingFirst rune
	var pendingGC string
	havePending := false
	for _, line := range strings.Split(mustRead("UnicodeData.txt"), "\n") {
		parts := strings.Split(line, ";")
		if len(parts) < 3 {
			continue
		}
		cp, ok := parseHex(parts[0])
		if !ok {
			continue
		}
		gc := parts[2]
		name := parts[1]
		switch {
		case strings.HasPrefix(name, "<") && strings.HasSuffix(name, ", First>"):
			pendingFirst, pendingGC, havePending = cp, gc, true
		case strings.HasPrefix(name, "<") && strings.HasSuffix(name, ", Last>") && havePending && pendingGC == gc:
			addCat(gc, pendingFirst, cp)
			havePending = false
		default:
			addCat(gc, cp, cp)
		}
	}
	for name, col := range catCol {
		db.Categories[name] = col.sorted()
	}
	for b, col := range catGroupCol {
		db.Groups[string(b)] = col.sorted()
	}

	// Derived "Cased_Letter" = Lu | Ll | Lt.
	lc := &rangeCollector{}
	for _, name := range []string{"Lu", "Ll", "Lt"} {
		for _, r := range db.Categories[name] {
			lc.add(r.Lo, r.Hi)
		}
	}
	db.Groups["LC"] = lc.sorted()

	propCol := map[string]*rangeCollector{}
	addProp := func(name string, lo, hi rune) {
		col := propCol[name]
		if col == nil {
			col = &rangeCollector{}
			propCol[name] = col
		}
		col.add(lo, hi)
	}

	parseRangeFile := func(file string) {
		for _, line := range strings.Split(mustRead(file), "\n") {
			f := fields(line)
			if len(f) < 2 {
				continue
			}
			lo, hi := parseRangeField(f[0])
			if lo < 0 {
				continue
			}
			addProp(f[1], lo, hi)
		}
	}
	parseRangeFile("PropList.txt")
	parseRangeFile("DerivedCoreProperties.txt")

	// Grapheme_Extend + Grapheme_Cluster_Break=Extend/SpacingMark define the
	// documented "combining mark" predicate used for combining-character
	// exercises (full cluster boundary rules live in the matcher).
	parseRangeFile("GraphemeBreakProperty.txt")

	for name, col := range propCol {
		db.Properties[name] = col.sorted()
	}

	// CaseFolding.txt status C and S: one-letter simple mappings.
	for _, line := range strings.Split(mustRead("CaseFolding.txt"), "\n") {
		f := fields(line)
		if len(f) < 3 {
			continue
		}
		if f[1] != "C" && f[1] != "S" {
			continue
		}
		cp, ok1 := parseHex(f[0])
		to, ok2 := parseHex(f[2])
		if ok1 && ok2 {
			db.Fold[cp] = to
		}
	}
	db.foldGroups = buildFoldGroups(db.Fold)
	return db, nil
}

func parseRangeField(s string) (lo, hi rune) {
	if i := strings.IndexByte(s, '.'); i >= 0 {
		lo, ok1 := parseHex(s[:i])
		hi, ok2 := parseHex(s[i+2:])
		if !ok1 || !ok2 {
			return -1, -1
		}
		return lo, hi
	}
	r, ok := parseHex(s)
	if !ok {
		return -1, -1
	}
	return r, r
}

// HasRange reports whether r is a member of a sorted range list.
func HasRange(rs []Range, r rune) bool {
	lo, hi := 0, len(rs)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		switch {
		case r < rs[mid].Lo:
			hi = mid - 1
		case r > rs[mid].Hi:
			lo = mid + 1
		default:
			return true
		}
	}
	return false
}

// FoldEq reports simple Unicode case-insensitive equivalence using UCD
// status C/S folds. Full multi-codepoint F mappings are intentionally not
// applied; see SEMANTICS.md.
func (d *DB) FoldEq(a, b rune) bool {
	if a == b {
		return true
	}
	return d.fold(a) == d.fold(b)
}

func (d *DB) fold(r rune) rune {
	if f, ok := d.Fold[r]; ok {
		return f
	}
	return r
}

// FoldEquivalents returns the small set of runes sharing a simple fold class.
func (d *DB) FoldEquivalents(r rune) []rune {
	return d.foldGroups[d.fold(r)]
}
