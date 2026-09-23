package engine

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type parser struct {
	src []byte
	pos int
	pat *Pattern
}

func Parse(pattern string) (*Pattern, error) {
	p := &parser{src: []byte(pattern), pat: &Pattern{Names: map[string]int{}}}
	p.pat.Groups = append(p.pat.Groups, GroupName{Index: 0})
	root, err := p.parseAlternation()
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.src) {
		return nil, fmt.Errorf("unexpected trailing input at byte %d", p.pos)
	}
	if c, ok := root.(*ConcatNode); ok && len(c.Items) == 0 {
		root = c
	}
	p.pat.Root = root
	AssignIDs(p.pat)
	return p.pat, nil
}

func (p *parser) parseAlternation() (Node, error) {
	var branches []Node
	first, err := p.parseConcatenation()
	if err != nil {
		return nil, err
	}
	branches = append(branches, first)
	for p.peekByte() == '|' {
		p.pos++
		b, err := p.parseConcatenation()
		if err != nil {
			return nil, err
		}
		branches = append(branches, b)
	}
	if len(branches) == 1 {
		return branches[0], nil
	}
	return &AltNode{Branches: branches}, nil
}

func (p *parser) parseConcatenation() (Node, error) {
	var items []Node
	for p.pos < len(p.src) {
		b := p.src[p.pos]
		if b == '|' || b == ')' {
			break
		}
		n, err := p.parseAtomWithQuantifier()
		if err != nil {
			return nil, err
		}
		if n != nil {
			items = append(items, n)
		}
	}
	return &ConcatNode{Items: items}, nil
}

func (p *parser) parseAtomWithQuantifier() (Node, error) {
	atom, err := p.parseAtom()
	if err != nil || atom == nil {
		return atom, err
	}
	return p.parseQuantifier(atom)
}

func (p *parser) parseQuantifier(atom Node) (Node, error) {
	b := p.peekByte()
	if b != '*' && b != '+' && b != '?' && b != '{' {
		return atom, nil
	}
	start := p.pos
	var min, max int
	switch b {
	case '*':
		p.pos++
		min, max = 0, -1
	case '+':
		p.pos++
		min, max = 1, -1
	case '?':
		p.pos++
		min, max = 0, 1
	case '{':
		saved := p.pos
		lo, loOK, hi, hiOK, ok := p.tryBraceRepeat()
		if !ok {
			p.pos = saved
			return atom, nil
		}
		p.pos = saved
		_ = p.consumeByte('{')
		min = lo
		if !loOK {
			min = 0
		}
		if hiOK {
			max = hi
		} else {
			max = -1
		}
		_ = start
	}
	lazy, possessive := false, false
	if p.peekByte() == '?' {
		lazy = true
		p.pos++
	}
	if p.peekByte() == '+' {
		possessive = true
		p.pos++
	}
	if lazy && possessive {
		return nil, fmt.Errorf("quantifier cannot be both lazy and possessive at byte %d", start)
	}
	return &RepeatNode{Child: atom, Min: min, Max: max, Lazy: lazy, Possessive: possessive}, nil
}

func (p *parser) tryBraceRepeat() (int, bool, int, bool, bool) {
	saved := p.pos
	if p.consumeByte('{') != nil {
		return 0, false, 0, false, false
	}
	loText := p.takeDigits()
	loOK := len(loText) > 0
	lo := 0
	if loOK {
		if v, err := strconv.Atoi(loText); err == nil {
			lo = v
		}
	}
	hi, hiOK := lo, loOK
	if p.consumeByte('}') == nil {
		return lo, loOK, hi, hiOK, true
	}
	if p.consumeByte(',') != nil {
		p.pos = saved
		return 0, false, 0, false, false
	}
	hiText := p.takeDigits()
	if p.consumeByte('}') != nil {
		p.pos = saved
		return 0, false, 0, false, false
	}
	if len(hiText) > 0 {
		v, err := strconv.Atoi(hiText)
		if err != nil {
			p.pos = saved
			return 0, false, 0, false, false
		}
		hi, hiOK = v, true
	} else {
		hi, hiOK = -1, true
	}
	return lo, loOK, hi, hiOK, true
}

func (p *parser) takeDigits() string {
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
		p.pos++
	}
	return string(p.src[start:p.pos])
}

func (p *parser) parseAtom() (Node, error) {
	switch b := p.peekByte(); b {
	case '(':
		return p.parseGroup()
	case '[':
		return p.parseClass()
	case '.':
		p.pos++
		return &DotNode{}, nil
	case '^':
		p.pos++
		return &AnchorNode{Kind: "start-text"}, nil
	case '$':
		p.pos++
		return &AnchorNode{Kind: "end-text"}, nil
	case '\\':
		return p.parseEscape()
	case '*', '+', '?', '}':
		return nil, fmt.Errorf("unmatched quantifier %q at byte %d", string(rune(b)), p.pos)
	case ')':
		return nil, nil
	default:
		r, size := utf8.DecodeRune(p.src[p.pos:])
		p.pos += size
		return &LiteralNode{Rune: r, Invalid: r == utf8.RuneError && size == 1}, nil
	}
}

func (p *parser) parseGroup() (Node, error) {
	start := p.pos
	if p.consumeByte('(') != nil {
		return nil, fmt.Errorf("missing ( at %d", start)
	}
	capture, atomic := true, false
	var look string
	if p.peekByte() == '?' {
		p.pos++
		switch p.peekByte() {
		case ':':
			p.pos++
			capture = false
		case '=':
			p.pos++
			capture = false
			look = "ahead"
		case '!':
			p.pos++
			capture = false
			look = "neg-ahead"
		case '<':
			p.pos++
			switch p.peekByte() {
			case '=':
				p.pos++
				capture = false
				look = "behind"
			case '!':
				p.pos++
				capture = false
				look = "neg-behind"
			default:
				name, err := p.parseGroupName()
				if err != nil {
					return nil, err
				}
				p.pat.GroupCount++
				idx := p.pat.GroupCount
				p.pat.Groups = append(p.pat.Groups, GroupName{idx, name})
				p.pat.Names[name] = idx
				child, err := p.untilGroupEnd()
				if err != nil {
					return nil, err
				}
				return &GroupNode{Index: idx, Name: name, Capture: true, Child: child}, nil
			}
		case '>':
			p.pos++
			capture = false
			atomic = true
		default:
			return nil, fmt.Errorf("unsupported group flag at byte %d", p.pos)
		}
	}
	idx := 0
	if capture {
		p.pat.GroupCount++
		idx = p.pat.GroupCount
		p.pat.Groups = append(p.pat.Groups, GroupName{Index: idx})
	}
	child, err := p.untilGroupEnd()
	if err != nil {
		return nil, err
	}
	if look != "" {
		return &LookaroundNode{Child: child, Behind: look == "behind" || look == "neg-behind", Negated: look == "neg-ahead" || look == "neg-behind"}, nil
	}
	return &GroupNode{Index: idx, Capture: capture, Atomic: atomic, Child: child}, nil
}

func (p *parser) parseGroupName() (string, error) {
	start := p.pos
	if p.pos >= len(p.src) {
		return "", fmt.Errorf("missing group name at byte %d", start)
	}
	first, size := utf8.DecodeRune(p.src[p.pos:])
	if first != '_' && !unicode.IsLetter(first) {
		return "", fmt.Errorf("invalid group name start at byte %d", start)
	}
	p.pos += size
	for p.pos < len(p.src) {
		r, sz := utf8.DecodeRune(p.src[p.pos:])
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			break
		}
		p.pos += sz
	}
	name := string(p.src[start:p.pos])
	if p.consumeByte('>') != nil {
		return "", fmt.Errorf("group name must end with > at byte %d", p.pos)
	}
	if existing, ok := p.pat.Names[name]; ok {
		return "", fmt.Errorf("duplicate group name %q already used by group %d", name, existing)
	}
	return name, nil
}

func (p *parser) untilGroupEnd() (Node, error) {
	child, err := p.parseAlternation()
	if err != nil {
		return nil, err
	}
	if p.consumeByte(')') != nil {
		return nil, fmt.Errorf("missing ) at byte %d", p.pos)
	}
	return child, nil
}

func (p *parser) peekByte() byte {
	if p.pos >= len(p.src) {
		return 0
	}
	return p.src[p.pos]
}

func (p *parser) consumeByte(b byte) error {
	if p.pos >= len(p.src) || p.src[p.pos] != b {
		return fmt.Errorf("expected %q at byte %d", b, p.pos)
	}
	p.pos++
	return nil
}

type classElem struct {
	ranges  [][2]rune
	ascii   string
	cat     *unicode.RangeTable
	catName string
}

func (p *parser) parseClass() (Node, error) {
	p.pos++
	negated := false
	if p.peekByte() == '^' {
		negated = true
		p.pos++
	}
	first := true
	cls := &CharClassNode{Negated: negated}
	for p.pos < len(p.src) && (first || p.peekByte() != ']') {
		first = false
		if p.peekByte() == ']' {
			break
		}
		elem, literal, err := p.parseClassElem()
		if err != nil {
			return nil, err
		}
		if p.peekByte() == '-' && p.pos+1 < len(p.src) && p.src[p.pos+1] != ']' {
			dash := p.pos
			p.pos++
			second, secondLiteral, err := p.parseClassElem()
			if err != nil {
				return nil, err
			}
			if len(elem.ranges) != 1 || len(second.ranges) != 1 || elem.ascii != "" || elem.cat != nil || second.ascii != "" || second.cat != nil || !literal || !secondLiteral {
				return nil, fmt.Errorf("only literal range is supported at byte %d", dash)
			}
			lo, hi := elem.ranges[0][0], second.ranges[0][0]
			if lo > hi {
				return nil, fmt.Errorf("invalid character range at byte %d", dash)
			}
			cls.Ranges = append(cls.Ranges, [2]rune{lo, hi})
			continue
		}
		cls.Ranges = append(cls.Ranges, elem.ranges...)
		if elem.ascii != "" {
			cls.ASCIIClass = elem.ascii
		}
		if elem.cat != nil {
			cls.Categories = append(cls.Categories, elem.cat)
			cls.CategoryNames = append(cls.CategoryNames, elem.catName)
		}
	}
	if p.consumeByte(']') != nil {
		return nil, fmt.Errorf("unterminated character class at byte %d", p.pos)
	}
	return cls, nil
}

func (p *parser) parseClassElem() (classElem, bool, error) {
	if p.peekByte() == '\\' {
		p.pos++
		return p.parseEscapedClassCode()
	}
	r, size, invalid := p.decodeInputRune()
	p.pos += size
	return classElem{ranges: [][2]rune{{r, r}}}, !invalid, nil
}

func (p *parser) parseEscape() (Node, error) {
	start := p.pos
	if p.consumeByte('\\') != nil {
		return nil, fmt.Errorf("missing backslash at %d", start)
	}
	if p.pos >= len(p.src) {
		return nil, fmt.Errorf("trailing backslash at %d", start)
	}
	b := p.src[p.pos]
	if b >= '1' && b <= '9' {
		n := p.takeDigits()
		idx, _ := strconv.Atoi(n)
		return &BackrefNode{Index: idx}, nil
	}
	if b == 'k' {
		p.pos++
		if p.consumeByte('<') != nil {
			return nil, fmt.Errorf("named backreference requires \\k<name> at %d", start)
		}
		nameStart := p.pos
		for p.pos < len(p.src) && p.src[p.pos] != '>' {
			p.pos++
		}
		name := string(p.src[nameStart:p.pos])
		if p.consumeByte('>') != nil || name == "" {
			return nil, fmt.Errorf("invalid named backreference at %d", start)
		}
		idx, ok := p.pat.Names[name]
		if !ok {
			return nil, fmt.Errorf("unknown named backreference %q", name)
		}
		return &BackrefNode{Index: idx, Name: name}, nil
	}
	if b == 'b' {
		p.pos++
		return &AnchorNode{Kind: "word-boundary"}, nil
	}
	if b == 'B' {
		p.pos++
		return &AnchorNode{Kind: "not-word-boundary"}, nil
	}
	if b == 'A' {
		p.pos++
		return &AnchorNode{Kind: "start-text"}, nil
	}
	if b == 'z' {
		p.pos++
		return &AnchorNode{Kind: "end-text"}, nil
	}
	if b == 'Q' {
		p.pos++
		qStart := p.pos
		end := strings.Index(string(p.src[p.pos:]), `\E`)
		if end < 0 {
			return nil, fmt.Errorf("unterminated \\Q at %d", qStart)
		}
		lit := p.src[p.pos : p.pos+end]
		p.pos += end + 2
		var items []Node
		for len(lit) > 0 {
			r, size := utf8.DecodeRune(lit)
			items = append(items, &LiteralNode{Rune: r, Invalid: r == utf8.RuneError && size == 1})
			lit = lit[size:]
		}
		return &ConcatNode{Items: items}, nil
	}
	elem, _, err := p.parseEscapedClassCode()
	if err != nil {
		return nil, err
	}
	if elem.ascii != "" || elem.cat != nil {
		var cats []*unicode.RangeTable
		var names []string
		if elem.cat != nil {
			cats, names = []*unicode.RangeTable{elem.cat}, []string{elem.catName}
		}
		return &CharClassNode{Ranges: elem.ranges, ASCIIClass: elem.ascii, Categories: cats, CategoryNames: names}, nil
	}
	if len(elem.ranges) == 1 {
		return &LiteralNode{Rune: elem.ranges[0][0]}, nil
	}
	return &CharClassNode{Ranges: elem.ranges}, nil
}

func (p *parser) parseEscapedClassCode() (classElem, bool, error) {
	start := p.pos
	b := p.src[p.pos]
	p.pos++
	simple := func(rs ...rune) classElem {
		var ranges [][2]rune
		for _, r := range rs {
			ranges = append(ranges, [2]rune{r, r})
		}
		return classElem{ranges: ranges}
	}
	switch b {
	case 'n':
		return simple('\n'), true, nil
	case 'r':
		return simple('\r'), true, nil
	case 't':
		return simple('\t'), true, nil
	case 'f':
		return simple('\f'), true, nil
	case 'v':
		return classElem{ranges: [][2]rune{{'\n', '\r'}, {'\t', '\t'}, {0x0b, 0x0b}, {0x0c, 0x0c}, {0x85, 0x85}, {0x2028, 0x2028}, {0x2029, 0x2029}}}, true, nil
	case '0':
		return simple(0), true, nil
	case 'x':
		r, err := p.parseHexEscape()
		if err != nil {
			return classElem{}, false, err
		}
		return simple(r), true, nil
	case 'd':
		return classElem{ascii: "d"}, true, nil
	case 'D':
		return classElem{ascii: "D"}, true, nil
	case 'w':
		return classElem{ascii: "w"}, true, nil
	case 'W':
		return classElem{ascii: "W"}, true, nil
	case 's':
		return classElem{ascii: "s"}, true, nil
	case 'S':
		return classElem{ascii: "S"}, true, nil
	case 'p', 'P':
		neg := b == 'P'
		name, err := p.parsePropertyName()
		if err != nil {
			return classElem{}, false, err
		}
		tbl, canonical, err := propertyTable(name)
		if err != nil {
			return classElem{}, false, err
		}
		if neg {
			r1, _ := unicode.Digit, ""
			_ = r1
			return classElem{cat: invertRangeTable(tbl), catName: "Not_" + canonical}, true, nil
		}
		return classElem{cat: tbl, catName: canonical}, true, nil
	default:
		r, size, invalid := decodeRuneAt(p.src, p.pos-1)
		_ = size
		p.pos = start + 1
		return classElem{ranges: [][2]rune{{r, r}}}, !invalid, nil
	}
}

func (p *parser) parseHexEscape() (rune, error) {
	if p.consumeByte('{') == nil {
		var text strings.Builder
		for p.pos < len(p.src) && p.src[p.pos] != '}' {
			text.WriteByte(p.src[p.pos])
			p.pos++
		}
		if p.consumeByte('}') != nil || text.Len() == 0 {
			return 0, fmt.Errorf("invalid \\x{} escape")
		}
		v, err := strconv.ParseInt(text.String(), 16, 32)
		if err != nil || v > unicode.MaxRune {
			return 0, fmt.Errorf("invalid rune in \\x{} escape")
		}
		return rune(v), nil
	}
	if p.pos+2 > len(p.src) {
		return 0, fmt.Errorf("short \\x escape")
	}
	v, err := strconv.ParseInt(string(p.src[p.pos:p.pos+2]), 16, 32)
	p.pos += 2
	if err != nil {
		return 0, fmt.Errorf("invalid \\x escape")
	}
	return rune(v), nil
}

func (p *parser) parsePropertyName() (string, error) {
	if p.consumeByte('{') == nil {
		start := p.pos
		for p.pos < len(p.src) && p.src[p.pos] != '}' {
			p.pos++
		}
		name := string(p.src[start:p.pos])
		if p.consumeByte('}') != nil || name == "" {
			return "", fmt.Errorf("invalid unicode property")
		}
		return name, nil
	}
	if p.pos >= len(p.src) {
		return "", fmt.Errorf("short unicode property")
	}
	name := string(p.src[p.pos])
	p.pos++
	return name, nil
}

func (p *parser) decodeInputRune() (rune, int, bool) {
	return decodeRuneAt(p.src, p.pos)
}

func decodeRuneAt(src []byte, pos int) (rune, int, bool) {
	r, size := utf8.DecodeRune(src[pos:])
	return r, size, r != utf8.RuneError || size != 1
}
