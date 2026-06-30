// Package sources holds the provenance registry: one declarative row per
// corpus edition/resource that the importer can load. Every text and word row
// in the database carries a source_id back to one of these, so provenance and
// redistributability ("can I ship this byte?") are answerable per row.
//
// The registry is checked-in data (sources.json), not inferred at import time.
package sources

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
)

// Source is one provenance row. Field tags mirror sources.json and the
// sources table columns.
type Source struct {
	Code        string `json:"code"`
	FullName    string `json:"full_name"`
	Language    string `json:"language"`
	Type        string `json:"type"`
	License     string `json:"license"`
	Attribution string `json:"attribution"`
	SourceFile  string `json:"source_file"`
	Format      string `json:"format"`
	Shippable   bool   `json:"shippable"`
	FetchURL    string `json:"fetch_url"`
}

//go:embed sources.json
var registryJSON []byte

// Registry returns the declared provenance rows, decoded from sources.json.
func Registry() ([]Source, error) {
	var s []Source
	if err := json.Unmarshal(registryJSON, &s); err != nil {
		return nil, fmt.Errorf("decode sources.json: %w", err)
	}
	return s, nil
}

// Seed inserts the registry into the sources table and returns the count
// inserted. It runs in a single transaction so a partial seed never lands.
func Seed(db *sql.DB) (int, error) {
	reg, err := Registry()
	if err != nil {
		return 0, err
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO sources
			(code, full_name, language, type, license, attribution, source_file, format, shippable, fetch_url)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, fmt.Errorf("prepare insert: %w", err)
	}
	defer stmt.Close()

	for _, s := range reg {
		if _, err := stmt.Exec(
			s.Code, s.FullName, s.Language, s.Type, s.License,
			s.Attribution, s.SourceFile, s.Format, s.Shippable, s.FetchURL,
		); err != nil {
			return 0, fmt.Errorf("insert source %q: %w", s.Code, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return len(reg), nil
}
