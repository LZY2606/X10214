// Package store persists execution runs in SQLite so every result
// stays pinned to its syntax and Unicode data versions.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Run is one persisted execution record.
type Run struct {
	ID             int64     `json:"id"`
	Pattern        string    `json:"pattern"`
	Input          string    `json:"input"`
	Status         string    `json:"status"`
	SyntaxVersion  string    `json:"syntaxVersion"`
	UnicodeVersion string    `json:"unicodeVersion"`
	ResultJSON     string    `json:"resultJSON"`
	CreatedAt      time.Time `json:"createdAt"`
}

// Store wraps the SQLite handle.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite database at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS runs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		pattern TEXT NOT NULL,
		input TEXT NOT NULL,
		status TEXT NOT NULL,
		syntax_version TEXT NOT NULL,
		unicode_version TEXT NOT NULL,
		result_json TEXT NOT NULL,
		created_at TEXT NOT NULL
	)`)
	return err
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// SaveRun persists one run and returns its id.
func (s *Store) SaveRun(ctx context.Context, r Run) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO runs (pattern, input, status, syntax_version, unicode_version, result_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.Pattern, r.Input, r.Status, r.SyntaxVersion, r.UnicodeVersion, r.ResultJSON,
		time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, fmt.Errorf("save run: %w", err)
	}
	return res.LastInsertId()
}

// RecentRuns returns the newest runs, newest first.
func (s *Store) RecentRuns(ctx context.Context, limit int) ([]Run, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, pattern, input, status, syntax_version, unicode_version, result_json, created_at
		 FROM runs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		var r Run
		var created string
		if err := rows.Scan(&r.ID, &r.Pattern, &r.Input, &r.Status,
			&r.SyntaxVersion, &r.UnicodeVersion, &r.ResultJSON, &created); err != nil {
			return nil, err
		}
		r.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, r)
	}
	return out, rows.Err()
}
