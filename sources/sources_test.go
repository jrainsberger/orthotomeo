package sources_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jrainsberger/orthotomeo/sources"
	"github.com/jrainsberger/orthotomeo/store"
)

// newDB opens a throwaway file-backed database in the test's temp dir and
// applies the schema. Setup failures abort immediately.
func newDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.ApplySchema(db); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	return db
}

func TestRegistryWellFormed(t *testing.T) {
	reg, err := sources.Registry()
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	if len(reg) == 0 {
		t.Fatal("registry is empty")
	}

	allowedType := map[string]bool{
		"translation": true, "original": true, "lemma": true,
		"lexicon": true, "morph-codes": true, "versification": true,
		"crossref": true,
	}
	seen := map[string]bool{}
	for _, s := range reg {
		// Soft-fail independent checks so every offending row is reported.
		if s.Code == "" {
			t.Errorf("row with empty code: %+v", s)
		}
		if seen[s.Code] {
			t.Errorf("duplicate code in registry: %q", s.Code)
		}
		seen[s.Code] = true
		if s.FullName == "" {
			t.Errorf("%s: empty full_name", s.Code)
		}
		if !allowedType[s.Type] {
			t.Errorf("%s: unknown type %q", s.Code, s.Type)
		}
		if s.License == "" {
			t.Errorf("%s: empty license", s.Code)
		}
		// A non-shippable source must say where to fetch it; a shippable one must not.
		if !s.Shippable && s.FetchURL == "" {
			t.Errorf("%s: non-shippable but no fetch_url", s.Code)
		}
		if s.Shippable && s.FetchURL != "" {
			t.Errorf("%s: shippable but has fetch_url %q", s.Code, s.FetchURL)
		}
	}
}

func TestSeedInsertsAll(t *testing.T) {
	db := newDB(t)

	n, err := sources.Seed(db)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	reg, _ := sources.Registry()
	if n != len(reg) {
		t.Fatalf("seed reported %d, registry has %d", n, len(reg))
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sources`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != len(reg) {
		t.Errorf("table has %d rows, want %d", count, len(reg))
	}
}

func TestSeedProvenanceSpotChecks(t *testing.T) {
	db := newDB(t)
	if _, err := sources.Seed(db); err != nil {
		t.Fatalf("seed: %v", err)
	}

	tests := []struct {
		name       string
		code       string
		wantType   string
		wantShip   bool
		licenseHas string // substring expected in license (provenance flags)
	}{
		{"PD English", "KJV", "translation", true, "Public Domain"},
		{"clean Greek lexicon defs", "TBESG", "lexicon", true, "Abbott-Smith"},
		{"flagged Hebrew lexicon defs", "TBESH", "lexicon", true, "permission"},
		{"PD Swete Greek text", "Swete", "original", true, "Public Domain"},
		{"CC-BY tagged NT", "TAGNT", "original", true, "CC BY"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var typ, license string
			var ship bool
			err := db.QueryRow(
				`SELECT type, shippable, license FROM sources WHERE code = ?`, tc.code,
			).Scan(&typ, &ship, &license)
			if err != nil {
				t.Fatalf("query %s: %v", tc.code, err)
			}
			if typ != tc.wantType {
				t.Errorf("%s type = %q, want %q", tc.code, typ, tc.wantType)
			}
			if ship != tc.wantShip {
				t.Errorf("%s shippable = %v, want %v", tc.code, ship, tc.wantShip)
			}
			if !strings.Contains(license, tc.licenseHas) {
				t.Errorf("%s license %q missing %q", tc.code, license, tc.licenseHas)
			}
		})
	}
}

func TestSeedRejectsDuplicateCode(t *testing.T) {
	db := newDB(t)
	if _, err := sources.Seed(db); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	// UNIQUE(code) must reject a second seed.
	if _, err := sources.Seed(db); err == nil {
		t.Fatal("second seed succeeded; expected UNIQUE(code) violation")
	}
}
