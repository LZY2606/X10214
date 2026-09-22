package syntax

import (
	"sort"
	"strconv"
)

// GroupInfo describes one capture group of a parsed pattern.
type GroupInfo struct {
	Index int    `json:"index"`
	Name  string `json:"name,omitempty"`
	From  int    `json:"from"`
	To    int    `json:"to"`
}

// Walk visits every node depth-first (pre-order), deterministic order.
func Walk(n *Node, fn func(*Node) bool) {
	if n == nil {
		return
	}
	if !fn(n) {
		return
	}
	for _, c := range n.Children {
		Walk(c, fn)
	}
	if n.Child != nil {
		Walk(n.Child, fn)
	}
}

// Groups returns capture groups ordered by index.
func Groups(root *Node) []GroupInfo {
	var out []GroupInfo
	Walk(root, func(n *Node) bool {
		if n.Kind == KindGroup && n.Capture {
			out = append(out, GroupInfo{Index: n.Index, Name: n.Name, From: n.From, To: n.To})
		}
		return true
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out
}

// NodeByID returns the node with the given deterministic ID.
func NodeByID(root *Node, id int) *Node {
	var found *Node
	Walk(root, func(n *Node) bool {
		if n.ID == id {
			found = n
			return false
		}
		return true
	})
	return found
}

// Validate performs post-parse checks that need the whole tree
// (backreferences must point at existing groups).
func Validate(root *Node) error {
	groups := map[int]bool{0: false}
	names := map[string]int{}
	var firstErr error
	Walk(root, func(n *Node) bool {
		switch n.Kind {
		case KindGroup:
			if n.Capture {
				groups[n.Index] = true
				if n.Name != "" {
					if _, dup := names[n.Name]; dup && firstErr == nil {
						firstErr = &ParseError{Msg: "duplicate group name " + n.Name, Offset: n.From}
					}
					names[n.Name] = n.Index
				}
			}
		case KindBackref:
			if n.Name != "" && n.Index == 0 {
				if _, ok := names[n.Name]; !ok && firstErr == nil {
					firstErr = &ParseError{Msg: "unknown named backreference " + n.Name, Offset: n.From}
				}
			} else if n.Index >= 0 && !groups[n.Index] && firstErr == nil {
				// groups map grows during walk; resolve against final pass below
			}
		}
		return firstErr == nil
	})
	if firstErr != nil {
		return firstErr
	}
	// second pass now that all groups are known
	var err error
	Walk(root, func(n *Node) bool {
		if n.Kind != KindBackref || err != nil {
			return err == nil
		}
		if n.Name != "" {
			if _, ok := names[n.Name]; !ok {
				err = &ParseError{Msg: "unknown named backreference \\" + n.Name, Offset: n.From}
				return false
			}
			return true
		}
		if !groups[n.Index] {
			err = &ParseError{Msg: "backreference \\" + strconv.Itoa(n.Index) + " has no group", Offset: n.From}
			return false
		}
		return true
	})
	return err
}

// ResolveBackref maps a backref node to a concrete group index.
func ResolveBackref(n *Node, root *Node) int {
	if n.Kind != KindBackref {
		return 0
	}
	if n.Index > 0 {
		return n.Index
	}
	idx := 0
	Walk(root, func(g *Node) bool {
		if g.Kind == KindGroup && g.Capture && g.Name == n.Name {
			idx = g.Index
			return false
		}
		return true
	})
	return idx
}

// fixedWidth reports the exact byte length a subtree can consume, or
// ok=false when the width is variable (forbidden inside lookbehind).
func fixedWidth(n *Node) (int, bool) {
	switch n.Kind {
	case KindEmpty, KindAssertStart, KindAssertEnd, KindWordBound, KindLook:
		return 0, true
	case KindChar:
		if n.Atom.RawByte >= 0 {
			return 1, true
		}
		return len(string(n.Atom.R)), true
	case KindAny:
		return 0, false
	case KindClass:
		// a class is one unit: invalid-byte atoms are 1 byte, runes vary
		for _, r := range n.Ranges {
			if r.Lo.RawByte >= 0 || r.Hi.RawByte >= 0 {
				return 0, false
			}
		}
		return 0, false
	case KindBackref:
		return 0, false
	case KindAtomic, KindGroup:
		return fixedWidthList(n.Children)
	case KindConcat:
		return fixedWidthList(n.Children)
	case KindAlt:
		w := -1
		for _, c := range n.Children {
			cw, ok := fixedWidth(c)
			if !ok {
				return 0, false
			}
			if w == -1 {
				w = cw
			} else if w != cw {
				return 0, false
			}
		}
		if w == -1 {
			return 0, true
		}
		return w, true
	case KindRepeat:
		if n.Max == -1 || n.Min != n.Max {
			return 0, false
		}
		cw, ok := fixedWidth(n.Child)
		if !ok {
			return 0, false
		}
		return cw * n.Min, true
	}
	return 0, false
}

func fixedWidthList(ns []*Node) (int, bool) {
	total := 0
	for _, c := range ns {
		cw, ok := fixedWidth(c)
		if !ok {
			return 0, false
		}
		total += cw
	}
	return total, true
}
