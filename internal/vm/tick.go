package vm

import (
	"fmt"
	"unicode"
)

// tick performs exactly one instruction-level micro-step.
// It returns false when the run reaches a terminal/paused state.
func (r *Run) tick() bool {
	if r.finished() {
		return false
	}
	r.steps++
	if r.steps > r.Budgets.MaxSteps {
		r.indeterminate("step", "instruction count")
		return false
	}
	t := r.Thread
	ins := r.Prog.Code[t.Pc]
	if r.Breakpoints[ins.NID] && !r.stoppedAtBP {
		// Halt before executing this node; Step() will resume by clearing.
		r.stoppedAtBP = true
		return r.stopReason(true)
	}
	r.stoppedAtBP = false

	switch ins.Op {
	case OpChar:
		c, ok := r.Input.at(t.Pos)
		r.logStep(ins, fmt.Sprintf("char %q at %d (found %q)", ins.Rune, t.Pos, c))
		if !ok || !runeEq(c, ins.Rune, ins.Fold) {
			return r.failThread("char mismatch")
		}
		t.Pos++
		t.Pc++
	case OpClass:
		c, ok := r.Input.at(t.Pos)
		matched := ok && ins.Class.Match(c)
		r.logStep(ins, fmt.Sprintf("class at %d -> %v", t.Pos, matched))
		if !matched {
			return r.failThread("class mismatch")
		}
		t.Pos++
		t.Pc++
	case OpAny:
		c, ok := r.Input.at(t.Pos)
		matched := ok && (ins.DotAll || c != '\n')
		r.logStep(ins, fmt.Sprintf("any at %d -> %v", t.Pos, matched))
		if !matched {
			return r.failThread("dot mismatch")
		}
		t.Pos++
		t.Pc++
	case OpAnchor:
		ok := r.anchorOK(ins.A, t.Pos, ins.Multiline)
		r.logStep(ins, fmt.Sprintf("anchor %d at %d -> %v", ins.A, t.Pos, ok))
		if !ok {
			return r.failThread("anchor mismatch")
		}
		t.Pc++
	case OpJmp:
		r.logStep(ins, fmt.Sprintf("jump to %d", ins.Target))
		t.Pc = ins.Target
	case OpSplit:
		r.doSplit(ins)
	case OpGroupBegin:
		r.doGroupBegin(ins)
	case OpGroupEnd:
		r.doGroupEnd(ins)
	case OpBackref:
		if !r.doBackref(ins) {
			return r.failThread("backreference mismatch")
		}
	case OpRepeatBegin:
		r.doRepeatBegin(ins)
	case OpRepeatPost:
		r.doRepeatPost(ins)
	case OpRepeatAfter:
		if !r.doRepeatAfter(ins) {
			return r.failThread(fmt.Sprintf("repeat needs at least %d", ins.B))
		}
	case OpLookPush:
		r.doLookPush(ins)
	case OpLookEnd:
		r.doLookEnd(ins)
	case OpMatch:
		r.doMatch(ins)
		return false
	}
	return !r.finished()
}

func (r *Run) logStep(ins Instr, detail string) {
	t := r.Thread
	r.log(Event{Kind: EvStep, Pc: t.Pc, NID: ins.NID, Op: ins.OpName(), Pos: t.Pos, Detail: detail})
}

func runeEq(a, b rune, fold bool) bool {
	if a == b {
		return true
	}
	if !fold {
		return false
	}
	for f := unicode.SimpleFold(a); f != a; f = unicode.SimpleFold(f) {
		if f == b {
			return true
		}
	}
	return false
}

func (r *Run) anchorOK(kind, pos int, multiline bool) bool {
	in := r.Input
	atStart := pos == 0
	atEnd := pos == in.lenRunes()
	switch kind {
	case AnchorBOL:
		if atStart {
			return true
		}
		return multiline && pos > 0 && (in.Runes[pos-1] == '\n' || in.Runes[pos-1] == '\r')
	case AnchorEOL:
		if atEnd {
			return true
		}
		if multiline && in.Runes[pos] == '\n' {
			return true
		}
		if multiline && in.Runes[pos] == '\r' {
			return true
		}
		return atEnd
	case AnchorWB:
		return r.wordAt(pos) != r.wordAt(pos-1)
	case AnchorNWB:
		return r.wordAt(pos) == r.wordAt(pos-1)
	}
	return false
}

func (r *Run) wordAt(pos int) bool {
	if pos < 0 || pos >= r.Input.lenRunes() {
		return false
	}
	return isWord(r.Input.Runes[pos])
}
