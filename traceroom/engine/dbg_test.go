package engine

import (
	"fmt"
	"testing"
)

func TestDbgLook(t *testing.T) {
	prog := mustProg(t, `(?<=abc)x`)
	r := NewVM(prog, "abcx", Config{}).Run()
	fmt.Println("sub disasm:")
	for id, sub := range prog.Subs { fmt.Println("sub", id, sub.Disassemble()) }
	fmt.Println("main:", prog.Disassemble())

	fmt.Println("status", r.Status, "start", r.Start, "end", r.End)
	for _, e := range r.Events {
		fmt.Printf("%3d %-14s pos=%d pc=%d %v\n", e.Seq, e.Kind, e.Pos.Byte, e.PC, e.Detail)
	}
}
