package main

import (
	"fmt"
	"capturetrail/internal/rx"
)

func main() {
	pat := `(?<=ab|a)x`
	_, prog, _ := rx.ParseAndCompile(pat)
	m := rx.NewMachine(prog, "abx", rx.Config{Mode: rx.ModeSearch, MaxBehind: 256, TrailLimit: 500}, rx.Budget{MaxSteps: 10000, MaxStates: 10000, MaxSnapshots: 10000})
	ex := rx.NewExecutor(prog, m)
	for ex.Step() { }
	fmt.Println("status", m.Status, m.MatchStart, m.MatchEnd)
	for _, ev := range m.Events {
		fmt.Printf("%d %s pc=%d pos=%d det=%s\n", ev.Seq, ev.Kind, ev.PC, ev.Pos, ev.Detail)
	}
}
