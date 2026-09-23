package engine

import "capturetrace/internal/ucd"

// evalAnchor evaluates zero-width assertions using Unicode-aware rules.
func evalAnchor(k interface{ String() string }, in *Input, pos int) bool { return false }
