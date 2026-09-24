// Package store persists runs, trace events, and frontiers in SQLite.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

// Run is one stored execution.
type Run struct {
	ID             int64
	Pattern        string
	Input          string
	Optimize       bool
	Status         string
	Reason         string
	Counters       string // JSON
	Frontier       string // JSON, empty when done
	Result         string // JSON
	SyntaxVersion  string
	UnicodeVersion string
	CreatedAt      string
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	st := &Store{db: db}
	if err := st.migrate(); err != nil {
		return nil, err
	}
	return st, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  pattern TEXT NOT NULL,
  input TEXT NOT NULL,
  optimize INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL,
  reason TEXT NOT NULL DEFAULT '',
  counters TEXT NOT NULL DEFAULT '{}',
  frontier TEXT NOT NULL DEFAULT '',
  result TEXT NOT NULL DEFAULT '',
  syntax_version TEXT NOT NULL,
  unicode_version TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS events (
  run_id INTEGER NOT NULL,
  seq INTEGER NOT NULL,
  body TEXT NOT NULL,
  PRIMARY KEY (run_id, seq)
);`)
	return err
}

// CreateRun inserts a run and returns its id.
func (s *Store) CreateRun(r *Run) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO runs(pattern, input, optimize, status, reason, counters, frontier, result, syntax_version, unicode_version)
		 VALUES (?,?,?,?,?,?,?,?,?,?)`,
		r.Pattern, r.Input, boolInt(r.Optimize), r.Status, r.Reason,
		r.Counters, r.Frontier, r.Result, r.SyntaxVersion, r.UnicodeVersion)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateRun updates the mutable execution fields of a run.
func (s *Store) UpdateRun(r *Run) error {
	_, err := s.db.Exec(
		`UPDATE runs SET status=?, reason=?, counters=?, frontier=?, result=? WHERE id=?`,
		r.Status, r.Reason, r.Counters, r.Frontier, r.Result, r.ID)
	return err
}

// AppendEvents stores events starting at sequence startSeq.
func (s *Store) AppendEvents(runID int64, startSeq int, bodies []json.RawMessage) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO events(run_id, seq, body) VALUES (?,?,?)`)
	if err != nil {
		tx.Rollback()
		return err
	}
	for i, b := range bodies {
		if _, err := stmt.Exec(runID, startSeq+i, string(b)); err != nil {
			tx.Rollback()
			return err
		}
	}
	stmt.Close()
	return tx.Commit()
}

// EventCount returns how many events a run has.
func (s *Store) EventCount(runID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM events WHERE run_id=?`, runID).Scan(&n)
	return n, err
}

// Events returns all stored events of a run in order.
func (s *Store) Events(runID int64) ([]json.RawMessage, error) {
	rows, err := s.db.Query(`SELECT body FROM events WHERE run_id=? ORDER BY seq`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []json.RawMessage
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		out = append(out, json.RawMessage(body))
	}
	return out, rows.Err()
}

// GetRun loads one run.
func (s *Store) GetRun(id int64) (*Run, error) {
	r := &Run{}
	var opt int
	err := s.db.QueryRow(
		`SELECT id, pattern, input, optimize, status, reason, counters, frontier, result, syntax_version, unicode_version, created_at
		 FROM runs WHERE id=?`, id).Scan(
		&r.ID, &r.Pattern, &r.Input, &opt, &r.Status, &r.Reason,
		&r.Counters, &r.Frontier, &r.Result, &r.SyntaxVersion, &r.UnicodeVersion, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	r.Optimize = opt != 0
	return r, nil
}

// ListRuns returns recent runs, newest first.
func (s *Store) ListRuns(limit int) ([]*Run, error) {
	rows, err := s.db.Query(
		`SELECT id, pattern, input, optimize, status, reason, counters, frontier, result, syntax_version, unicode_version, created_at
		 FROM runs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Run
	for rows.Next() {
		r := &Run{}
		var opt int
		if err := rows.Scan(&r.ID, &r.Pattern, &r.Input, &opt, &r.Status, &r.Reason,
			&r.Counters, &r.Frontier, &r.Result, &r.SyntaxVersion, &r.UnicodeVersion, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.Optimize = opt != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

var _ = fmt.Sprintf
