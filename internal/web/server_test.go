package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pairwise-gsb/internal/store"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st)
}

func post(t *testing.T, s *Server, path string, body string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST %s: %d %s", path, rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestIndexShowsTitle(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "捕获轨迹室") {
		t.Fatalf("index page missing 捕获轨迹室")
	}
}

func TestRunAndStepFlow(t *testing.T) {
	s := newTestServer(t)
	res := post(t, s, "/api/run", `{
		"pattern": "(a|b)*c",
		"input": "ababc",
		"budget": {"maxSteps": 5}
	}`)
	if res["status"] != "indeterminate" {
		t.Fatalf("status = %v, want indeterminate", res["status"])
	}
	if res["syntaxVersion"] == "" || res["unicodeVersion"] == "" {
		t.Fatalf("versions not pinned: %v", res)
	}
	id := int64(res["runID"].(float64))
	body, _ := json.Marshal(map[string]any{"runID": id, "steps": 100000})
	req := httptest.NewRequest(http.MethodPost, "/api/step", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	if out["status"] != "match" {
		t.Fatalf("resumed status = %v, want match", out["status"])
	}
}

func TestDiffEndpoint(t *testing.T) {
	s := newTestServer(t)
	res := post(t, s, "/api/diff", `{"patternA":"a+","patternB":"a*","maxLen":3}`)
	ce, ok := res["counterexamples"].([]any)
	if !ok || len(ce) != 1 || ce[0].(string) != "" {
		t.Fatalf("counterexamples = %v", res["counterexamples"])
	}
	if res["note"].(string) == "" {
		t.Fatalf("missing budget note")
	}
}
