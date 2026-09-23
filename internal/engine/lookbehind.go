package engine

// maxLookbehindLen returns an upper bound on the length, in bytes, that a
// lookbehind body can consume. -1 means "unbounded": the VM then walks every
// possible start from the assertion position back to 0 (nearest first),
// which keeps the documented branch order while still being exact.
// Zero-width bodies contribute 0.
func maxLookbehindLen(n Node) int {
	switch t := n.(type) {
	case *Concat:
		sum := 0
		for _, ch := range t.Nodes {
			v := maxLookbehindLen(ch)
			if v < 0 {
				return -1
			}
			sum += v
		}
		return sum
	case *Alt:
		best := 0
		for _, b := range t.Branches {
			v := maxLookbehindLen(b)
			if v < 0 {
				return -1
			}
			if v > best {
				best = v
			}
		}
		return best
	case *Repeat:
		ch := maxLookbehindLen(t.Child)
		if ch < 0 {
			return -1
		}
		if t.Max < 0 {
			if ch == 0 {
				return 0
			}
			return -1
		}
		return t.Max * ch
	case *Group:
		return maxLookbehindLen(t.Child)
	case *Lookaround:
		return 0
	case *Char, *CharClass:
		return 4 // a single code point is at most 4 UTF-8 bytes
	case *AnyChar:
		return 4
	case *Anchor, *Ref:
		if _, ok := n.(*Ref); ok {
			// backreference length is only known at run time
			return -1
		}
		return 0
	}
	return 0
}
