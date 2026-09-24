package engine

// Capture is one half-open capture pair. -1 means unset.
type Capture struct {
	Start int  `json:"start"`
	End   int  `json:"end"`
	Set   bool `json:"set"`
}

// caps is a capture vector version. Every Save copies the vector (each copy
// counts against the capture snapshot budget).
type caps []Capture

func rootCaps(n int) caps {
	v := make(caps, n+1)
	for i := range v {
		v[i] = Capture{Start: -1, End: -1}
	}
	return v
}

func (c caps) clone() caps {
	nv := make(caps, len(c))
	copy(nv, c)
	return nv
}

// loopState tracks a quantifier invocation's entry rune position and the
// number of completed iterations.
type loopState struct {
	start int
	count int
}

type loops struct {
	parent *loops
	id     int
	val    loopState
}

func (l *loops) get(id int) (loopState, bool) {
	for cur := l; cur != nil; cur = cur.parent {
		if cur.id == id {
			return cur.val, true
		}
	}
	return loopState{}, false
}

func (l *loops) set(id int, v loopState) *loops {
	return &loops{parent: l, id: id, val: v}
}

// lookFrame describes an active lookahead anchor.
type lookFrame struct {
	pos      int
	positive bool
}

type lookStack struct {
	parent *lookStack
	frame  lookFrame
}

func (s *lookStack) push(f lookFrame) *lookStack {
	return &lookStack{parent: s, frame: f}
}

func (s *lookStack) top() (lookFrame, *lookStack) {
	if s == nil {
		return lookFrame{}, nil
	}
	return s.frame, s.parent
}

// Thread is one ordered backtracking alternative.
type Thread struct {
	PC      int        `json:"pc"`
	Pos     int        `json:"pos"`
	Caps    caps       `json:"-"`
	Loops   *loops     `json:"-"`
	Looks   *lookStack `json:"-"`
	LBStart int        `json:"lbStart"`
	LBNeg   bool       `json:"lbNeg"`
	Kind    string     `json:"kind"`
	Depth   int        `json:"depth"`
}
