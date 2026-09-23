package regex

func cloneCaps(caps []Capture) []Capture {
	if caps == nil {
		return nil
	}
	out := make([]Capture, len(caps))
	copy(out, caps)
	return out
}

func cloneCalls(calls []Call) []Call {
	if calls == nil {
		return nil
	}
	out := make([]Call, len(calls))
	copy(out, calls)
	return out
}

func cloneLoops(loops []LoopFrame) []LoopFrame {
	if loops == nil {
		return nil
	}
	out := make([]LoopFrame, len(loops))
	copy(out, loops)
	return out
}

func cloneChoices(choices []Choice) []Choice {
	if choices == nil {
		return nil
	}
	out := make([]Choice, len(choices))
	copy(out, choices)
	for i := range out {
		out[i].Caps = cloneCaps(choices[i].Caps)
		out[i].Calls = cloneCalls(choices[i].Calls)
		out[i].Loops = cloneLoops(choices[i].Loops)
	}
	return out
}

func cloneState(s *State) State {
	return State{PC: s.PC, Pos: s.Pos, SearchStart: s.SearchStart, Caps: cloneCaps(s.Caps), Calls: cloneCalls(s.Calls), Loops: cloneLoops(s.Loops), Choices: cloneChoices(s.Choices)}
}
