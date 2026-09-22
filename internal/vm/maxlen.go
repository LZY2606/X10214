package vm

import "trajectoryroom/internal/syntax"

// maxMatchLen returns an upper bound on the number of runes the node can
// consume, or -1 when unbounded. Lookarounds inside consume nothing for the
// outer length (they are zero-width).
func maxMatchLen(n *syntax.Node) int {
	if n == nil {
		return 0
	}
	switch n.Kind {
	case syntax.NEmpty, syntax.NAnchor, syntax.NLook:
		return 0
	case syntax.NChar, syntax.NAny, syntax.NCharClass:
		return 1
	case syntax.NBackref:
		// A backreference can consume an unbounded amount at runtime.
		return -1
	case syntax.NFlags:
		return 0
	case syntax.NGroup:
		return maxMatchLen(n.Children[0])
	case syntax.NConcat:
		total := 0
		for _, ch := range n.Children {
			v := maxMatchLen(ch)
			if v < 0 {
				return -1
			}
			total += v
		}
		return total
	case syntax.NAlt:
		best := 0
		for _, ch := range n.Children {
			v := maxMatchLen(ch)
			if v < 0 {
				return -1
			}
			if v > best {
				best = v
			}
		}
		return best
	case syntax.NRepeat:
		v := maxMatchLen(n.Child)
		if v == 0 {
			return 0
		}
		if n.Max < 0 || v < 0 {
			return -1
		}
		return v * n.Max
	}
	return -1
}
