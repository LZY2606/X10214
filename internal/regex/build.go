package regex

// Build parses pattern, optionally optimizes the AST, and compiles it.
// Every returned Program carries the pinned syntax and Unicode versions.
func Build(pattern string, optimize bool) (*Program, error) {
	ast, err := Parse(pattern)
	if err != nil {
		return nil, err
	}
	if optimize {
		ast = Optimize(ast)
	}
	prog, err := Compile(ast, pattern)
	if err != nil {
		return nil, err
	}
	prog.Optimized = optimize
	return prog, nil
}

// PositionMap exposes both UTF-8 byte and Unicode code point coordinates for
// every rune boundary in the input.
type PositionMap struct {
	ByteToRune []int  `json:"byte_to_rune"`
	RuneToByte []int  `json:"rune_to_byte"`
	Runes      []rune `json:"runes"`
	Bytes      int    `json:"bytes"`
	HasInvalid bool   `json:"has_invalid"`
}

// MapPositions builds the dual-coordinate index for input.
func MapPositions(input []byte) PositionMap {
	pm := PositionMap{ByteToRune: buildRuneIndex(input), Bytes: len(input)}
	for b := 0; b < len(input); {
		r, w, invalid := decodeAt(input, b)
		pm.RuneToByte = append(pm.RuneToByte, b)
		pm.Runes = append(pm.Runes, r)
		if invalid {
			pm.HasInvalid = true
		}
		b += w
	}
	pm.RuneToByte = append(pm.RuneToByte, len(input))
	pm.ByteToRune = append(pm.ByteToRune, len(pm.Runes))
	return pm
}

// ASTView is the JSON-serializable AST payload for the page.
type ASTView struct {
	Pattern        string         `json:"pattern"`
	SyntaxVersion  string         `json:"syntax_version"`
	UnicodeVersion string         `json:"unicode_version"`
	Optimized      bool           `json:"optimized"`
	Root           *Node          `json:"root"`
	Labels         map[int]string `json:"labels"`
}

// View renders the AST plus per-node labels.
func (p *Program) View() ASTView {
	labels := map[int]string{}
	walk(p.AST, func(n *Node) { labels[n.ID] = describeNode(n) })
	return ASTView{Pattern: p.Pattern, SyntaxVersion: SyntaxVersion,
		UnicodeVersion: UnicodeDataVersion, Optimized: p.Optimized, Root: p.AST, Labels: labels}
}
