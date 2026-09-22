package syntax

// parseFlagGroupStart is called with p.i pointing at '?' immediately after
// '(' has been consumed. It accepts (?:, (?i:, (?i-s: and friends.
func (p *parser) parseFlagGroupStart() (set, clear Flags, mask Mask, scoped bool, ok bool) {
	j := p.i + 1
	on := true
	sawFlag := false
	for j < len(p.src) {
		switch p.src[j] {
		case 'i':
			mask.I, sawFlag = true, true
			if on {
				set.I = true
			} else {
				clear.I = true
			}
		case 's':
			mask.S, sawFlag = true, true
			if on {
				set.S = true
			} else {
				clear.S = true
			}
		case 'm':
			mask.M, sawFlag = true, true
			if on {
				set.M = true
			} else {
				clear.M = true
			}
		case '-':
			if !on {
				p.fail(j, "repeated '-' in flag group")
				return Flags{}, Flags{}, Mask{}, false, false
			}
			on = false
		case ':':
			p.i = j + 1
			return set, clear, mask, true, true
		case ')':
			// (?:...) with empty flags is the plain non-capturing group;
			// bare (?i) is rejected by the caller.
			if sawFlag {
				p.fail(j, "bare flag groups are not supported, use (?flags:...)")
				return Flags{}, Flags{}, Mask{}, false, false
			}
			p.i = j + 1
			return Flags{}, Flags{}, Mask{}, false, true
		default:
			p.fail(j, "unsupported flag letter")
			return Flags{}, Flags{}, Mask{}, false, false
		}
		j++
	}
	p.fail(j, "unterminated flag group")
	return Flags{}, Flags{}, Mask{}, false, false
}
