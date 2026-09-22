// Package server exposes the regex execution model to the capture
// trace room page.
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"capturelab/internal/regex"
	"capturelab/internal/store"
)

// Server wires the regex engine, persistence and HTTP handlers.
type Server struct {
	st       *store.Store
	mu       sync.Mutex
	sessions map[string]*session
	sessSeq  int64
}

type session struct {
	pattern string
	input   string
	root    regex.Node
	prog    *regex.Program
	vm      *regex.VM
	sent    int
}

// New constructs a Server.
func New(st *store.Store) *Server {
	return &Server{st: st, sessions: map[string]*session{}}
}

// Handler returns the HTTP mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handlePage)
	mux.HandleFunc("/api/parse", s.handleParse)
	mux.HandleFunc("/api/match", s.handleMatch)
	mux.HandleFunc("/api/session", s.handleNewSession)
	mux.HandleFunc("/api/session/step", s.handleStep)
	mux.HandleFunc("/api/session/resume", s.handleResume)
	mux.HandleFunc("/api/diff", s.handleDiff)
	mux.HandleFunc("/api/runs", s.handleRuns)
	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

type matchReq struct {
	Pattern  string         `json:"pattern"`
	Input    string         `json:"input"`
	Budget   regex.Budget   `json:"budget"`
	Optimize bool           `json:"optimize"`
}

func (s *Server) handleParse(w http.ResponseWriter, r *http.Request) {
	var req matchReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	root, info, err := regex.Parse(req.Pattern)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	prog := regex.Compile(root, info, req.Optimize)
	writeJSON(w, map[string]any{
		"ast":            root.String(),
		"program":        regex.ProgramText(prog),
		"syntaxVersion":  regex.SyntaxVersion,
		"unicodeVersion": regex.UnicodeVersion,
		"groupCount":     info.GroupCount,
		"groups":         info.Groups,
	})
}

func (s *Server) handleMatch(w http.ResponseWriter, r *http.Request) {
	var req matchReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	res, err := regex.Execute(req.Pattern, req.Input, regex.Options{Budget: req.Budget, Optimize: req.Optimize}, nil)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	if s.st != nil {
		raw, _ := json.Marshal(res)
		_, _ = s.st.SaveRun(context.Background(), store.Run{
			Pattern:        req.Pattern,
			Input:          req.Input,
			Status:         string(res.Status),
			SyntaxVersion:  res.SyntaxVersion,
			UnicodeVersion: res.UnicodeVersion,
			ResultJSON:     string(raw),
		})
	}
	writeJSON(w, res)
}

type sessionReq struct {
	Pattern  string       `json:"pattern"`
	Input    string       `json:"input"`
	Budget   regex.Budget `json:"budget"`
	Optimize bool         `json:"optimize"`
}

func (s *Server) handleNewSession(w http.ResponseWriter, r *http.Request) {
	var req sessionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	root, info, err := regex.Parse(req.Pattern)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	prog := regex.Compile(root, info, req.Optimize)
	vm := regex.NewVM(prog, req.Input, req.Budget, false)
	s.mu.Lock()
	s.sessSeq++
	id := formatID(s.sessSeq)
	s.sessions[id] = &session{pattern: req.Pattern, input: req.Input, root: root, prog: prog, vm: vm}
	s.mu.Unlock()
	writeJSON(w, map[string]any{
		"id":             id,
		"program":        regex.ProgramText(prog),
		"syntaxVersion":  regex.SyntaxVersion,
		"unicodeVersion": regex.UnicodeVersion,
	})
}

type stepReq struct {
	ID          string `json:"id"`
	Count       int    `json:"count"`
	Breakpoints []int  `json:"breakpoints"`
}

func (s *Server) handleStep(w http.ResponseWriter, r *http.Request) {
	var req stepReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	if req.Count <= 0 {
		req.Count = 1
	}
	s.mu.Lock()
	sess := s.sessions[req.ID]
	s.mu.Unlock()
	if sess == nil {
		writeErr(w, 404, errNoSession)
		return
	}
	bp := map[int]bool{}
	for _, pc := range req.Breakpoints {
		bp[pc] = true
	}
	stoppedAtBreakpoint := false
	for i := 0; i < req.Count; i++ {
		if cur := sess.vm.Cur(); cur != nil && bp[cur.PC] {
			stoppedAtBreakpoint = true
			break
		}
		if sess.vm.StatusOf() != regex.StatusRunning {
			break
		}
		sess.vm.Step()
	}
	writeJSON(w, s.sessionView(sess, stoppedAtBreakpoint))
}

type resumeReq struct {
	ID     string       `json:"id"`
	Budget regex.Budget `json:"budget"`
}

func (s *Server) handleResume(w http.ResponseWriter, r *http.Request) {
	var req resumeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	s.mu.Lock()
	sess := s.sessions[req.ID]
	s.mu.Unlock()
	if sess == nil {
		writeErr(w, 404, errNoSession)
		return
	}
	if f := sess.vm.Frontier(); f != nil {
		sess.vm.ResumeFrom(f, req.Budget)
	}
	sess.vm.Run()
	writeJSON(w, s.sessionView(sess, false))
}

func (s *Server) sessionView(sess *session, stoppedAtBreakpoint bool) map[string]any {
	events := sess.vm.Events()
	view := map[string]any{
		"status":      sess.vm.StatusOf(),
		"reason":      sess.vm.Reason(),
		"stats":       sess.vm.Stats(),
		"cur":         sess.vm.Cur(),
		"stack":       sess.vm.Stack(),
		"events":      events[sess.sent:],
		"frontier":    sess.vm.Frontier(),
		"breakpoint":  stoppedAtBreakpoint,
		"traceTotal":  len(events),
	}
	if sess.vm.StatusOf() != regex.StatusRunning {
		view["result"] = regex.ResultFromVM(sess.pattern, sess.input, sess.root, sess.prog, sess.vm)
	}
	sess.sent = len(events)
	return view
}

type diffReq struct {
	PatternA   string `json:"patternA"`
	PatternB   string `json:"patternB"`
	MaxLen     int    `json:"maxLen"`
	MaxResults int    `json:"maxResults"`
}

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	var req diffReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	res, err := regex.Diff(req.PatternA, req.PatternB, req.MaxLen, req.MaxResults)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, res)
}

func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request) {
	if s.st == nil {
		writeJSON(w, []any{})
		return
	}
	runs, err := s.st.RecentRuns(r.Context(), 30)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, runs)
}

var errNoSession = newErr("session not found")

type errStr string

func (e errStr) Error() string { return string(e) }

func newErr(msg string) error { return errStr(msg) }

func formatID(n int64) string {
	return "s" + itoa(n)
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
