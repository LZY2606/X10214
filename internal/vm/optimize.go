package vm

// optimize performs semantics-preserving peephole cleanup:
//
//  1. jump threading: every target that points at an unconditional OpJmp is
//     rewritten to that jump's destination (iterated to a fixed point);
//  2. unreachable instruction removal: code that no instruction can transfer
//     control to is deleted.
//
// No instruction reordering is performed: equal-priority branches keep their
// documented (source) order, and all Split/Guard target priority is preserved.
func (c *compiler) optimize() string {
	code := c.prog.Code
	n := len(code)

	// resolve(x) follows OpJmp chains.
	resolve := func(x int) int {
		seen := map[int]bool{}
		for code[x].Op == OpJmp && !seen[x] {
			seen[x] = true
			x = code[x].Targets[0]
		}
		return x
	}
	for {
		changed := false
		for i := range code {
			for t := range code[i].Targets {
				rt := resolve(code[i].Targets[t])
				if rt != code[i].Targets[t] {
					code[i].Targets[t] = rt
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}

	reachable := make([]bool, n)
	reachable[0] = true
	var walk func(int)
	walk = func(pc int) {
		if pc < 0 || pc >= n || !reachable[pc] {
			return
		}
		in := code[pc]
		switch in.Op {
		case OpMatch, OpFail:
			return
		case OpJmp:
			if !reachable[in.Targets[0]] {
				reachable[in.Targets[0]] = true
				walk(in.Targets[0])
			}
		default:
			if pc+1 < n && !reachable[pc+1] {
				reachable[pc+1] = true
				walk(pc + 1)
			}
			for _, t := range in.Targets {
				if !reachable[t] {
					reachable[t] = true
					walk(t)
				}
			}
		}
	}
	walk(0)

	kept := 0
	old2new := make([]int, n)
	for i := range old2new {
		if reachable[i] {
			old2new[i] = kept
			code[kept] = code[i]
			kept++
		}
	}
	removed := n - kept
	code = code[:kept]
	for i := range code {
		for t := range code[i].Targets {
			code[i].Targets[t] = old2new[code[i].Targets[t]]
		}
	}
	c.prog.Code = code
	return "jump-threading + unreachable-code removal"
}
