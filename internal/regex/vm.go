package regex

const (
	StatusMatch         = "match"
	StatusNoMatch       = "no_match"
	StatusIndeterminate = "indeterminate"
)

type Limits struct {
	Steps     int `json:"steps"`
	States    int `json:"states"`
	Snapshots int `json:"snapshots"`
	Events    int `json:"events"`
}

type Usage struct {
	Steps      int `json:"steps"`
	StatesPeak int `json:"states_peak"`
	Snapshots  int `json:"snapshots"`
}

type Capture struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type Call struct {
	Kind         int  `json:"kind"`
	Negative     bool `json:"negative"`
	OuterPos     int  `json:"outer_pos"`
	AfterPC      int  `json:"after_pc"`
	MarkerChoice int  `json:"marker_choice"`
	Width        int  `json:"width"`
}

type LoopFrame struct {
	StartPC   int `json:"start_pc"`
	Count     int `json:"count"`
	BodyStart int `json:"body_start"`
	LastEnd   int `json:"last_end"`
}

const (
	choiceAlt = iota + 1
	choiceLoop
	choiceAssert
)

type Choice struct {
	Kind      int         `json:"kind"`
	PC        int         `json:"pc"`
	Pos       int         `json:"pos"`
	Caps      []Capture   `json:"caps"`
	Calls     []Call      `json:"calls"`
	Loops     []LoopFrame `json:"loops"`
	StartPC   int         `json:"start_pc,omitempty"`
	EndPC     int         `json:"end_pc,omitempty"`
	Count     int         `json:"count,omitempty"`
	BodyStart int         `json:"body_start,omitempty"`
	Negative  bool        `json:"negative,omitempty"`
	AfterPC   int         `json:"after_pc,omitempty"`
	NodeID    int         `json:"node_id,omitempty"`
}

type State struct {
	PC          int         `json:"pc"`
	Pos         int         `json:"pos"`
	SearchStart int         `json:"search_start"`
	Caps        []Capture   `json:"caps"`
	Calls       []Call      `json:"calls"`
	Loops       []LoopFrame `json:"loops"`
	Choices     []Choice    `json:"choices"`
}

type TraceEvent struct {
	Step      int       `json:"step"`
	Type      string    `json:"type"`
	NodeID    int       `json:"node_id,omitempty"`
	PC        int       `json:"pc,omitempty"`
	Pos       int       `json:"pos,omitempty"`
	CodePoint int       `json:"code_point,omitempty"`
	Detail    string    `json:"detail,omitempty"`
	Group     int       `json:"group,omitempty"`
	Caps      []Capture `json:"caps,omitempty"`
	Pruned    *Choice   `json:"pruned,omitempty"`
	Usage     Usage     `json:"usage"`
}

type Result struct {
	Status         string       `json:"status"`
	Pattern        string       `json:"pattern"`
	Input          string       `json:"input"`
	SyntaxVersion  string       `json:"syntax_version"`
	UnicodeVersion string       `json:"unicode_version"`
	Match          *Capture     `json:"match,omitempty"`
	Groups         []Group      `json:"groups"`
	Events         []TraceEvent `json:"events"`
	Frontier       *State       `json:"frontier,omitempty"`
	Usage          Usage        `json:"usage"`
	Reason         string       `json:"reason,omitempty"`
	InputMap       Input        `json:"input_map"`
}

type VM struct {
	program *Program
	input   Input
	groups  []Group
	limits  Limits
	used    Usage
	state   State
	events  []TraceEvent
	reason  string
}

func NewVM(prog *Program, in Input, groups []Group, limits Limits) *VM {
	return &VM{program: prog, input: in, groups: groups, limits: normalizeLimits(limits)}
}

func normalizeLimits(l Limits) Limits {
	if l.Steps <= 0 {
		l.Steps = 50000
	}
	if l.States <= 0 {
		l.States = 10000
	}
	if l.Snapshots <= 0 {
		l.Snapshots = 50000
	}
	if l.Events <= 0 {
		l.Events = 2000
	}
	return l
}

func RunPattern(pattern, text string, limits Limits, frontier *State) (*Result, error) {
	root, err := Parse(pattern)
	if err != nil {
		return nil, err
	}
	prog, err := Compile(root)
	if err != nil {
		return nil, err
	}
	in := NewInput(text)
	vm := NewVM(prog, in, root.Groups, limits)
	if frontier != nil {
		vm.state = cloneState(frontier)
	} else {
		vm.resetSearch(0)
	}
	vm.run()
	return vm.result(), nil
}

func (vm *VM) result() *Result {
	r := &Result{Status: vm.status(), Pattern: "", Input: vm.input.Text, SyntaxVersion: SyntaxVersion, UnicodeVersion: UnicodeDataVersion, Groups: vm.groups, Events: vm.events, Frontier: &vm.state, Usage: vm.used, Reason: vm.reason, InputMap: vm.input}
	if r.Status == StatusMatch {
		m := vm.state.Caps[0]
		r.Match = &m
	}
	if r.Status != StatusIndeterminate {
		r.Frontier = nil
	}
	return r
}

func (vm *VM) status() string {
	if vm.reason != "" {
		return StatusIndeterminate
	}
	if vm.state.PC >= 0 && vm.state.PC < len(vm.program.Ops) && vm.program.Ops[vm.state.PC].Kind == OpMatch {
		return StatusMatch
	}
	return StatusNoMatch
}
