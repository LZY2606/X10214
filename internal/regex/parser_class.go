package regex

import (
	"strings"
)

func (p *parser) parseClass(flags Flags) (*Node, error) {
	node := &Node{ID: p.newID(), Kind: NClass, CI: flags.I}
	if r, ok := p.peek(); ok && r == '^' {
		node.Negated = true
		p.eat()
	}
	first := true
	for {
		r, ok := p.peek()
		if !ok {
			return nil, p.errf("unterminated character class")
		}
		if r == ']' && !first {
			p.eat()
			break
		}
		first = false
		rr, builtin, err := p.readClassAtom(flags)
		if err != nil {
			return nil, err
		}
		// range?
		if !builtin {
			if c, ok := p.peek(); ok && c == '-' {
				if nx, ok := p.peekAt(1); ok && nx != ']' {
					p.eat() // '-'
					hi, hb, err := p.readClassAtom(flags)
					if err != nil {
						return nil, err
					}
					if hb {
						return nil, p.errf("range end cannot be a character class escape")
					}
					if hi.Lo < rr.Lo {
						return nil, p.errf("invalid character range %c-%c", rr.Lo, hi.Lo)
					}
					rr.Hi = hi.Lo
				}
			}
		}
		node.Ranges = append(node.Ranges, rr)
	}
	return node, nil
}

func (p *parser) peekAt(n int) (rune, bool) {
	if p.pos+n >= len(p.src) {
		return 0, false
	}
	return p.src[p.pos+n], true
}

// readClassAtom returns one range element. builtin means Kind != "".
func (p *parser) readClassAtom(flags Flags) (RuneRange, bool, error) {
	r := p.eat()
	if r == '\\' {
		next, ok := p.peek()
		if !ok {
			return RuneRange{}, false, p.errf("trailing backslash in class")
		}
		p.eat()
		switch next {
		case 'd', 'D', 'w', 'W', 's', 'S':
			kind := map[rune]string{'d': "digit", 'w': "word", 's': "space"}[next|('a'-'A')]
			neg := next == 'D' || next == 'W' || next == 'S'
			return RuneRange{Kind: kind, Neg: neg}, true, nil
		case 'p', 'P':
			name, err := p.readPropName()
			if err != nil {
				return RuneRange{}, false, err
			}
			return RuneRange{Kind: "unicode:" + name, Neg: next == 'P'}, true, nil
		case 'a':
			return RuneRange{Lo: '\a', Hi: '\a'}, false, nil
		case 'f':
			return RuneRange{Lo: '\f', Hi: '\f'}, false, nil
		case 'n':
			return RuneRange{Lo: '\n', Hi: '\n'}, false, nil
		case 'r':
			return RuneRange{Lo: '\r', Hi: '\r'}, false, nil
		case 't':
			return RuneRange{Lo: '\t', Hi: '\t'}, false, nil
		case 'v':
			return RuneRange{Lo: '\v', Hi: '\v'}, false, nil
		case 'x':
			cp, err := p.readHexEscape(2)
			if err != nil {
				return RuneRange{}, false, err
			}
			return RuneRange{Lo: cp, Hi: cp}, false, nil
		case 'u':
			cp, err := p.readHexEscape(4)
			if err != nil {
				return RuneRange{}, false, err
			}
			return RuneRange{Lo: cp, Hi: cp}, false, nil
		case 'U':
			cp, err := p.readHexEscape(8)
			if err != nil {
				return RuneRange{}, false, err
			}
			return RuneRange{Lo: cp, Hi: cp}, false, nil
		default:
			return RuneRange{Lo: next, Hi: next}, false, nil
		}
	}
	return RuneRange{Lo: r, Hi: r}, false, nil
}

// describeNode renders a compact human-readable label for traces/UI.
func describeNode(n *Node) string {
	var b strings.Builder
	writeNode(&b, n)
	return b.String()
}

func writeNode(b *strings.Builder, n *Node) {
	switch n.Kind {
	case NEmpty:
		b.WriteString("ε")
	case NChar:
		b.WriteRune('\'')
		b.WriteRune(n.Value)
		b.WriteRune('\'')
	case NDot:
		b.WriteByte('.')
	case NAnchor:
		b.WriteString(n.Anchor)
	case NClass:
		writeClass(b, n)
	case NBackref:
		b.WriteString("\\")
		b.WriteString(itoa(n.RefIndex))
	case NGroup:
		b.WriteByte('(')
		if n.Name != "" {
			b.WriteString("?<" + n.Name + ">")
		}
		writeNode(b, n.Body)
		b.WriteByte(')')
	case NLook:
		b.WriteString("(?")
		if n.Behind {
			b.WriteByte('<')
		}
		if n.Negative {
			b.WriteByte('!')
		} else {
			b.WriteByte('=')
		}
		writeNode(b, n.Body)
		b.WriteString(")")
	case NConcat:
		for i, c := range n.Children {
			if i > 0 {
				b.WriteByte(' ')
			}
			writeNode(b, c)
		}
	case NAlt:
		for i, c := range n.Children {
			if i > 0 {
				b.WriteString(" | ")
			}
			writeNode(b, c)
		}
	case NRepeat:
		writeNode(b, n.Body)
		writeQuant(b, n)
	case NFlagScope:
		b.WriteString("(?" + n.SetFlags)
		if n.ClearFlags != "" {
			b.WriteString("-" + n.ClearFlags)
		}
		b.WriteByte(':')
		writeNode(b, n.Body)
		b.WriteString(")")
	}
}

func writeQuant(b *strings.Builder, n *Node) {
	switch {
	case n.Min == 0 && n.Max == -1:
		b.WriteByte('*')
	case n.Min == 1 && n.Max == -1:
		b.WriteByte('+')
	case n.Min == 0 && n.Max == 1:
		b.WriteByte('?')
	default:
		b.WriteByte('{')
		b.WriteString(itoa(n.Min))
		if n.Max == -1 {
			b.WriteString(",}")
		} else if n.Max != n.Min {
			b.WriteString("," + itoa(n.Max) + "}")
		} else {
			b.WriteByte('}')
		}
	}
	if n.Lazy {
		b.WriteByte('?')
	}
}

func writeClass(b *strings.Builder, n *Node) {
	b.WriteByte('[')
	if n.Negated {
		b.WriteByte('^')
	}
	for _, rr := range n.Ranges {
		if rr.Kind != "" {
			if rr.Neg {
				b.WriteByte('\\')
				switch rr.Kind {
				case "digit":
					b.WriteByte('D')
				case "word":
					b.WriteByte('W')
				case "space":
					b.WriteByte('S')
				default:
					b.WriteString("P{" + strings.TrimPrefix(rr.Kind, "unicode:") + "}")
				}
			} else if strings.HasPrefix(rr.Kind, "unicode:") {
				b.WriteString("\\p{" + strings.TrimPrefix(rr.Kind, "unicode:") + "}")
			} else {
				b.WriteString("\\" + rr.Kind[:1])
			}
			continue
		}
		b.WriteRune(rr.Lo)
		if rr.Hi > rr.Lo {
			b.WriteByte('-')
			b.WriteRune(rr.Hi)
		}
	}
	b.WriteByte(']')
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
