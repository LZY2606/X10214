package vm

import "capturetrace/internal/syntax"

// ResumeOptions controls continuation from a persisted frontier.
type ResumeOptions struct {
	ExtraSteps     int
	ExtraStates    int
	ExtraSnapshots int
	StepCap        int
	Breakpoints    []int
}

// Resume rehydrates a machine from a frontier and continues.
func Resume(pattern string, root *syntax.Node, input *Input, mode Mode, f *Frontier, opts ResumeOptions) (*Machine, Status) {
	m := &Machine{
		Pattern:      pattern,
		Root:         root,
		Input:        input,
		Mode:         mode,
		breakpoints:  map[int]bool{},
		stopAt:       -1,
		status:       StatusRunning,
		keepEvents:   5000,
		backrefIndex: map[int]int{},
	}
	use := f.Use
	if opts.ExtraSteps > 0 {
		use.MaxSteps = use.Steps + opts.ExtraSteps
	}
	if opts.ExtraStates > 0 {
		use.MaxStates = use.States + opts.ExtraStates
	}
	if opts.ExtraSnapshots > 0 {
		use.MaxSnaps = use.Snapshots + opts.ExtraSnapshots
	}
	m.use = use
	m.snaps = newSnapStore(len(syntax.Groups(root)), use.MaxSnaps)
	m.snaps.count = use.Snapshots
	m.snaps.nextID = use.Snapshots + 1

	m.levels = make([]*Level, len(f.Levels))
	for i := range f.Levels {
		lv := f.Levels[i]
		m.levels[i] = &lv
	}
	for _, id := range opts.Breakpoints {
		m.breakpoints[id] = true
	}
	m.emit(EvStart, 0, "resumed from frontier")
	st := m.Run(opts.StepCap)
	return m, st
}
