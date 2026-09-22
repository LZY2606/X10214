package regex

// Optimize simplifies the AST without changing the documented branch
// order or the observable capture history. It only removes structure
// that emits no instructions (non-capturing groups, singleton
// concatenations/alternations, {1,1} repeats, empty repeats).
func Optimize(n Node) Node {
	switch t := n.(type) {
	case *Concat:
		var flat []Node
		for _, sub := range t.Nodes {
			s := Optimize(sub)
			if sc, ok := s.(*Concat); ok {
				flat = append(flat, sc.Nodes...)
			} else {
				flat = append(flat, s)
			}
		}
		if len(flat) == 1 {
			return flat[0]
		}
		return &Concat{Nodes: flat}
	case *Alt:
		branches := make([]Node, len(t.Branches))
		for i, b := range t.Branches {
			branches[i] = Optimize(b)
		}
		if len(branches) == 1 {
			return branches[0]
		}
		return &Alt{Branches: branches}
	case *Group:
		child := Optimize(t.Child)
		if !t.Capture {
			return child
		}
		return &Group{Child: child, Index: t.Index, Name: t.Name, Capture: true}
	case *Repeat:
		child := Optimize(t.Child)
		if t.Max == 0 {
			return &Concat{}
		}
		if t.Min == 1 && t.Max == 1 {
			return child
		}
		return &Repeat{Child: child, Min: t.Min, Max: t.Max, Lazy: t.Lazy}
	case *Look:
		return &Look{Child: Optimize(t.Child), Negate: t.Negate, Behind: t.Behind}
	default:
		return n
	}
}
