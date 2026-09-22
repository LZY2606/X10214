package main
import ("fmt";"capturetrail/internal/rx")
func main(){ for _, s := range []string{`(?<x>a)`,`(\d)\5`} { _,e:=rx.Parse(s); fmt.Printf("%q -> %v\n",s,e) } }
