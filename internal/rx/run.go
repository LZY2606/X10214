package rx

// Result is the public outcome of a run.
type Result struct {
	Status    Status       `json:"status"`
	MatchText string       `json:"match_text,omitempty"`
	Start     int          `json:"start"`
	End       int          `json:"end"`
	StartByte int          `json:"start_byte"`
	EndByte   int          `json:"end_byte"`
	Caps      []GroupMatch `json:"caps"`
	Machine   *Machine     `json:"machine,omitempty"`
	Frontier  []byte       `json:"-"`
	Reason    string       `json:"reason,omitempty"`
	Grammar   string       `json:"grammar"`
	Unicode   string       `json:"unicode"`
}

type GroupMatch struct {
	Index int    `json:"index"`
	Name  string `json:"name,omitempty"`
	Text  string `json:"text,omitempty"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	Set   bool   `json:"set"`
}

// Options configures Run.
type Options struct {
	Mode        MatchMode
	Budget      Budget
	Breakpoints []int
	MaxBehind   int
	TrailLimit  int
	KeepMachine bool
}

// Run parses and executes pattern against input in one shot.
func Run(pattern, input string, opts Options) (*Result, error) {
	pat, err := Parse(pattern)
	if err != nil {
		return nil, err
	}
	prog := Compile(pat)
	cfg := Config{Mode: opts.Mode, Breakpoints: opts.Breakpoints, MaxBehind: opts.MaxBehind, TrailLimit: opts.TrailLimit}
	m := NewMachine(prog, input, cfg, opts.Budget)
	ex := NewExecutor(prog, m)
	ex.RunUntil(0)
	res := buildResult(prog, ex, opts.KeepMachine)
	return res, nil
}

func buildResult(prog *Program, ex *Executor, keep bool) *Result {
	m := ex.M
	r := &Result{Status: m.Status, Reason: m.StopReason, Grammar: SyntaxVersion, Unicode: UnicodeVersion}
	if m.Status == StatusMatch {
		r.Start, r.End = m.MatchStart, m.MatchEnd
		r.StartByte, r.EndByte = ex.Idx.BytePos(m.MatchStart), ex.Idx.BytePos(m.MatchEnd)
		r.MatchText = ex.Idx.Slice(m.MatchStart, m.MatchEnd)
		r.Caps = make([]GroupMatch, len(m.Caps))
		for i := range m.Caps {
			gm := GroupMatch{Index: i, Start: -1, End: -1}
			if i < len(prog.Groups) {
				gm.Name = prog.Groups[i].Name
			}

			if m.Caps[i].Set {
				gm.Set = true
				gm.Start, gm.End = m.Caps[i].Start, m.Caps[i].End
				gm.Text = ex.Idx.Slice(m.Caps[i].Start, m.Caps[i].End)
			}
			r.Caps[i] = gm
		}
	}
	if m.Status == StatusIndeterminate {
		b, _ := m.Marshal()
		r.Frontier = b
	}
	if keep {
		r.Machine = m
	}
	return r
}

// ResumeMachine continues a previously paused run.
func ResumeMachine(prog *Program, m *Machine, extra Budget) *Result {
	ex := NewExecutor(prog, m)
	ex.Resume(extra)
	return buildResult(prog, ex, true)
}

// ParseAndCompile is a convenience used by the server layer.
func ParseAndCompile(pattern string) (*Pattern, *Program, error) {
	pat, err := Parse(pattern)
	if err != nil {
		return nil, nil, err
	}
	return pat, Compile(pat), nil
}
