package main
import (
	"fmt"
	"capturetrace/internal/syntax"
	"capturetrace/internal/vm"
)
func run(p, s string, b ...vm.Budget) {
	root, err := syntax.Parse(p)
	if err != nil { fmt.Printf("%-22s PARSE ERR %v\n", p, err); return }
	if err := syntax.Validate(root); err != nil { fmt.Printf("%-22s VAL ERR %v\n", p, err); return }
	in := vm.NewInput(s)
	var opts []vm.Option
	if len(b) > 0 { opts = append(opts, vm.WithBudget(b[0])) }
	m := vm.New(p, root, in, vm.ModeSearch, opts...)
	m.Run(0)
	r := m.Result()
	fmt.Printf("%-22q x %q => %s", p, s, r.Status)
	if r.Match {
		fmt.Printf(" %q g=[", r.MatchedText)
		for i, g := range r.Groups {
			if g.Set { fmt.Sprintf(""); fmt.Printf(" %d:%q", i+1, s[g.Start:g.End]) } else { fmt.Printf(" %d:-", i+1) }
		}
		fmt.Print(" ]")
	}
	if r.Frontier != nil { fmt.Printf(" frontier=%s steps=%d", r.Frontier.Reason, r.Use.Steps) }
	fmt.Println()
}
func main() {
	run(`(a)|(b)`, "b")
	run(`(a)?(a)`, "a")
	run(`(?:(a)|(b))+`, "ab")
	run(`((a)|(b))+`, "ab")
	run(`(\w)(\w)`, "é̗x")
	run(`.`, "é̗")
	run(`.`, string([]byte{0xff, 'a'}))
	run(`\xff`, string([]byte{0xff}))
	run(`(a*)+b`, "aaab")
	run(`(a+)+b`, "aaaab")
	run(`^(a+)+$`, "aaaaaaaaaaaaaaaaaaaa!")
	run(`(a+)+b`, "aaaaaaaaaaaaaaaaaaa!", vm.Budget{MaxSteps: 3000, MaxStates: 0, MaxSnapshots: 0})
	run(`a?a?a?b`, "aaab")
	run(`(x)?\1`, "y")
	run(`(?=(a))a\1`, "aa")
	run(`(?<=a)(b)`, "ab")
	fmt.Println("runes of é̗:", vm.NewInput("é̗").Runes())
}
