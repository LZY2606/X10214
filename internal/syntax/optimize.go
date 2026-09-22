package syntax

// Optimize rewrites an AST into a cheaper but observationally equivalent
// form. The rewrite rules are deliberately conservative so that branch
// order, capture numbering and the emitted trace stay identical:
//
//  1. nested concatenations and alternations are flattened;
//  2. flag-free non-capturing groups are unwrapped;
//  3. quantifiers {1,1} are dropped and {0,0} becomes the empty sequence;
//  4. adjacent literal runes are fused into a single string instruction
//     (the VM still emits one trace event per rune).
func Optimize(n Node) Node {
	return opt(n)
}

func opt(n Node) Node {
	switch t := n.(type) {
	case *ConcatNode:
		var items []Node
		for _, item := range t.Items {
			item = opt(item)
			if inner, ok := item.(*ConcatNode); ok {
				items = append(items, inner.Items...)
				continue
			}
			items = append(items, item)
		}
		items = fuseLiterals(items)
		if len(items) == 1 {
			return items[0]
		}
		return &ConcatNode{Offset: t.Offset, Items: items}
	case *AlternationNode:
		var alts []Node
		for _, a := range t.Alternatives {
			a = opt(a)
			if inner, ok := a.(*AlternationNode); ok {
				alts = append(alts, inner.Alternatives...)
				continue
			}
			alts = append(alts, a)
		}
		if len(alts) == 1 {
			return alts[0]
		}
		return &AlternationNode{Offset: t.Offset, Alternatives: alts}
	case *GroupNode:
		t.Body = opt(t.Body)
		if !t.Capture && t.Mask == (Mask{}) {
			return t.Body
		}
		return t
	case *QuantNode:
		t.Child = opt(t.Child)
		if t.Min == 1 && t.Max == 1 {
			return t.Child
		}
		if t.Min == 0 && t.Max == 0 {
			return &ConcatNode{Offset: t.Offset}
		}
		return t
	case *LookaroundNode:
		t.Body = opt(t.Body)
		return t
	default:
		return n
	}
}

// fuseLiterals merges adjacent plain CharNodes into StrNodes. Raw
// invalid-byte chars are left untouched so byte-exact matching survives.
func fuseLiterals(items []Node) []Node {
	var out []Node
	var buf []rune
	start := 0
	flush := func() {
		if len(buf) == 0 {
			return
		}
		if len(buf) == 1 {
			out = append(out, &CharNode{Offset: start, Rune: buf[0]})
		} else {
			out = append(out, &StrNode{Offset: start, Runes: string(buf)})
		}
		buf = nil
	}
	for _, item := range items {
		if ch, ok := item.(*CharNode); ok && ch.Raw == 0 {
			if len(buf) == 0 {
				start = ch.Offset
			}
			buf = append(buf, ch.Rune)
			continue
		}
		flush()
		out = append(out, item)
	}
	flush()
	return out
}
