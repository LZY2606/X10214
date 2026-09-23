package engine

import "unicode"

func classMatches(n *CharClassNode, r rune, invalid bool) bool {
	if invalid {
		return n.Negated
	}
	ok := false
	for _, rr := range n.Ranges {
		if r >= rr[0] && r <= rr[1] {
			ok = true
			break
		}
	}
	if !ok {
		switch n.ASCIIClass {
		case "d":
			ok = r <= '9' && r >= '0'
		case "D":
			ok = !(r <= '9' && r >= '0')
		case "w":
			ok = r == '_' || (r <= 'z' && r >= 'a') || (r <= 'Z' && r >= 'A') || (r <= '9' && r >= '0')
		case "W":
			ok = !(r == '_' || (r <= 'z' && r >= 'a') || (r <= 'Z' && r >= 'A') || (r <= '9' && r >= '0'))
		case "s":
			ok = r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' || r == 0x0b
		case "S":
			ok = !(r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' || r == 0x0b)
		}
	}
	if !ok {
		for _, t := range n.Categories {
			if unicode.In(r, t) {
				ok = true
				break
			}
		}
	}
	return ok != n.Negated
}

func (n *LookaroundNode) maxLookbehind(defaultLimit int) int {
	if !n.Behind {
		return 0
	}
	max := maxMatchLen(n.Child)
	if max < 0 {
		return defaultLimit
	}
	return max
}

func maxMatchLen(n Node) int {
	switch x := n.(type) {
	case *LiteralNode, *DotNode, *CharClassNode, *BackrefNode:
		return 4
	case *AnchorNode, *LookaroundNode:
		return 0
	case *GroupNode:
		if x.Child == nil {
			return 0
		}
		return maxMatchLen(x.Child)
	case *ConcatNode:
		total := 0
		for _, c := range x.Items {
			v := maxMatchLen(c)
			if v < 0 {
				return -1
			}
			total += v
		}
		return total
	case *AltNode:
		max := 0
		for _, c := range x.Branches {
			v := maxMatchLen(c)
			if v < 0 {
				return -1
			}
			if v > max {
				max = v
			}
		}
		return max
	case *RepeatNode:
		v := maxMatchLen(x.Child)
		if v == 0 {
			return 0
		}
		if v < 0 || x.Max < 0 {
			return -1
		}
		return v * x.Max
	default:
		return -1
	}
}
