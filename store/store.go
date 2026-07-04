// Package store opens the derived SQLite database and applies the schema.
//
// The corpus files are the source of truth; this database is a regenerable
// build artifact assembled by cmd/build. See docs/erd-v1.svg.
package store

import (
	"database/sql"
	_ "embed"
	"fmt"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// SchemaVersion is bumped every time schema.sql adds/renames/drops a column
// or table. It is stamped into the DB via PRAGMA user_version (ApplySchema)
// and checked at read time (engine.Open) so a DB built against an older
// schema.sql fails fast with an actionable "rebuild your DB" error instead
// of a cryptic "no such column" deep inside a query - CREATE TABLE IF NOT
// EXISTS silently keeps an existing table's old shape forever, so this is
// the only signal that a DB predates a schema change.
const SchemaVersion = 1

// Open opens (creating if needed) the SQLite database at path. The pool is
// pinned to a single connection so per-connection PRAGMAs stay in effect and
// an in-progress build sees a consistent view.
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA foreign_keys = ON; PRAGMA busy_timeout = 5000;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("set pragmas: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping %s: %w", path, err)
	}
	return db, nil
}

// ApplySchema creates the tables if they do not already exist, and stamps
// the DB with the current SchemaVersion (see its doc comment).
func ApplySchema(db *sql.DB) error {
	if _, err := db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d;`, SchemaVersion)); err != nil {
		return fmt.Errorf("set schema version: %w", err)
	}
	return nil
}
