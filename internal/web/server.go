// Package web serves the 捕获轨迹室 UI and its JSON API.
package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"unicode"

	"pairwise-gsb/internal/diff"
	"pairwise-gsb/internal/engine"
	"pairwise-gsb/internal/store"
	"pairwise-gsb/internal/syntax"
)

type Server struct {
	st  *store.Store
	mux *http.ServeMux
}

func New(st *store.Store) *Server {
	s := &Server{st: st, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /", s.handleIndex)
	s.mux.HandleFunc("POST /api/run", s.handleRun)
	s.mux.HandleFunc("POST /api/step", s.handleStep)
	s.mux.HandleFunc("GET /api/run", s.handleGetRun)
	s.mux.HandleFunc("GET /api/runs", s.handleListRuns)
	s.mux.HandleFunc("POST /api/diff", s.handleDiff)
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

type astDTO struct {
	Kind     string   `json:"kind"`
	Label    string   `json:"label"`
	Children []astDTO `json:"children,omitempty"`
}

func toDTO(n *syntax.Node) astDTO {
	d := astDTO{Kind: n.Kind.String()}
	switch n.Kind {
	case syntax.Literal:
		d.Label = fmt.Sprintf("%q", n.Lit)
	case syntax.CharClass:
		d.Label = fmt.Sprintf("neg=%v ranges=%v cats=%v preds=%v", n.Negate, n.Ranges, n.Cats, n.Preds)
	case syntax.Repeat:
		max := strconv.Itoa(n.Max)
		if n.Max < 0 {
			max = "∞"
		}
		d.Label = fmt.Sprintf("{%d,%s} lazy=%v", n.Min, max, n.Lazy)
	case syntax.Group:
		d.Label = fmt.Sprintf("#%d %s", n.Index, n.Name)
	case syntax.Backref:
		d.Label = fmt.Sprintf("idx=%d name=%s", n.Index, n.Name)
	case syntax.Anchor:
		d.Label = n.Anchor.String()
	case syntax.Lookaround:
		d.Label = n.Look.String() + "..."
	}
	for _, c := range n.Children {
		d.Children = append(d.Children, toDTO(c))
	}
	return d
}

type groupDTO struct {
	Index     int    `json:"index"`
	Name      string `json:"name"`
	StartCP   int    `json:"startCP"`
	EndCP     int    `json:"endCP"`
	StartByte int    `json:"startByte"`
	EndByte   int    `json:"endByte"`
	Text      string `json:"text"`
}

type runResponse struct {
	RunID          int64            `json:"runID"`
	Status         string           `json:"status"`
	Reason         string           `json:"reason,omitempty"`
	SyntaxVersion  string           `json:"syntaxVersion"`
	UnicodeVersion string           `json:"unicodeVersion"`
	AST            astDTO           `json:"ast"`
	Program        []string         `json:"program"`
	Events         []engine.Event   `json:"events"`
	Counters       engine.Counters  `json:"counters"`
	Groups         []groupDTO       `json:"groups,omitempty"`
	Frontier       *engine.Frontier `json:"frontier,omitempty"`
}

func compilePattern(pattern string, optimize bool) (*syntax.Node, *syntax.Info, *engine.Program, error) {
	ast, info, err := syntax.Parse(pattern)
	if err != nil {
		return nil, nil, nil, err
	}
	prog, err := engine.Compile(ast, info)
	if err != nil {
		return nil, nil, nil, err
	}
	if optimize {
		prog = engine.Optimize(prog)
	}
	return ast, info, prog, nil
}

func groupsOf(prog *engine.Program, in *engine.Input, res engine.Result) []groupDTO {
	if res.Status != engine.StatusMatch {
		return nil
	}
	var out []groupDTO
	for g := 0; 2*g+1 < len(res.CapsCP); g++ {
		name := ""
		if g < len(prog.Groups) {
			name = prog.Groups[g]
		}
		s, e := res.CapsCP[2*g], res.CapsCP[2*g+1]
		gd := groupDTO{Index: g, Name: name, StartCP: s, EndCP: e,
			StartByte: res.CapsByte[2*g], EndByte: res.CapsByte[2*g+1]}
		if s >= 0 && e >= 0 {
			gd.Text = in.Slice(s, e)
		}
		out = append(out, gd)
	}
	return out
}

func marshalEvents(events []engine.Event) []json.RawMessage {
	out := make([]json.RawMessage, len(events))
	for i, ev := range events {
		b, _ := json.Marshal(ev)
		out[i] = b
	}
	return out
}

func marshalJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

type runRequest struct {
	Pattern  string        `json:"pattern"`
	Input    string        `json:"input"`
	Optimize bool          `json:"optimize"`
	Budget   engine.Budget `json:"budget"`
	Breaks   []int         `json:"breaks"`
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	var req runRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ast, _, prog, err := compilePattern(req.Pattern, req.Optimize)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	breaks := map[int]bool{}
	for _, pc := range req.Breaks {
		breaks[pc] = true
	}
	in := engine.DecodeInput(req.Input)
	v := engine.NewVM(prog, in, req.Budget, breaks)
	res := v.Run()
	events := v.Events()

	frontier := ""
	if res.Frontier != nil {
		frontier = marshalJSON(res.Frontier)
	}
	run := &store.Run{
		Pattern: req.Pattern, Input: req.Input, Optimize: req.Optimize,
		Status: string(res.Status), Reason: res.Reason,
		Counters: marshalJSON(v.Counters()), Frontier: frontier,
		Result:         marshalJSON(res),
		SyntaxVersion:  syntax.Version,
		UnicodeVersion: unicode.Version,
	}
	id, err := s.st.CreateRun(run)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.st.AppendEvents(id, 0, marshalEvents(events)); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, runResponse{
		RunID: id, Status: string(res.Status), Reason: res.Reason,
		SyntaxVersion: syntax.Version, UnicodeVersion: unicode.Version,
		AST: toDTO(ast), Program: prog.Disasm(), Events: events,
		Counters: *v.Counters(), Groups: groupsOf(prog, in, res),
		Frontier: res.Frontier,
	})
}

type stepRequest struct {
	RunID  int64 `json:"runID"`
	Steps  int64 `json:"steps"`
	Breaks []int `json:"breaks"`
}

func (s *Server) handleStep(w http.ResponseWriter, r *http.Request) {
	var req stepRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	run, err := s.st.GetRun(req.RunID)
	if err != nil {
		writeErr(w, http.StatusNotFound, fmt.Errorf("run %d: %w", req.RunID, err))
		return
	}
	if run.Frontier == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("run %d already finished with status %s", run.ID, run.Status))
		return
	}
	ast, _, prog, err := compilePattern(run.Pattern, run.Optimize)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	var frontier engine.Frontier
	if err := json.Unmarshal([]byte(run.Frontier), &frontier); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	var counters engine.Counters
	if err := json.Unmarshal([]byte(run.Counters), &counters); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	stored, err := s.st.Events(run.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	prior := make([]engine.Event, len(stored))
	for i, b := range stored {
		json.Unmarshal(b, &prior[i])
	}
	breaks := map[int]bool{}
	for _, pc := range req.Breaks {
		breaks[pc] = true
	}
	budget := engine.Budget{MaxSteps: counters.Steps + req.Steps}
	in := engine.DecodeInput(run.Input)
	v := engine.ResumeVM(prog, in, budget, &counters, &frontier, breaks, prior)
	res := v.Run()
	events := v.Events()

	frontierJSON := ""
	if res.Frontier != nil {
		frontierJSON = marshalJSON(res.Frontier)
	}
	run.Status = string(res.Status)
	run.Reason = res.Reason
	run.Counters = marshalJSON(v.Counters())
	run.Frontier = frontierJSON
	run.Result = marshalJSON(res)
	if err := s.st.UpdateRun(run); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.st.AppendEvents(run.ID, len(prior), marshalEvents(events[len(prior):])); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, runResponse{
		RunID: run.ID, Status: string(res.Status), Reason: res.Reason,
		SyntaxVersion: run.SyntaxVersion, UnicodeVersion: run.UnicodeVersion,
		AST: toDTO(ast), Program: prog.Disasm(),
		Events:   events[len(prior):],
		Counters: *v.Counters(), Groups: groupsOf(prog, in, res),
		Frontier: res.Frontier,
	})
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	run, err := s.st.GetRun(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	events, err := s.st.Events(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"run": run, "events": events,
	})
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.st.ListRuns(20)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

type diffRequest struct {
	PatternA string `json:"patternA"`
	PatternB string `json:"patternB"`
	MaxLen   int    `json:"maxLen"`
}

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	var req diffRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.MaxLen <= 0 || req.MaxLen > 8 {
		req.MaxLen = 4
	}
	out, err := diff.Counterexamples(req.PatternA, req.PatternB, req.MaxLen, engine.Budget{})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
