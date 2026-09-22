// Package server exposes the capture trace room over HTTP with an embedded
// single-page UI and a JSON API.
package server

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strconv"
	"strings"

	rx "capturetraceroom/internal/regex"
	"capturetraceroom/internal/store"
)

//go:embed web/*
var webFS embed.FS

// Server wires the engine and the store to HTTP.
type Server struct {
	store *store.Store
	mux   *http.ServeMux
}

// New constructs a Server backed by dbPath ("" => in-memory).
func New(ctx context.Context, dbPath string) (*Server, error) {
	if dbPath == "" {
		dbPath = "file::memory:?cache=shared"
	}
	st, err := store.Open(ctx, dbPath)
	if err != nil {
		return nil, err
	}
	s := &Server{store: st, mux: http.NewServeMux()}
	s.routes()
	return s, nil
}

// Close releases resources.
func (s *Server) Close() error { return s.store.Close() }

func (s *Server) routes() {
	s.mux.HandleFunc("/api/parse", s.handleParse)
	s.mux.HandleFunc("/api/run", s.handleRun)
	s.mux.HandleFunc("/api/resume", s.handleResume)
	s.mux.HandleFunc("/api/runs", s.handleListRuns)
	s.mux.HandleFunc("/api/runs/", s.handleGetRun)
	s.mux.HandleFunc("/api/diff", s.handleDiff)
	s.mux.HandleFunc("/api/positions", s.handlePositions)
	s.mux.HandleFunc("/api/versions", s.handleVersions)

	sub, _ := fs.Sub(webFS, "web")
	s.mux.Handle("/", http.FileServer(http.FS(sub)))
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler { return s.mux }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

type parseRequest struct {
	Pattern  string `json:"pattern"`
	Optimize bool   `json:"optimize"`
}

func (s *Server) handleParse(w http.ResponseWriter, r *http.Request) {
	var req parseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	prog, err := rx.Build(req.Pattern, req.Optimize)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, prog.View())
}

type runRequest struct {
	Pattern     string          `json:"pattern"`
	Input       string          `json:"input"`
	Optimize    bool            `json:"optimize"`
	StepLimit   int             `json:"step_limit"`
	Breakpoints []rx.Breakpoint `json:"breakpoints"`
	Budget      *rx.Budget      `json:"budget"`
	Persist     bool            `json:"persist"`
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	var req runRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	prog, err := rx.Build(req.Pattern, req.Optimize)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	budget := rx.DefaultBudget()
	if req.Budget != nil {
		if req.Budget.MaxSteps > 0 {
			budget = *req.Budget
		}
	}
	opts := rx.Options{Budget: budget, StepLimit: req.StepLimit, Breakpoints: req.Breakpoints}
	res := rx.Search(prog, []byte(req.Input), opts)
	id := s.persist(r.Context(), prog, req.Input, res, req.Optimize, req.Persist)
	writeJSON(w, http.StatusOK, responsePayload{Result: res, RunID: id, Program: prog.View()})
}

type resumeRequest struct {
	Pattern     string          `json:"pattern"`
	Input       string          `json:"input"`
	Optimize    bool            `json:"optimize"`
	Frontier    *rx.Frontier    `json:"frontier"`
	RunID       int64           `json:"run_id"`
	StepLimit   int             `json:"step_limit"`
	Breakpoints []rx.Breakpoint `json:"breakpoints"`
	Budget      *rx.Budget      `json:"budget"`
}

type responsePayload struct {
	Result  *rx.Result `json:"result"`
	RunID   int64      `json:"run_id,omitempty"`
	Program rx.ASTView `json:"program"`
}

func (s *Server) handleResume(w http.ResponseWriter, r *http.Request) {
	var req resumeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	prog, err := rx.Build(req.Pattern, req.Optimize)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	frontier := req.Frontier
	if frontier == nil && req.RunID > 0 {
		row, err := s.store.LoadRunRow(r.Context(), req.RunID)
		if err != nil {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		if row["frontier_json"] == "" {
			writeErr(w, http.StatusConflict, "run has no resumable frontier")
			return
		}
		var f rx.Frontier
		if err := json.Unmarshal([]byte(row["frontier_json"]), &f); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		frontier = &f
	}
	if frontier == nil {
		writeErr(w, http.StatusBadRequest, "frontier or run_id required")
		return
	}
	budget := rx.DefaultBudget()
	if req.Budget != nil && req.Budget.MaxSteps > 0 {
		budget = *req.Budget
	}
	res := rx.Resume(prog, []byte(req.Input), frontier,
		rx.Options{Budget: budget, StepLimit: req.StepLimit, Breakpoints: req.Breakpoints})
	id := int64(0)
	if req.RunID > 0 {
		id = req.RunID
		fb, _ := json.Marshal(res.Frontier)
		var mb []byte
		if res.Match != nil {
			mb, _ = json.Marshal(res.Match)
		}
		ub, _ := json.Marshal(res.BudgetUsed)
		_ = s.store.UpdateRunFrontier(r.Context(), req.RunID, string(res.Status), fb, mb, ub, []byte(res.Reason))
	} else {
		id = s.persist(r.Context(), prog, req.Input, res, req.Optimize, true)
	}
	writeJSON(w, http.StatusOK, responsePayload{Result: res, RunID: id, Program: prog.View()})
}

func (s *Server) persist(ctx context.Context, prog *rx.Program, input string, res *rx.Result, opt, persist bool) int64 {
	if !persist {
		return 0
	}
	budget, _ := json.Marshal(res.Budget)
	used, _ := json.Marshal(res.BudgetUsed)
	var match, frontier []byte
	if res.Match != nil {
		match, _ = json.Marshal(res.Match)
	}
	if res.Frontier != nil {
		frontier, _ = json.Marshal(res.Frontier)
	}
	events := make([]json.RawMessage, 0, len(res.Events))
	for _, ev := range res.Events {
		raw, err := json.Marshal(ev)
		if err == nil {
			events = append(events, raw)
		}
	}
	id, err := s.store.SaveRun(ctx, store.SaveRunRecord{
		Pattern: prog.Pattern, Input: input, Optimized: opt,
		Status: string(res.Status), SyntaxVersion: res.SyntaxVersion,
		UnicodeVersion: res.UnicodeVersion, MatchJSON: match, FrontierJSON: frontier,
		BudgetJSON: budget, UsedJSON: used, Reason: res.Reason, Events: events,
	})
	if err != nil {
		return 0
	}
	return id
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	runs, err := s.store.ListRuns(r.Context(), limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if runs == nil {
		runs = []store.RunSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/runs/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad run id")
		return
	}
	row, err := s.store.LoadRunRow(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	events, _ := s.store.LoadEvents(r.Context(), id)
	if events == nil {
		events = []json.RawMessage{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": row, "events": events})
}

type diffRequest struct {
	PatternA string         `json:"pattern_a"`
	PatternB string         `json:"pattern_b"`
	Budget   *rx.DiffBudget `json:"budget"`
	Persist  bool           `json:"persist"`
}

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	var req diffRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	budget := rx.DefaultDiffBudget()
	if req.Budget != nil && req.Budget.MaxLength > 0 {
		budget = *req.Budget
		if budget.MaxCases == 0 {
			budget.MaxCases = rx.DefaultDiffBudget().MaxCases
		}
		if budget.Alphabet == "" {
			budget.Alphabet = rx.DefaultDiffBudget().Alphabet
		}
		if budget.PerMatch.MaxSteps == 0 {
			budget.PerMatch = rx.DefaultDiffBudget().PerMatch
		}
	}
	res, err := rx.Diff(req.PatternA, req.PatternB, budget)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if req.Persist {
		raw, _ := json.Marshal(res)
		_, _ = s.store.SaveDiff(r.Context(), req.PatternA, req.PatternB, raw)
	}
	writeJSON(w, http.StatusOK, res)
}

type positionsRequest struct {
	Input string `json:"input"`
}

func (s *Server) handlePositions(w http.ResponseWriter, r *http.Request) {
	var req positionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rx.MapPositions([]byte(req.Input)))
}

func (s *Server) handleVersions(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"syntax_version":  rx.SyntaxVersion,
		"unicode_version": rx.UnicodeDataVersion,
	})
}
