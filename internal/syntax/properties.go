package syntax

import (
	"strings"
	"sync"
	"unicode"
)

var propCache sync.Map

func normProp(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	r := strings.NewReplacer("_", "", "-", "", " ", "")
	return r.Replace(name)
}

func propertyRangesCached(name string) ([]RuneRange, bool) {
	key := normProp(name)
	if v, ok := propCache.Load(key); ok {
		rs := v.([]RuneRange)
		return rs, rs != nil
	}
	rs, ok := lookupProperty(key)
	if !ok {
		propCache.Store(key, []RuneRange(nil))
		return nil, false
	}
	rs = NormalizeRanges(rs)
	propCache.Store(key, rs)
	return rs, true
}

func lookupProperty(key string) ([]RuneRange, bool) {
	// general categories
	if rt, ok := unicode.Categories[key]; ok {
		return tableRanges(rt), true
	}
	// 复合类别 C, L, M, N, P, S, Z
	switch key {
	case "c":
		return multiTable(unicode.C, unicode.Cc, unicode.Cf, unicode.Cs, unicode.Co, unicode.Cn), true
	case "l":
		return multiTable(unicode.L, unicode.Lu, unicode.Ll, unicode.Lt, unicode.Lm, unicode.Lo), true
	case "m":
		return multiTable(unicode.M, unicode.Mn, unicode.Mc, unicode.Me), true
	case "n":
		return multiTable(unicode.N, unicode.Nd, unicode.Nl, unicode.No), true
	case "p":
		return multiTable(unicode.P, unicode.Pc, unicode.Pd, unicode.Ps, unicode.Pe, unicode.Pi, unicode.Pf, unicode.Po), true
	case "s":
		return multiTable(unicode.S, unicode.Sm, unicode.Sc, unicode.Sk, unicode.So), true
	case "z":
		return multiTable(unicode.Z, unicode.Zs, unicode.Zl, unicode.Zp), true
	}
	if rt, ok := unicode.Scripts[key]; ok {
		return tableRanges(rt), true
	}
	if rt, ok := unicode.Properties[key]; ok {
		return tableRanges(rt), true
	}
	return nil, false
}

func multiTable(tabs ...*unicode.RangeTable) []RuneRange {
	var out []RuneRange
	for _, t := range tabs {
		out = append(out, tableRanges(t)...)
	}
	return out
}

func namedClassRanges(kind byte) []RuneRange {
	switch kind {
	case 'd':
		return digitRanges
	case 'D':
		return ComplementRanges(digitRanges)
	case 'w':
		return wordRanges
	case 'W':
		return ComplementRanges(wordRanges)
	case 's':
		return spaceRanges
	case 'S':
		return ComplementRanges(spaceRanges)
	}
	return nil
}
