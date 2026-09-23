package syntax

import (
	"fmt"
	"strings"
	"unicode"
)

// Kind identifies an AST node.
type Kind uint8

const (
	KEmpty   Kind = iota // epsilon
	KConcat              // sequence of children
	KAlt                 // alternation children
	KChar                // one rune (or a raw byte when RawByte>=0)
	KAny                 // .
	KClass               // character class
	KGroup               // capturing / non-capturing group
	KRepeat              // quantifier
	KBackref             // \N or \k<name>
	KAnchor              // ^ $ \A \z \b \B
	KLook                // lookaround
)

func (k Kind) String() string {
	switch k {
	case KEmpty:
		return "Empty"
	case KConcat:
		return "Concat"
	case KAlt:
		return "Alt"
	case KChar:
		return "Char"
	case KAny:
		return "Any"
	case KClass:
		return "Class"
	case KGroup:
		return "Group"
	case KRepeat:
		return "Repeat"
	case KBackref:
		return "Backref"
	case KAnchor:
		return "Anchor"
	case KLook:
		return "Look"
	}
	return "?"
}

// ClassPart is one member of a character class: a rune singleton, a rune
// range, or a Perl/POSIX escape class.
type ClassPart struct {
	Lo, Hi rune // inclusive rune range; Lo==Hi for a singleton
	Class  byte // one of 0,'d','D','w','W','s','S','p'
	Prop   string
	Neg    bool // negated \P
	Raw    bool // range is a raw byte (Lo 0..255)
}

// Node is the AST element.
type Node struct {
	Kind Kind

	// KChar
	Rune    rune
	RawByte int // -1 unless this node matches a raw byte (e.g. \x80)

	// KClass
	Parts   []ClassPart
	Negated bool

	// KGroup
	Index int    // capture index, 0 for non-capturing
	Name  string // named capture, "" if unnamed
	Caps  bool   // capturing?

	// KRepeat
	Min, Max int // Max == -1 means unbounded
	Greedy   bool

	// KBackref
	RefIndex int
	RefName  string

	// KAnchor
	Anchor byte

	// KLook
	Positive bool
	Behind   bool

	// Flags active while this node/region was parsed (msi)
	Flags uint8

	Child  *Node
	Kids   []*Node
	Source string // slice of the original pattern that produced this node
}

// Flags bits.
const (
	FlagI uint8 = 1 << iota // (?i)
	FlagM                   // (?m)
	FlagS                   // (?s)
)

// NewNode constructs a node.
func NewNode(k Kind) *Node { return &Node{Kind: k, RawByte: -1, Min: 0, Max: 0} }

// WriteTo renders a debug S-expression into b.
func (n *Node) WriteTo(b *strings.Builder, depth int) {
	if n == nil {
		b.WriteString("nil")
		return
	}
	indent := strings.Repeat("  ", depth)
	fmt.Fprintf(b, "%s(%s", indent, n.Kind)
	switch n.Kind {
	case KChar:
		if n.RawByte >= 0 {
			fmt.Fprintf(b, " byte=%02x", n.RawByte)
		} else {
			fmt.Fprintf(b, " %q", n.Rune)
		}
	case KClass:
		if n.Negated {
			b.WriteString(" neg")
		}
		for _, p := range n.Parts {
			b.WriteString(" ")
			b.WriteString(classPartString(p))
		}
	case KAny:
	case KGroup:
		if n.Caps {
			if n.Name != "" {
				fmt.Fprintf(b, " #%d %q", n.Index, n.Name)
			} else {
				fmt.Fprintf(b, " #%d", n.Index)
			}
		}
	case KRepeat:
		fmt.Fprintf(b, " {%d,", n.Min)
		if n.Max < 0 {
			b.WriteString("inf}")
		} else {
			fmt.Fprintf(b, "%d}", n.Max)
		}
		if !n.Greedy {
			b.WriteString(" lazy")
		}
	case KBackref:
		if n.RefName != "" {
			fmt.Fprintf(b, " name=%q", n.RefName)
		} else {
			fmt.Fprintf(b, " #%d", n.RefIndex)
		}
	case KAnchor:
		fmt.Fprintf(b, " %q", n.Anchor)
	case KLook:
		if n.Behind {
			b.WriteString(" behind")
		}
		if !n.Positive {
			b.WriteString(" negative")
		}
	}
	if n.Flags != 0 {
		fmt.Fprintf(b, " flags=%s", flagString(n.Flags))
	}
	if n.Child != nil {
		b.WriteString("\n")
		n.Child.WriteTo(b, depth+1)
		b.WriteString("\n  " + indent + ")")
		return
	}
	if len(n.Kids) > 0 {
		b.WriteString("\n")
		for i, k := range n.Kids {
			k.WriteTo(b, depth+1)
			if i < len(n.Kids)-1 {
				b.WriteString("\n")
			}
		}
		b.WriteString("\n  " + indent + ")")
		return
	}
	b.WriteString(")")
}

// SExpr returns the S-expression rendering.
func (n *Node) SExpr() string {
	var b strings.Builder
	n.WriteTo(&b, 0)
	return b.String()
}

func classPartString(p ClassPart) string {
	if p.Class != 0 {
		neg := ""
		if p.Neg {
			neg = "P"
		}
		return fmt.Sprintf("\\%s%c", neg, p.Class)
	}
	if p.Raw {
		if p.Lo == p.Hi {
			return fmt.Sprintf("byte:%02x", p.Lo)
		}
		return fmt.Sprintf("byte:%02x-%02x", p.Lo, p.Hi)
	}
	if p.Lo == p.Hi {
		return fmt.Sprintf("%q", p.Lo)
	}
	return fmt.Sprintf("%q-%q", p.Lo, p.Hi)
}

func flagString(f uint8) string {
	var s string
	if f&FlagI != 0 {
		s += "i"
	}
	if f&FlagM != 0 {
		s += "m"
	}
	if f&FlagS != 0 {
		s += "s"
	}
	if s == "" {
		return "-"
	}
	return s
}

// Matches reports whether the class accepts the rune/byte.
func (n *Node) Matches(r rune, raw byte, isRaw bool, flags uint8) bool {
	hit := false
	for _, p := range n.Parts {
		if p.Class != 0 {
			if classMatches(p, r, raw, isRaw, flags) {
				hit = true
				break
			}
			continue
		}
		if p.Raw {
			if isRaw && int(raw) >= int(p.Lo) && int(raw) <= int(p.Hi) {
				hit = true
				break
			}
			continue
		}
		if !isRaw && runeIn(r, p, flags) {
			hit = true
			break
		}
	}
	if n.Negated {
		return !hit
	}
	return hit
}

func runeIn(r rune, p ClassPart, flags uint8) bool {
	if r >= p.Lo && r <= p.Hi {
		return true
	}
	if flags&FlagI != 0 {
		// case-insensitive range endpoints: expand via simple fold.
		for rf := unicodeSimpleFold(p.Lo); rf != p.Lo; rf = unicodeSimpleFold(rf) {
			if r == rf {
				return true
			}
			if rf > p.Lo && rf <= p.Hi {
				break
			}
		}
		if p.Lo != p.Hi {
			for rf := unicodeSimpleFold(p.Hi); rf != p.Hi; rf = unicodeSimpleFold(rf) {
				if r == rf {
					return true
				}
			}
		}
	}
	return false
}

func classMatches(p ClassPart, r rune, raw byte, isRaw bool, flags uint8) bool {
	var ok bool
	switch p.Class {
	case 'd':
		ok = !isRaw && r >= '0' && r <= '9'
	case 'D':
		ok = isRaw || r < '0' || r > '9'
	case 'w':
		ok = !isRaw && isWord(r)
	case 'W':
		ok = isRaw || !isWord(r)
	case 's':
		ok = !isRaw && isSpace(r)
	case 'S':
		ok = isRaw || !isSpace(r)
	case 'p':
		ok = !isRaw && unicodeCategory(p.Prop, r)
	case 'o':
		ok = !isRaw && posixCategory(p.Prop, r)
	default:
		ok = false
	}
	return ok
}

func posixCategory(name string, r rune) bool {
	switch name {
	case "alpha":
		return unicode.IsLetter(r)
	case "lower":
		return unicode.IsLower(r)
	case "upper":
		return unicode.IsUpper(r)
	case "blank":
		return r == '\t' || r == ' ' || r == 0xA0 || r == 0x1680 ||
			r == 0x2000 || r == 0x2001 || r == 0x2002 || r == 0x2003 ||
			r == 0x2004 || r == 0x2005 || r == 0x2006 || r == 0x2007 ||
			r == 0x2008 || r == 0x2009 || r == 0x200A || r == 0x202F ||
			r == 0x205F || r == 0x3000
	case "cntrl":
		return unicode.IsControl(r)
	case "punct":
		return unicode.IsPunct(r)
	case "xdigit":
		return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
	case "print":
		return !unicode.IsControl(r)
	case "graph":
		return !unicode.IsSpace(r) && !unicode.IsControl(r)
	}
	return false
}

func isWord(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') || r == '_' || r >= 0x80 && wordUnicode(r)
}

func isSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x85, 0xA0:
		return true
	}
	return r >= 0x2000 && r <= 0x200A || r == 0x2028 || r == 0x2029 ||
		r == 0x202F || r == 0x205F || r == 0x3000 || r == 0x1680 || r == 0xFEFF
}

func wordUnicode(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func unicodeCategory(prop string, r rune) bool {
	if prop == "" {
		return false
	}
	neg := false
	if strings.HasPrefix(prop, "^") {
		prop = prop[1:]
		neg = true
	}
	ok := false
	switch prop {
	case "L", "Letter":
		ok = unicode.IsLetter(r)
	case "Lu", "Uppercase_Letter":
		ok = unicode.IsUpper(r)
	case "Ll", "Lowercase_Letter":
		ok = unicode.IsLower(r)
	case "N", "Number":
		ok = unicode.IsNumber(r)
	case "Nd", "Decimal_Number":
		ok = unicode.IsDigit(r)
	case "Z", "Separator":
		ok = unicode.Is(unicode.Zs, r) || r == 0x2028 || r == 0x2029
	case "P", "Punctuation":
		ok = unicode.IsPunct(r)
	case "S", "Symbol":
		ok = unicode.IsSymbol(r)
	case "M", "Mark":
		ok = unicode.IsMark(r)
	case "C", "Other":
		ok = unicode.IsControl(r) || r == 0xFFFD
	default:
		if table := unicode.Categories[prop]; table != nil {
			ok = unicode.Is(table, r)
		} else {
			ok = unicode.Is(unicode.Scripts[prop], r)
		}
	}
	return ok != neg
}

func unicodeSimpleFold(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - 'a' + 'A'
	}
	if r >= 'A' && r <= 'Z' {
		return r - 'A' + 'a'
	}
	return unicode.SimpleFold(r)
}
