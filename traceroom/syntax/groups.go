package syntax

import (
	"unicode/utf8"
)

// parseGroup handles every `( ... )` opener: capture, (?P<..>), (?:),
// (?imsUx:), (?=)/(?!)/(?<=)/(?<!), (?>).
func (p *parser) parseGroup() (Node, error) {
	lo := p.pos
	p.pos++ // consume (
	if p.pos >= len(p.src) {
		return nil, p.errorf("unterminated group")
	}
	if p.src[p.pos] != '?' {
		// plain capturing group
		p.caps++
		id := p.caps
		kid, _, err := p.parseAlternation(')', true)
		if err != nil {
			return nil, err
		}
		p.pos++ // consume )
		grp := &Group{Kid: kid, CapID: id, SrcLo: lo, SrcHi: p.pos}
		return p.parseQuantifier(grp)
	}

	// '?' forms
	p.pos++
	if p.pos >= len(p.src) {
		return nil, p.errorf("unterminated group")
	}
	switch {
	case p.src[p.pos] == ':':
		p.pos++
		kid, _, err := p.parseAlternation(')', true)
		if err != nil {
			return nil, err
		}
		p.pos++
		g := &Group{Kid: kid, CapID: 0, SrcLo: lo, SrcHi: p.pos}
		return p.parseQuantifier(g)
	case p.src[p.pos] == '>':
		p.pos++
		kid, _, err := p.parseAlternation(')', true)
		if err != nil {
			return nil, err
		}
		p.pos++
		g := &Atomic{Kid: kid, SrcLo: lo, SrcHi: p.pos}
		return p.parseQuantifier(g)
	case p.src[p.pos] == '=' || p.src[p.pos] == '!':
		neg := p.src[p.pos] == '!'
		p.pos++
		kid, _, err := p.parseAlternation(')', true)
		if err != nil {
			return nil, err
		}
		p.pos++
		lk := &Look{Kid: kid, Neg: neg, Behind: false, SrcLo: lo, SrcHi: p.pos}
		return p.parseQuantifier(lk)
	case p.src[p.pos] == '<' && p.pos+1 < len(p.src) && p.src[p.pos+1] != 'P' && (p.src[p.pos+1] == '=' || p.src[p.pos+1] == '!'):
		neg := p.src[p.pos+1] == '!'
		p.pos += 2
		kid, _, err := p.parseAlternation(')', true)
		if err != nil {
			return nil, err
		}
		p.pos++
		lk := &Look{Kid: kid, Neg: neg, Behind: true, SrcLo: lo, SrcHi: p.pos}
		return p.parseQuantifier(lk)
	case p.src[p.pos] == '<' || (p.src[p.pos] == 'P' && p.pos+1 < len(p.src) && p.src[p.pos+1] == '<'):
		// (?<name>...) or (?P<name>...)
		start := p.pos + 1 // one byte after '<'
		pStyle := false
		if p.src[p.pos] == 'P' {
			pStyle = true
			start = p.pos + 2 // after "<P"
		}
		{
			// named capture
			if start < len(p.src) && (pStyle || isNameStart(p.src[start])) {
				nameStart := start
				i := nameStart
				for i < len(p.src) && isNameByte(p.src[i]) {
					i++
				}
				if i >= len(p.src) || p.src[i] != '>' {
					return nil, p.errorf("malformed named capture")
				}
				name := p.src[nameStart:i]
				p.pos = i + 1
				if existing, ok := p.names[name]; ok {
					return nil, p.errorf("duplicate capture name %q (already group %d)", name, existing)
				}
				p.caps++
				id := p.caps
				p.names[name] = id
				p.capName[id] = name
				kid, _, err := p.parseAlternation(')', true)
				if err != nil {
					return nil, err
				}
				p.pos++
				g := &Group{Kid: kid, CapID: id, Name: name, SrcLo: lo, SrcHi: p.pos}
				return p.parseQuantifier(g)
			}
		}
		return nil, p.errorf("unsupported group syntax after (?")
	case isFlagByte(p.src[p.pos]) || p.src[p.pos] == '-':
		return p.parseFlagGroup(lo)
	default:
		return nil, p.errorf("unsupported group syntax after (?")
	}
}

func isNameStart(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isNameByte(b byte) bool {
	return isNameStart(b) || (b >= '0' && b <= '9')
}

func isFlagByte(b byte) bool {
	return b == 'i' || b == 'm' || b == 's' || b == 'U' || b == 'x'
}

// parseFlagGroup parses (?imsUx), (?ims-ims) and (?imsUx:...).
func (p *parser) parseFlagGroup(lo int) (Node, error) {
	var set, clear uint8
	for p.pos < len(p.src) && isFlagByte(p.src[p.pos]) {
		set |= flagBit(p.src[p.pos])
		p.pos++
	}
	if p.pos < len(p.src) && p.src[p.pos] == '-' {
		p.pos++
		for p.pos < len(p.src) && isFlagByte(p.src[p.pos]) {
			clear |= flagBit(p.src[p.pos])
			p.pos++
		}
	}
	if set&clear != 0 {
		return nil, p.errorf("flag both set and cleared")
	}
	var kid Node
	if p.pos < len(p.src) && p.src[p.pos] == ':' {
		p.pos++
		var err error
		kid, _, err = p.parseAlternation(')', true)
		if err != nil {
			return nil, err
		}
	}
	if p.pos >= len(p.src) || p.src[p.pos] != ')' {
		return nil, p.errorf("malformed flag group")
	}
	p.pos++
	fn := &Flags{Set: set, Clear: clear, Kid: kid, SrcLo: lo, SrcHi: p.pos}
	if kid == nil {
		// bare flag change affects the rest of the enclosing sequence.
		p.flags = (p.flags | set) &^ clear
		return fn, nil
	}
	return p.parseQuantifier(fn)
}

func flagBit(b byte) uint8 {
	switch b {
	case 'i':
		return FlagI
	case 'm':
		return FlagM
	case 's':
		return FlagS
	case 'U':
		return FlagU
	case 'x':
		return FlagX
	}
	return 0
}

var _ = utf8.RuneSelf
