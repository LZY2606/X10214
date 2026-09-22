package regex

// Optimize applies semantics-preserving rewrites. The contract is strict:
//
//   - alternation branch textual priority order never changes;
//   - greedy/lazy preference of every quantifier never changes;
//   - capture indices and names never change;
//   - lookaround polarity never changes;
//   - every emitted thread must explore alternatives in the identical
//     preferred/deferred ordering (verified by trace-equivalence tests).
//
// Implemented rewrites:
//  1. flatten nested concat nodes;
//  2. drop empty children from concatenations;
//  3. unwrap non-capturing groups whose body is a plain child (the group
//     itself emitted no save slots);
//  4. flatten unary alternations.
func Optimize(n *Node) *Node {
	return optNode(n)
}

func optNode(n *Node) *Node {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case NGroup:
		n.Body = optNode(n.Body)
		if n.Capture == 0 && n.Name == "" {
			// Non-capturing group: safe to replace by body in a value position;
			// callers that need grouping (quantifier body) keep semantics
			// because the body node itself remains the quantifier's body.
			return n.Body
		}
		return n
	case NLook:
		n.Body = optNode(n.Body)
		return n
	case NRepeat:
		n.Body = optNode(n.Body)
		return n
	case NFlagScope:
		n.Body = optNode(n.Body)
		return n
	case NConcat:
		var kids []*Node
		for _, ch := range n.Children {
			ch = optNode(ch)
			if ch.Kind == NConcat {
				kids = append(kids, ch.Children...)
				continue
			}
			if ch.Kind == NEmpty {
				continue
			}
			kids = append(kids, ch)
		}
		if len(kids) == 0 {
			return &Node{ID: n.ID, Kind: NEmpty}
		}
		if len(kids) == 1 {
			return kids[0]
		}
		n.Children = kids
		return n
	case NAlt:
		for i := range n.Children {
			n.Children[i] = optNode(n.Children[i])
		}
		if len(n.Children) == 1 {
			return n.Children[0]
		}
		return n
	}
	return n
}

// Program.Optimized is flipped by Build when the optimizer ran.
