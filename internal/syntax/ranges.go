package syntax

import (
	"sort"
	"unicode"
)

// MaxRune 是合法 Unicode 标量值上界。
const MaxRune = unicode.MaxRune

// invalidBase 之下的 256 个伪码点代表 0x80..0xFF 的无效 UTF-8 字节
// （伪码点 = invalidBase + byte）。它们绝不与任何真实 rune 区间相交。
const invalidBase rune = 0x110000

// InvalidByteRune 把单个非法 UTF-8 字节映射为伪码点。
func InvalidByteRune(b byte) rune { return invalidBase + rune(b) }

// IsPseudoRune 报告该码点是否为无效字节伪码点。
func IsPseudoRune(r rune) bool { return r >= invalidBase }

// NormalizeRanges 合并相邻/重叠区间，输出有序不相交区间列表。
func NormalizeRanges(rs []RuneRange) []RuneRange {
	if len(rs) == 0 {
		return nil
	}
	cp := make([]RuneRange, len(rs))
	copy(cp, rs)
	sort.Slice(cp, func(i, j int) bool {
		if cp[i].Lo != cp[j].Lo {
			return cp[i].Lo < cp[j].Lo
		}
		return cp[i].Hi < cp[j].Hi
	})
	out := cp[:0]
	cur := cp[0]
	for _, rr := range cp[1:] {
		if rr.Lo <= cur.Hi+1 {
			if rr.Hi > cur.Hi {
				cur.Hi = rr.Hi
			}
		} else {
			out = append(out, cur)
			cur = rr
		}
	}
	out = append(out, cur)
	return out
}

// ComplementRanges 返回全集（合法 rune + 伪字节码点）上的补集。
func ComplementRanges(rs []RuneRange) []RuneRange {
	norm := NormalizeRanges(rs)
	out := make([]RuneRange, 0, len(norm)+2)
	var next rune
	for _, rr := range norm {
		if rr.Lo > next {
			out = append(out, RuneRange{Lo: next, Hi: rr.Lo - 1})
		}
		if rr.Hi+1 > next {
			next = rr.Hi + 1
		}
	}
	// 合法 rune 域
	if next <= MaxRune {
		out = append(out, RuneRange{Lo: next, Hi: MaxRune})
	}
	// 伪字节码点域（0x80..0xFF）
	pLo, pHi := invalidBase+0x80, invalidBase+0xFF
	for _, rr := range norm {
		if rr.Lo > pHi {
			break
		}
		if rr.Hi >= pLo && rr.Lo <= pHi {
			if rr.Lo > pLo {
				out = append(out, RuneRange{Lo: pLo, Hi: rr.Lo - 1})
			}
			if rr.Hi+1 > pLo {
				pLo = rr.Hi + 1
			}
		}
	}
	if pLo <= pHi {
		out = append(out, RuneRange{Lo: pLo, Hi: pHi})
	}
	return NormalizeRanges(out)
}

// RangeContains 报告 r 是否落在区间集合内。
func RangeContains(rs []RuneRange, r rune) bool {
	i := sort.Search(len(rs), func(i int) bool { return rs[i].Hi >= r })
	return i < len(rs) && rs[i].Lo <= r && r <= rs[i].Hi
}

// 常用字符类区间（构造一次，解析期复用）。
var (
	digitRanges = tableRanges(unicode.Digit)
	spaceRanges = spaceRanges0()
	wordRanges  = wordRanges0()
)

func tableRanges(rt *unicode.RangeTable) []RuneRange {
	out := make([]RuneRange, 0, 32)
	for _, r16 := range rt.R16 {
		out = append(out, RuneRange{Lo: rune(r16.Lo), Hi: rune(r16.Hi)})
	}
	for _, r32 := range rt.R32 {
		if r32.Lo <= unicode.MaxRune {
			out = append(out, RuneRange{Lo: rune(r32.Lo), Hi: rune(r32.Hi)})
		}
	}
	return NormalizeRanges(out)
}

func spaceRanges0() []RuneRange {
	out := tableRanges(unicode.White_Space)
	// Go 的 White_Space 已含 \t\n\v\f\r 与 U+85/A0 等。
	return out
}

func wordRanges0() []RuneRange {
	out := tableRanges(unicode.Letter)
	out = append(out, tableRanges(unicode.Digit)...)
	out = append(out, RuneRange{Lo: '_', Hi: '_'})
	return NormalizeRanges(out)
}

// PropertyRanges 返回 \p{name} 的区间。general category 与常见脚本/属性
// 都支持；名字大小写不敏感，空格/下划线/连字符等价。
func PropertyRanges(name string) ([]RuneRange, bool) {
	return propertyRangesCached(name)
}
