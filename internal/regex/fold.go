package regex

import "sort"

// foldNext is a deterministic cyclic traversal of each simple-fold
// equivalence class, ordered by code point ascending and wrapping from
// the largest member back to the smallest. Built once from the frozen
// UCD 15.1 CaseFolding table (statuses C and S).
var foldNext = buildFoldCycle()

func buildFoldCycle() map[rune]rune {
	parent := map[rune]rune{}
	find := func(x rune) rune {
		for {
			p, ok := parent[x]
			if !ok || p == x {
				return x
			}
			x = p
		}
	}
	union := func(a, b rune) {
		ra, rb := find(a), find(b)
		if ra == rb {
			return
		}
		if rb < ra {
			ra, rb = rb, ra
		}
		parent[rb] = ra
	}
	for from, to := range uniSimpleFold {
		parent[from] = find0(parent, from)
		union(from, to)
	}
	groups := map[rune][]rune{}
	for r := range parent {
		root := find(r)
		groups[root] = append(groups[root], r)
	}
	next := map[rune]rune{}
	for _, g := range groups {
		sort.Slice(g, func(i, j int) bool { return g[i] < g[j] })
		for i := range g {
			next[g[i]] = g[(i+1)%len(g)]
		}
	}
	return next
}

func find0(m map[rune]rune, x rune) rune {
	if p, ok := m[x]; ok {
		return p
	}
	m[x] = x
	return x
}
