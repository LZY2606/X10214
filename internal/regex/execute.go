package regex

// Options controls one execution.
type Options struct {
	Budget   Budget `json:"budget"`
	Optimize bool   `json:"optimize"`
	Anchored bool   `json:"anchored"` // full-match semantics (semantic diff)
}

// Capture is one capture group in the final result.
type Capture struct {
	Index     int    `json:"index"`
	Name      string `json:"name,omitempty"`
	Set       bool   `json:"set"`
	Start     int    `json:"start"`
	End       int    `json:"end"`
	StartByte int    `json:"startByte"`
	EndByte   int    `json:"endByte"`
	Text      string `json:"text"`
}

// Result is the pinned, serializable outcome of one execution.
type Result struct {
	Status         Status       `json:"status"`
	Reason         string       `json:"reason,omitempty"`
	Pattern        string       `json:"pattern"`
	Input          string       `json:"input"`
	Start          int          `json:"start"`
	End            int          `json:"end"`
	StartByte      int          `json:"startByte"`
	EndByte        int          `json:"endByte"`
	Captures       []Capture    `json:"captures"`
	Trace          []TraceEvent `json:"trace"`
	TraceTruncated bool         `json:"traceTruncated"`
	Frontier       *Frontier    `json:"frontier,omitempty"`
	Pruned         []Thread     `json:"pruned,omitempty"`
	Stats          Stats        `json:"stats"`
	SyntaxVersion  string       `json:"syntaxVersion"`
	UnicodeVersion string       `json:"unicodeVersion"`
	AST            string       `json:"ast"`
	Program        []string     `json:"program"`
}

// Execute runs pattern over input. When frontier is non-nil the run
// resumes from it with opt.Budget as the new cumulative budget.
func Execute(pattern, input string, opt Options, frontier *Frontier) (*Result, error) {
	root, info, err := Parse(pattern)
	if err != nil {
		return nil, err
	}
	prog := Compile(root, info, opt.Optimize)
	vm := NewVM(prog, input, opt.Budget, opt.Anchored)
	if frontier != nil {
		vm.ResumeFrom(frontier, opt.Budget)
	}
	vm.Run()
	return ResultFromVM(pattern, input, root, prog, vm), nil
}

// ResultFromVM builds the pinned result view of a (possibly still
// running) VM. Used by the stepping API.
func ResultFromVM(pattern, input string, root Node, prog *Program, vm *VM) *Result {
	runes, offs := vm.Input()
	res := &Result{
		Status:         vm.StatusOf(),
		Reason:         vm.Reason(),
		Pattern:        pattern,
		Input:          input,
		Start:          -1,
		End:            -1,
		StartByte:      -1,
		EndByte:        -1,
		Trace:          vm.Events(),
		TraceTruncated: vm.TraceTruncated(),
		Frontier:       vm.Frontier(),
		Pruned:         vm.Pruned(),
		Stats:          vm.Stats(),
		SyntaxVersion:  SyntaxVersion,
		UnicodeVersion: UnicodeVersion,
		AST:            root.String(),
		Program:        prog.disassemble(),
	}
	if m := vm.Matched(); m != nil {
		res.Start, res.End = m.Saved[0], m.Saved[1]
		res.StartByte, res.EndByte = offs[res.Start], offs[res.End]
		names := map[int]string{}
		for name, idx := range prog.GroupNames {
			names[idx] = name
		}
		for i := 0; i <= prog.NumGroups; i++ {
			s, e := m.Saved[2*i], m.Saved[2*i+1]
			c := Capture{Index: i, Name: names[i], Start: -1, End: -1, StartByte: -1, EndByte: -1}
			if s >= 0 && e >= 0 {
				c.Set = true
				c.Start, c.End = s, e
				c.StartByte, c.EndByte = offs[s], offs[e]
				c.Text = string(runes[s:e])
			}
			res.Captures = append(res.Captures, c)
		}
	}
	return res
}
