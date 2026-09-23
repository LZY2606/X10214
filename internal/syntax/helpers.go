package syntax

import "strconv"

func parseHex32(s string) (int64, bool) {
	v, err := strconv.ParseInt(s, 16, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// ResolveBackrefs assigns numeric indices to named backreferences after the
// whole pattern has been parsed (names may be defined after use).
func ResolveBackrefs(root *Node, names map[string]int) error {
	var walk func(n *Node) error
	walk = func(n *Node) error {
		if n == nil {
			return nil
		}
		if n.Kind == KBackref && n.RefName != "" {
			idx, ok := names[n.RefName]
			if !ok {
				return &ParseError{Pos: 0, Msg: "unknown named backreference " + n.RefName}
			}
			n.RefIndex = idx
		}
		if n.Child != nil {
			if err := walk(n.Child); err != nil {
				return err
			}
		}
		for _, k := range n.Kids {
			if err := walk(k); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(root)
}

// CountGroups reports the highest capture index assigned.
func CountGroups(root *Node) int {
	max := 0
	var walk func(n *Node)
	walk = func(n *Node) {
		if n == nil {
			return
		}
		if n.Kind == KGroup && n.Index > max {
			max = n.Index
		}
		if n.Child != nil {
			walk(n.Child)
		}
		for _, k := range n.Kids {
			walk(k)
		}
	}
	walk(root)
	return max
}
