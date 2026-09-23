package syntax

import "encoding/json"

type jsonNode struct {
	Kind      string      `json:"kind"`
	Pos       int         `json:"pos"`
	End       int         `json:"end"`
	Label     string      `json:"label,omitempty"`
	Negated   bool        `json:"negated,omitempty"`
	Min       *int        `json:"min,omitempty"`
	Max       *int        `json:"max,omitempty"`
	Greedy    *bool       `json:"greedy,omitempty"`
	Index     int         `json:"index,omitempty"`
	Name      string      `json:"name,omitempty"`
	LookAhead bool        `json:"lookAhead,omitempty"`
	LookNeg   bool        `json:"lookNeg,omitempty"`
	LookMax   int         `json:"lookMax,omitempty"`
	Children  []*jsonNode `json:"children,omitempty"`
}

// MarshalAST renders the AST plus pinned grammar metadata as JSON.
func (p *Pattern) MarshalAST() []byte {
	jn := toJSON(p.Root)
	type out struct {
		SyntaxVersion string    `json:"syntaxVersion"`
		NumGroups     int       `json:"numGroups"`
		Groups        []Group   `json:"groups"`
		Root          *jsonNode `json:"root"`
	}
	b, _ := json.Marshal(out{
		SyntaxVersion: SyntaxVersion,
		NumGroups:     p.NumGroups,
		Groups:        p.Groups,
		Root:          jn,
	})
	return b
}

func toJSON(a *Atom) *jsonNode {
	if a == nil {
		return nil
	}
	j := &jsonNode{Kind: string(a.Kind), Pos: a.Pos, End: a.End}
	switch a.Kind {
	case KindLiteral:
		j.Label = string(a.Value)
	case KindAny:
		j.Label = "."
	case KindAnchor:
		j.Label = string(a.Anchor)
		caseKind := a.Kind
		_ = caseKind
	case KindClass, KindProperty:
		j.Negated = a.Negated
		if a.Kind == KindProperty {
			j.Label = a.Prop
		} else {
			j.Label = classLabel(a)
		}
	case KindQuant:
		mn := a.Min
		j.Min = &mn
		if a.Max == -1 {
			v := -1
			j.Max = &v
			j.Label = "∞"
		} else {
			mx := a.Max
			j.Max = &mx
		}
		g := a.Mode == Greedy
		j.Greedy = &g
		j.Children = []*jsonNode{toJSON(a.Target)}
	case KindAlt, KindConcat:
		for _, n := range a.Nodes {
			j.Children = append(j.Children, toJSON(n))
		}
	case KindGroup:
		j.Index = a.Index
		j.Name = a.Name
		j.Children = []*jsonNode{toJSON(a.Target)}
	case KindLook:
		j.LookAhead = a.LookAhead
		j.LookNeg = a.LookNeg
		j.LookMax = a.LookMax
		j.Children = []*jsonNode{toJSON(a.Target)}
	case KindBackref:
		j.Index = a.Index
		j.Label = "\\" + itoa(a.Index)
	}
	return j
}

func classLabel(a *Atom) string {
	s := "["
	if a.Negated {
		s += "^"
	}
	for _, m := range a.Members {
		switch {
		case m.IsRange:
			s += string(m.Lo) + "-" + string(m.Hi)
		case m.IsShort:
			if m.NegProp {
				s += string(m.Short - 'a' + 'A')
			} else {
				s += "\\" + string(m.Short)
			}
		case m.IsProp:
			if m.NegProp {
				s += `\P{` + m.Prop + `}`
			} else {
				s += `\p{` + m.Prop + `}`
			}
		default:
			s += string(m.Single)
		}
	}
	return s + "]"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
