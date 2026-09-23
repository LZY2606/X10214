package regex

import "fmt"

// RequireFixedLength validates that a lookbehind pattern consumes a
// constant number of input atoms. It returns that length.
func RequireFixedLength(n Node) (int, error) {
	switch x := n.(type) {
	case *Concat:
		total := 0
		for _, c := range x.Items {
			k, err := RequireFixedLength(c)
			if err != nil {
				return 0, err
			}
			total += k
		}
		return total, nil
	case *Alt:
		if len(x.Branches) == 0 {
			return 0, nil
		}
		first, err := RequireFixedLength(x.Branches[0])
		if err != nil {
			return 0, err
		}
		for _, b := range x.Branches[1:] {
			k, err := RequireFixedLength(b)
			if err != nil {
				return 0, err
			}
			if k != first {
				return 0, fmt.Errorf("alternation branches differ in length")
			}
		}
		return first, nil
	case *Quant:
		if x.Max == -1 {
			return 0, fmt.Errorf("unbounded quantifier in lookbehind")
		}
		k, err := RequireFixedLength(x.Child)
		if err != nil {
			return 0, err
		}
		_ = x.Min
		if x.Max != x.Min {
			return 0, fmt.Errorf("variable repetition in lookbehind")
		}
		return k * x.Max, nil
	case *Group:
		return RequireFixedLength(x.Child)
	case *Look:
		return 0, fmt.Errorf("nested lookaround in lookbehind")
	case *AtomLit:
		return 1, nil
	case *AtomDot, *AtomClass, *AtomGrapheme:
		return 1, nil
	case *AtomBackref:
		return 0, fmt.Errorf("backreference in lookbehind is not fixed-length")
	case *AtomBoundary:
		return 0, nil
	}
	return 0, fmt.Errorf("unknown node")
}
