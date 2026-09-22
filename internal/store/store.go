// Package store persists runs, events and differential searches to SQLite.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"
)

// Store is the persistence layer.
type Store struct {
	db *sql.DB
}

// Open opens (creating the schema in) path.
func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `
		PRAGMA journal_mode=WAL;
		PRAGMA foreign_keys=ON;
		CREATE TABLE IF NOT EXISTS runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			pattern TEXT NOT NULL,
			input TEXT NOT NULL,
			optimized INTEGER NOT NULL,
			status TEXT NOT NULL,
			syntax_version TEXT NOT NULL,
			unicode_version TEXT NOT NULL,
			match_json TEXT,
			frontier_json TEXT,
			budget_json TEXT NOT NULL,
			used_json TEXT NOT NULL,
			reason TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE TABLE IF NOT EXISTS events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
			seq INTEGER NOT NULL,
			payload TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS events_run_seq ON events(run_id, seq);
		CREATE TABLE IF NOT EXISTS diffs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			pattern_a TEXT NOT NULL,
			pattern_b TEXT NOT NULL,
			result_json TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
	`); err != nil {
		return nil, fmt.Errorf("schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// SaveRunRecord stores a run with its trace events.
type SaveRunRecord struct {
	Pattern        string
	Input          string
	Optimized      bool
	Status         string
	SyntaxVersion  string
	UnicodeVersion string
	MatchJSON      []byte
	FrontierJSON   []byte
	BudgetJSON     []byte
	UsedJSON       []byte
	Reason         string
	Events         []json.RawMessage
}

// SaveRun inserts a run and returns its id.
func (s *Store) SaveRun(ctx context.Context, rec SaveRunRecord) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `
		INSERT INTO runs(pattern, input, optimized, status, syntax_version,
			unicode_version, match_json, frontier_json, budget_json, used_json, reason)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		rec.Pattern, rec.Input, boolInt(rec.Optimized), rec.Status,
		rec.SyntaxVersion, rec.UnicodeVersion, nullable(rec.MatchJSON),
		nullable(rec.FrontierJSON), rec.BudgetJSON, rec.UsedJSON, rec.Reason)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if len(rec.Events) > 0 {
		stmt, err := tx.PrepareContext(ctx, `INSERT INTO events(run_id, seq, payload) VALUES(?,?,?)`)
		if err != nil {
			return 0, err
		}
		defer stmt.Close()
		for i, ev := range rec.Events {
			if _, err := stmt.ExecContext(ctx, id, i+1, []byte(ev)); err != nil {
				return 0, err
			}
		}
	}
	return id, tx.Commit()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullable(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}

// UpdateRunFrontier persists a new frontier/status after resume.
func (s *Store) UpdateRunFrontier(ctx context.Context, id int64, status string,
	frontierJSON, matchJSON []byte, usedJSON, reason []byte) error {
	_, err := s.db.ExecContext(ctx, `UPDATE runs SET status=?, frontier_json=?, match_json=?, used_json=?, reason=? WHERE id=?`,
		status, nullable(frontierJSON), nullable(matchJSON), string(usedJSON), string(reason), id)
	return err
}

// AppendEvent stores one additional trace event for a run.
func (s *Store) AppendEvent(ctx context.Context, runID int64, seq int, payload json.RawMessage) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO events(run_id, seq, payload) VALUES(?,?,?)`, runID, seq, string(payload))
	return err
}

// RunSummary is the stored metadata of a run.
type RunSummary struct {
	ID             int64  `json:"id"`
	Pattern        string `json:"pattern"`
	Input          string `json:"input"`
	Optimized      bool   `json:"optimized"`
	Status         string `json:"status"`
	SyntaxVersion  string `json:"syntax_version"`
	UnicodeVersion string `json:"unicode_version"`
	Reason         string `json:"reason"`
}

// ListRuns returns the most recent runs.
func (s *Store) ListRuns(ctx context.Context, limit int) ([]RunSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, pattern, input, optimized, status, syntax_version, unicode_version, reason
		FROM runs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunSummary
	for rows.Next() {
		var r RunSummary
		var opt int
		var reason sql.NullString
		if err := rows.Scan(&r.ID, &r.Pattern, &r.Input, &opt, &r.Status,
			&r.SyntaxVersion, &r.UnicodeVersion, &reason); err != nil {
			return nil, err
		}
		r.Optimized = opt == 1
		r.Reason = reason.String
		out = append(out, r)
	}
	return out, rows.Err()
}

// LoadEvents returns the trace events of a run.
func (s *Store) LoadEvents(ctx context.Context, runID int64) ([]json.RawMessage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM events WHERE run_id=? ORDER BY seq`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []json.RawMessage
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, json.RawMessage(p))
	}
	return out, rows.Err()
}

// LoadRunRow returns selected raw columns of a run.
func (s *Store) LoadRunRow(ctx context.Context, id int64) (map[string]string, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT pattern, input, optimized, status, syntax_version, unicode_version,
			COALESCE(match_json,''), COALESCE(frontier_json,''), budget_json, used_json,
			COALESCE(reason,'')
		FROM runs WHERE id=?`, id)
	var pattern, input, status, sv, uv, match, frontier, budget, used, reason string
	var opt int
	if err := row.Scan(&pattern, &input, &opt, &status, &sv, &uv, &match, &frontier, &budget, &used, &reason); err != nil {
		return nil, err
	}
	return map[string]string{
		"pattern": pattern, "input": input, "optimized": fmt.Sprint(opt),
		"status": status, "syntax_version": sv, "unicode_version": uv,
		"match_json": match, "frontier_json": frontier, "budget_json": budget,
		"used_json": used, "reason": reason,
	}, nil
}

// SaveDiff persists a differential search and returns its id.
func (s *Store) SaveDiff(ctx context.Context, a, b string, result []byte) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO diffs(pattern_a, pattern_b, result_json) VALUES(?,?,?)`, a, b, string(result))
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return id, nil
}

// LoadDiff returns the stored differential result payload.
func (s *Store) LoadDiff(ctx context.Context, id int64) (string, error) {
	var payload string
	err := s.db.QueryRowContext(ctx, `SELECT result_json FROM diffs WHERE id=?`, id).Scan(&payload)
	return payload, err
}
