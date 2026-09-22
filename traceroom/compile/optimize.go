package compile

// optimizeProgram applies semantics-preserving rewrites:
//
//  1. Jump threading: Jmp->Jmp, Split secondary->Jmp resolve to final targets.
//  2. Dead code elimination: code unreachable from entry (pc 0) is removed,
//     and pcs are remapped. OpSplit primary always remains fall-through so
//     textual branch priority is preserved exactly.
//
// These rewrites never reorder alternatives, never change capture order and
// never change which input strings match. The engine test suite compares
// optimized vs unoptimized trajectories as a guard.
func optimizeProgram(p *Program) {
	threadJumps(p)
	reachableCompact(p)
	for _, sub := range p.Subs {
		optimizeProgram(sub)
	}
}

func threadJumps(p *Program) {
	resolve := func(target int) int {
		seen := map[int]bool{}
		for target < len(p.Code) && p.Code[target].Op == OpJmp && !seen[target] {
			seen[target] = true
			target = p.Code[target].Arg
		}
		return target
	}
	for i := range p.Code {
		switch p.Code[i].Op {
		case OpJmp:
			p.Code[i].Arg = resolve(p.Code[i].Arg)
		case OpSplit:
			// only secondary (Arg) can safely be threaded; primary
			// fall-through semantics stay intact.
			p.Code[i].Arg = resolve(p.Code[i].Arg)
		}
	}
}

func reachableCompact(p *Program) {
	n := len(p.Code)
	live := make([]bool, n)
	stack := []int{0}
	for len(stack) > 0 {
		pc := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if pc < 0 || pc >= n || live[pc] {
			continue
		}
		live[pc] = true
		ins := p.Code[pc]
		switch ins.Op {
		case OpJmp:
			stack = append(stack, ins.Arg)
		case OpSplit:
			stack = append(stack, pc+1) // primary fall through
			stack = append(stack, ins.Arg)
		case OpMatch, OpSubEnd:
			// no successors
		default:
			stack = append(stack, pc+1)
		}
	}
	// Build remap; unreachable pcs map to -1.
	remap := make([]int, n)
	newCode := make([]Instr, 0, n)
	for i, ok := range live {
		if ok {
			remap[i] = len(newCode)
			newCode = append(newCode, p.Code[i])
		} else {
			remap[i] = -1
		}
	}
	for i := range newCode {
		ins := &newCode[i]
		switch ins.Op {
		case OpJmp:
			ins.Arg = remap[ins.Arg]
		case OpSplit:
			ins.Arg = remap[ins.Arg]
		}
	}
	p.Code = newCode
}

// Disassemble renders code for tests/debug.
func (p *Program) Disassemble() []string {
	names := map[Op]string{
		OpChar: "Char", OpClass: "Class", OpAny: "Any", OpMatch: "Match",
		OpJmp: "Jmp", OpSplit: "Split", OpSave: "Save", OpLoopStart: "LoopStart",
		OpLoopGuard: "LoopGuard", OpLook: "Look", OpOpenCut: "OpenCut",
		OpCloseCut: "CloseCut", OpAnchor: "Anchor", OpBackref: "Backref",
		OpSubEnd: "SubEnd",
	}
	out := make([]string, len(p.Code))
	for i, in := range p.Code {
		s := names[in.Op]
		if in.Arg != 0 || in.Op == OpJmp || in.Op == OpSplit {
			s += " " + itoa(in.Arg)
		}
		if in.Arg2 != 0 {
			s += "/" + itoa(in.Arg2)
		}
		out[i] = s
	}
	return out
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}
