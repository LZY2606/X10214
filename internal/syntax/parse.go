package syntax

import (
	"fmt"
	"unicode/utf8"
)

// ParseError describes a grammar failure with a byte offset.
type ParseError struct {
	Msg    string
	Offset int
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("parse error at byte %d: %s", e.Offset, e.Msg)
}

// parser turns the pattern source into an AST, assigning deterministic IDs.
type parser struct {
	src    string
	pos    int
	nextID int

	// global flags from (?i) anywhere apply to the whole pattern (PCRE-style);
	// group-local scoped flags only apply inside their group.
	globalI, globalS, globalM bool

	// stack of effective scoped flags
	scoped       []Flags
	captureCount int
}

// Parse compiles src into an AST under SyntaxVersion.
func Parse(src string) (*Node, error) {
	p := &parser{src: src, nextID: 1}
	if err := p.scanGlobalFlags(); err != nil {
		return nil, err
	}
	p.pos = 0
	root, err := p.parseAlternation()
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.src) {
		return nil, &ParseError{Msg: "unexpected trailing input", Offset: p.pos}
	}
	if root == nil {
		root = p.newNode(KindEmpty)
	}
	return root, nil
}

// scanGlobalFlags finds top-level modifier groups like (?i) and (?s-m)
// that set global flags; actual parsing still consumes them as groups.
func (p *parser) scanGlobalFlags() error {
	for i := 0; i < len(p.src); i++ {
		switch p.src[i] {
		case '\\':
			i++
		case '[':
			i++
			for i < len(p.src) {
				if p.src[i] == '\\' {
					i += 2
					continue
				}
				if p.src[i] == ']' {
					break
				}
				i++
			}
		case '(':
			if i+2 < len(p.src) && p.src[i+1] == '?' && isFlagChar(p.src[i+2]) {
				j := i + 2
				neg := false
				for j < len(p.src) && p.src[j] != ')' {
					if p.src[j] == ':' {
						break
					}
					c := p.src[j]
					switch c {
					case '-':
						neg = true
					case 'i':
						p.globalI = !neg
					case 's':
						if !neg {
							p.globalS = true
						}
					case 'm':
						if !neg {
							p.globalM = true
						}
					default:
						return &ParseError{Msg: "unknown inline flag " + string(c), Offset: j}
					}
					j++
				}
			}
		}
	}
	return nil
}

func isFlagChar(c byte) bool { return c == 'i' || c == 's' || c == 'm' || c == '-' }

func (p *parser) newNode(k NodeKind) *Node {
	n := &Node{ID: p.nextID, Kind: k}
	p.nextID++
	return n
}

func (p *parser) errf(format string, args ...any) error {
	return &ParseError{Msg: fmt.Sprintf(format, args...), Offset: p.pos}
}

func (p *parser) flags() Flags {
	if len(p.scoped) > 0 {
		return p.scoped[len(p.scoped)-1]
	}
	return Flags{I: p.globalI, S: p.globalS, M: p.globalM}
}

func (p *parser) pushFlags(base Flags, set, unset string) Flags {
	for _, c := range set {
		switch c {
		case 'i':
			base.I = true
		case 's':
			base.S = true
		case 'm':
			base.M = true
		}
	}
	for _, c := range unset {
		switch c {
		case 'i':
			base.I = false
		case 's':
			base.S = false
		case 'm':
			base.M = false
		}
	}
	p.scoped = append(p.scoped, base)
	return base
}

func (p *parser) popFlags() { p.scoped = p.scoped[:len(p.scoped)-1] }

var _ = utf8.RuneError
