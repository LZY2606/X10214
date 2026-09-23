package syntax

// Width reports the minimum and maximum symbol count a subtree can consume.
// bounded is false when the width is unbounded (max = -1).
// Width is measured in symbols (Unicode code points or invalid bytes).
func Width(a *Atom) (minW, maxW int, bounded bool) {
	if a == nil {
		return 0, 0, true
	}
	switch a.Kind {
	case KindLiteral, KindAny, KindClass, KindProperty, KindBackref:
		if a.Kind == KindBackref {
			return 0, -1, false
		}
		return 1, 1, true
	case KindAnchor, KindGroup:
		if a.Kind == KindGroup {
			return Width(a.Target)
		}
		return 0, 0, true
	case KindLook:
		return 0, 0, true
	case KindQuant:
		cMin, cMax, cBound := Width(a.Target)
		mn := cMin * a.Min
		if !cBound || a.Max == -1 {
			return mn, -1, false
		}
		return mn, cMax * a.Max, true
	case KindConcat:
		for _, n := range a.Nodes {
			mn, mx, b := Width(n)
			minW += mn
			if !b {
				maxW = -1
				bounded = false
			} else if bounded {
				maxW += mx
			}
		}
		return minW, maxW, bounded
	case KindAlt:
		first := true
		for _, n := range a.Nodes {
			mn, mx, b := Width(n)
			if first {
				minW, maxW, bounded, first = mn, mx, b, false
				continue
			}
			if mn < minW {
				minW = mn
			}
			if !b {
				maxW = -1
				bounded = false
			} else if bounded && mx > maxW {
				maxW = mx
			}
		}
		return
	}
	return 0, 0, true
}

func containsBackref(a *Atom) bool {
	if a == nil {
		return false
	}
	if a.Kind == KindBackref {
		return true
	}
	if containsBackref(a.Target) {
		return true
	}
	for _, n := range a.Nodes {
		if containsBackref(n) {
			return true
		}
	}
	for _, m := range a.Members {
		_ = m
	}
	return false
}

func validate(p *Pattern) error {
	bad := -1
	Walk(p.Root, func(a *Atom) bool {
		if bad >= 0 {
			return false
		}
		if a.Kind == KindBackref {
			if a.Index < 1 || a.Index > p.NumGroups {
				bad = a.Pos
				return false
			}
		}
		if a.Kind == KindQuant {
			if a.Target.Kind == KindQuant {
				// Nested quantifiers are legal in CTR-1.0.
			}
		}
		return true
	})
	if bad >= 0 {
		return &Error{Pos: bad, Msg: "backreference points at a group that does not exist"}
	}
	return nil
}

// Walk visits every node in the AST in source order.
func Walk(a *Atom, fn func(*Atom) bool) {
	if a == nil || !fn(a) {
		return
	}
	if a.Target != nil {
		Walk(a.Target, fn)
	}
	for _, n := range a.Nodes {
		Walk(n, fn)
	}
}
