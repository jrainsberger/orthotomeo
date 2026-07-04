package store_test

import (
	"path/filepath"
	"testing"

	"github.com/jrainsberger/orthotomeo/store"
)

func TestApplySchemaStampsCurrentVersion(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := store.ApplySchema(db); err != nil {
		t.Fatalf("apply schema: %v", err)
	}

	var version int
	if err := db.QueryRow(`PRAGMA user_version;`).Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != store.SchemaVersion {
		t.Errorf("user_version = %d, want %d (store.SchemaVersion)", version, store.SchemaVersion)
	}
}
