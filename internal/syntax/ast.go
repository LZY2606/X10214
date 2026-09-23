// Package syntax parses regular expressions into an AST.
// The engine never delegates matching to the host regexp package.
package syntax

// Version is the pinned syntax version reported in every result.
const Version = "rxsyn/1.0"

// MaxRepeat bounds {n,m} expansion at compile time.
const MaxRepeat = 256

type Kind int

const (
	KEmpty Kind = iota
	KLiteral
	KAny // any rune except '\n'
	KClass
	KConcat
	KAlternate
	KRepeat
	KGroup
	KBackref
	KAnchor
	KLook
)

type ClassRange struct {
	Lo, Hi rune
}

type Anchor int

const (
	ABeginText Anchor = iota // ^ or \A
	AEndText                 // $ or \z
	AWordBoundary            // \b
	ANotWordBoundary         // \B
)

type Class struct {
	Ranges []ClassRange
	Negate bool
}

func (c *Class) Contains(r rune) bool {
	in := false
	for _, rg := range c.Ranges {
		if r >= rg.Lo && r <= rg.Hi {
			in = true
			break
		}
	}
	return in != c.Negate
}

type Node struct {
	Kind   Kind
	Runes  []rune  // KLiteral
	Class  *Class  // KClass: match one rune
	Sub    []*Node // KConcat / KAlternate
	Child  *Node   // KRepeat / KGroup / KLook
	Min    int     // KRepeat
	Max    int     // KRepeat, -1 = unbounded
	Lazy   bool    // KRepeat
	Group  int     // KGroup / KBackref (1-based)
	Name   string  // KGroup / KBackref name
	Anchor Anchor  // KAnchor
	Negate bool    // KLook
	Behind bool    // KLook
}
