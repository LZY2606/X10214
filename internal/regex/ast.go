package regex

// SyntaxVersion pins the pattern grammar understood by this implementation.
const SyntaxVersion = "CTR-1.0"

// UnicodeDataVersion pins the Unicode character database used by the built-in
// char-class tables. The engine performs all property/category lookups against
// the Go standard library unicode tables shipped with the building toolchain;
// this constant records the UCD edition those tables correspond to.
const UnicodeDataVersion = "16.0.0"

// Flags holds the mode bits a pattern can toggle with (?imnsux) groups.
type Flags struct {
	I bool // case-insensitive
	M bool // multiline: ^ and $ match at line boundaries
	S bool // dot-all: dot matches newline
	U bool // ungreedy: greedy <-> lazy swap (documented, supported)
}

func (f Flags) with(letter rune, on bool) (Flags, bool) {
	g := f
	switch letter {
	case 'i':
		g.I = on
	case 'm':
		g.M = on
	case 's':
		g.S = on
	case 'u':
		g.U = on
	default:
		return f, false
	}
	return g, true
}

// NodeKind enumerates AST node kinds.
type NodeKind string

const (
	NEmpty     NodeKind = "empty"
	NChar      NodeKind = "char"
	NClass     NodeKind = "class"
	NDot       NodeKind = "dot"
	NAnchor    NodeKind = "anchor"
	NGroup     NodeKind = "group"
	NBackref   NodeKind = "backref"
	NLook      NodeKind = "lookaround"
	NConcat    NodeKind = "concat"
	NAlt       NodeKind = "alternation"
	NRepeat    NodeKind = "repeat"
	NFlagScope NodeKind = "flag_scope"
)

// Node is an AST node. ID is a dense 1-based identifier used for breakpoints.
type Node struct {
	ID   int      `json:"id"`
	Kind NodeKind `json:"kind"`

	// char / anchor
	Value rune `json:"value,omitempty"`

	// class
	Ranges   []RuneRange `json:"ranges,omitempty"`
	Negated  bool        `json:"negated,omitempty"`
	CI       bool        `json:"ci,omitempty"`
	ClassAST bool        `json:"class_ast,omitempty"` // unicode property class

	// anchor kinds: "bol","eol","boa","eoa","wb","nwb"
	Anchor string `json:"anchor,omitempty"`

	// group
	Capture int    `json:"capture,omitempty"` // 0 = non-capturing
	Name    string `json:"name,omitempty"`

	// backref
	RefIndex int `json:"ref_index,omitempty"`

	// lookaround
	Negative bool `json:"negative,omitempty"`
	Behind   bool `json:"behind,omitempty"`

	// repeat
	Min  int  `json:"min,omitempty"`
	Max  int  `json:"max,omitempty"` // -1 = unbounded
	Lazy bool `json:"lazy,omitempty"`

	// composite
	Children []*Node `json:"children,omitempty"`
	Body     *Node   `json:"body,omitempty"`

	// flag_scope
	SetFlags   string `json:"set_flags,omitempty"`
	ClearFlags string `json:"clear_flags,omitempty"`
}

// RuneRange is an inclusive range of code points in a character class.
type RuneRange struct {
	Lo   rune   `json:"lo"`
	Hi   rune   `json:"hi"`
	Kind string `json:"kind,omitempty"`  // "" literal range, otherwise "letter","digit","space","word" or "unicode:<name>"
	Neg  bool   `json:"neg_p,omitempty"` // range is a negated built-in (\D etc.)
}

type parser struct {
	src    []rune
	pos    int
	nextID int
	flags  Flags
	groups map[string]int
	ng     int // number of capturing groups
}

// Parse parses pattern into an AST, resolving group numbers and backreferences.
func Parse(pattern string) (*Node, error) {
	p := &parser{src: []rune(pattern), groups: map[string]int{}, ng: 0}
	n, err := p.parseAlt(Flags{})
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.src) {
		return nil, p.errf("unexpected '%c'", p.src[p.pos])
	}
	return n, nil
}

func (p *parser) newID() int {
	p.nextID++
	return p.nextID
}

func (p *parser) errf(format string, args ...any) error {
	return &ParseError{At: p.pos, Msg: sprintf(format, args...)}
}

// ParseError describes a grammar failure with a rune offset.
type ParseError struct {
	At  int    `json:"at"`
	Msg string `json:"msg"`
}

func (e *ParseError) Error() string { return sprintf("syntax error at rune %d: %s", e.At, e.Msg) }

func (p *parser) peek() (rune, bool) {
	if p.pos >= len(p.src) {
		return 0, false
	}
	return p.src[p.pos], true
}

func (p *parser) eat() rune {
	r := p.src[p.pos]
	p.pos++
	return r
}

func (p *parser) parseAlt(flags Flags) (*Node, error) {
	p.flags = flags
	var branches []*Node
	first, err := p.parseConcat(flags)
	if err != nil {
		return nil, err
	}
	branches = append(branches, first)
	for {
		r, ok := p.peek()
		if !ok || r != '|' {
			break
		}
		p.eat()
		b, err := p.parseConcat(flags)
		if err != nil {
			return nil, err
		}
		branches = append(branches, b)
	}
	if len(branches) == 1 {
		return branches[0], nil
	}
	return &Node{ID: p.newID(), Kind: NAlt, Children: branches}, nil
}

func (p *parser) parseConcat(flags Flags) (*Node, error) {
	var kids []*Node
	for {
		r, ok := p.peek()
		if !ok || r == '|' || r == ')' {
			break
		}
		n, err := p.parseAtom(flags)
		if err != nil {
			return nil, err
		}
		n, err = p.parseQuantifier(n, flags)
		if err != nil {
			return nil, err
		}
		if n != nil {
			kids = append(kids, n)
		}
	}
	switch len(kids) {
	case 0:
		return &Node{ID: p.newID(), Kind: NEmpty}, nil
	case 1:
		return kids[0], nil
	default:
		return &Node{ID: p.newID(), Kind: NConcat, Children: kids}, nil
	}
}

func (p *parser) parseQuantifier(atom *Node, flags Flags) (*Node, error) {
	r, ok := p.peek()
	if !ok {
		return atom, nil
	}
	var n *Node
	switch r {
	case '*', '+', '?':
		p.eat()
		n = &Node{ID: p.newID(), Kind: NRepeat, Body: atom}
		switch r {
		case '*':
			n.Min, n.Max = 0, -1
		case '+':
			n.Min, n.Max = 1, -1
		case '?':
			n.Min, n.Max = 0, 1
		}
	case '{':
		save := p.pos
		p.eat()
		lo, hasLo, err := p.readDecimal()
		if err != nil {
			p.pos = save
			return atom, nil
		}
		hi := lo
		unbounded := false
		if c, ok := p.peek(); ok && c == ',' {
			p.eat()
			h, hasHi, e := p.readDecimal()
			if e != nil {
				p.pos = save
				return atom, nil
			}
			if hasHi {
				hi = h
			} else {
				unbounded = true
			}
		}
		c, ok := p.peek()
		if !ok || c != '}' {
			p.pos = save
			return atom, nil
		}
		p.eat()
		_ = hasLo
		n = &Node{ID: p.newID(), Kind: NRepeat, Body: atom, Min: lo}
		if unbounded {
			n.Max = -1
		} else {
			n.Max = hi
			if hi < lo {
				return nil, p.errf("quantifier range out of order: {%d,%d}", lo, hi)
			}
		}
	default:
		return atom, nil
	}
	if c, ok := p.peek(); ok && (c == '?' || c == '+') {
		if c == '+' {
			return nil, p.errf("possessive quantifiers are not supported by this execution model")
		}
		p.eat()
		n.Lazy = !flags.U
	} else if flags.U {
		n.Lazy = true
	}
	return n, nil
}

func (p *parser) readDecimal() (int, bool, error) {
	start := p.pos
	v := 0
	for {
		r, ok := p.peek()
		if !ok || r < '0' || r > '9' {
			break
		}
		v = v*10 + int(p.eat()-'0')
		if v > 1_000_000 {
			return 0, false, p.errf("quantifier bound too large")
		}
	}
	if p.pos == start {
		return 0, false, nil
	}
	return v, true, nil
}
