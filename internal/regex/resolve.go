package regex

// resolveRefs validates backreferences now that all groups are numbered.
func resolveRefs(n Node, info *GroupInfo) error {
	var bad error
	Walk(n, func(x Node) bool {
		if bad != nil {
			return false
		}
		if br, ok := x.(*AtomBackref); ok {
			if br.Name != "" {
				idx, ok := info.Names[br.Name]
				if !ok {
					bad = &ParseError{Offset: br.pos, Msg: "unknown named reference " + br.Name}
					return false
				}
				br.Index = idx
			} else if br.Index < 1 || br.Index > info.Count {
				bad = &ParseError{Offset: br.pos, Msg: "backreference to invalid group"}
				return false
			}
		}
		return true
	})
	return bad
}

// Walk visits every node; visiting stops deeper when fn returns false.
func Walk(n Node, fn func(Node) bool) {
	if n == nil {
		return
	}
	if !fn(n) {
		return
	}
	switch x := n.(type) {
	case *Concat:
		for _, c := range x.Items {
			Walk(c, fn)
		}
	case *Alt:
		for _, b := range x.Branches {
			Walk(b, fn)
		}
	case *Quant:
		Walk(x.Child, fn)
	case *Group:
		Walk(x.Child, fn)
	case *Look:
		Walk(x.Child, fn)
	}
}
