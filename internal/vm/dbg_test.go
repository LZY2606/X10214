package vm

import (
	"fmt"
	"testing"
)

func TestDbg(t *testing.T) {
	_, prog := MustProg(`a*`)
	for i, in := range prog.Code {
		fmt.Printf("pc=%d %s target=%d a=%d b=%d c=%d greedy=%v\n", i, in.OpName(), in.Target, in.A, in.B, in.C, in.Greedy)
	}
	run, _ := MatchPattern(`a*`, "", DefaultBudgets())
	fmt.Println("===", run.Status)
	for _, e := range run.Events {
		fmt.Printf("%d %s pc=%d pos=%d %s\n", e.Seq, e.Kind, e.Pc, e.Pos, e.Detail)
	}
}
