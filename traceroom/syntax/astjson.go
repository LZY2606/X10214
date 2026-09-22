package syntax

import "encoding/json"

// ASTNode is the JSON projection of an AST node for the UI.
type ASTNode struct {
	Kind     string         `json:"kind"`
	Text     string         `json:"text,omitempty"`
	SrcLo    int            `json:"srcLo"`
	SrcHi    int            `json:"srcHi"`
	Detail   map[string]any `json:"detail,omitempty"`
	Children []ASTNode      `json:"children,omitempty"`
}

// MarshalAST renders a pattern AST as JSON-ready data.
func MarshalAST(p *Pattern) ASTNode {
	root := nodeToJSON(p.Root)
	return ASTNode{
		Kind:     "pattern",
		Text:     p.Source,
		Detail:   map[string]any{"numCaps": p.NumCaps, "names": p.Names, "syntaxVersion": Version},
		Children: []ASTNode{root},
	}
}

func nodeToJSON(n Node) ASTNode {
	lo, hi := n.srcRange()
	out := ASTNode{Kind: n.nodeKind(), SrcLo: lo, SrcHi: hi}
	switch t := n.(type) {
	case *Seq:
		for _, k := range t.Kids {
			out.Children = append(out.Children, nodeToJSON(k))
		}
	case *Alt:
		for i, br := range t.Branches {
			ch := ASTNode{Kind: "branch", Detail: map[string]any{"index": i, "priority": i}}
			for _, k := range br {
				ch.Children = append(ch.Children, nodeToJSON(k))
			}
			out.Children = append(out.Children, ch)
		}
	case *Char:
		out.Text = string(t.R)
		out.Detail = map[string]any{"rune": t.R, "codepoint": t.R}
	case *Dot:
		out.Text = "."
	case *Class:
		out.Text = renderClass(t)
		out.Detail = map[string]any{"negated": t.Negated, "terms": renderTerms(t.Terms)}
	case *Anchor:
		out.Text = t.Kind.String()
		out.Detail = map[string]any{"anchor": t.Kind.String()}
	case *Repeat:
		out.Detail = map[string]any{"min": t.Min, "max": repeatMax(t.Max), "greedy": t.Greedy, "possessive": t.Possessive}
		out.Children = []ASTNode{nodeToJSON(t.Kid)}
	case *Group:
		out.Detail = map[string]any{"capID": t.CapID, "name": t.Name}
		if t.Kid != nil {
			out.Children = []ASTNode{nodeToJSON(t.Kid)}
		}
	case *Atomic:
		out.Children = []ASTNode{nodeToJSON(t.Kid)}
	case *Flags:
		out.Detail = map[string]any{"set": flagNames(t.Set), "clear": flagNames(t.Clear)}
		if t.Kid != nil {
			out.Children = []ASTNode{nodeToJSON(t.Kid)}
		}
	case *Look:
		out.Text = lookText(t)
		out.Detail = map[string]any{"behind": t.Behind, "negative": t.Neg}
		out.Children = []ASTNode{nodeToJSON(t.Kid)}
	case *Backref:
		out.Text = backrefText(t)
		out.Detail = map[string]any{"id": t.ID, "name": t.Name}
	}
	return out
}

func repeatMax(m int) any {
	if m == -1 {
		return "inf"
	}
	return m
}

func flagNames(f uint8) []string {
	var names []string
	if f&FlagI != 0 {
		names = append(names, "i")
	}
	if f&FlagM != 0 {
		names = append(names, "m")
	}
	if f&FlagS != 0 {
		names = append(names, "s")
	}
	if f&FlagU != 0 {
		names = append(names, "U")
	}
	if f&FlagX != 0 {
		names = append(names, "x")
	}
	return names
}

func renderTerms(terms []ClassTerm) []map[string]any {
	var out []map[string]any
	for _, t := range terms {
		m := map[string]any{"neg": t.Neg}
		if t.Prop != "" {
			m["prop"] = t.Prop
		} else {
			m["lo"] = t.Lo
			m["hi"] = t.Hi
		}
		out = append(out, m)
	}
	return out
}

func renderClass(c *Class) string {
	s := "["
	if c.Negated {
		s += "^"
	}
	for _, t := range c.Terms {
		if t.Prop != "" {
			s += "\\" + t.Prop
			continue
		}
		if t.Lo == t.Hi {
			s += string(t.Lo)
		} else {
			s += string(t.Lo) + "-" + string(t.Hi)
		}
	}
	return s + "]"
}

func lookText(l *Look) string {
	switch {
	case l.Behind && l.Neg:
		return "(?<!)"
	case l.Behind:
		return "(?<=)"
	case l.Neg:
		return "(?!)"
	default:
		return "(?=)"
	}
}

func backrefText(b *Backref) string {
	if b.Name != "" {
		return "\\k<" + b.Name + ">"
	}
	return "\\" + itoa(b.ID)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}

// ASTJSON returns JSON for direct embedding.
func ASTJSON(p *Pattern) ([]byte, error) {
	return json.Marshal(MarshalAST(p))
}
