package unicodex

// buildFoldGroups groups code points by their simple fold target. The
// representative key is itself included so case-insensitive classes can list
// every member of the fold class.
func buildFoldGroups(fold map[rune]rune) map[rune][]rune {
	groups := map[rune]map[rune]struct{}{}
	for cp := range fold {
		key := fold[cp]
		g, ok := groups[key]
		if !ok {
			g = map[rune]struct{}{key: {}}
			groups[key] = g
		}
		g[cp] = struct{}{}
	}
	out := make(map[rune][]rune, len(groups))
	for key, g := range groups {
		rs := make([]rune, 0, len(g))
		for r := range g {
			rs = append(rs, r)
		}
		sortRunes(rs)
		out[key] = rs
	}
	return out
}

func sortRunes(rs []rune) {
	for i := 1; i < len(rs); i++ {
		for j := i; j > 0 && rs[j-1] > rs[j]; j-- {
			rs[j-1], rs[j] = rs[j], rs[j-1]
		}
	}
}
