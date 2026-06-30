package books_test

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/jrainsberger/orthotomeo/books"
	"github.com/jrainsberger/orthotomeo/store"
)

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
	reg, err := books.Registry()
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	if len(reg) != 66 {
		t.Fatalf("got %d books, want 66 (protestant canon)", len(reg))
	}

	var ot, nt int
	seenCode := map[string]bool{}
	seenOrder := map[int]bool{}
	for _, b := range reg {
		// Soft-fail so every offending row surfaces in one run.
		if seenCode[b.Code] {
			t.Errorf("duplicate code %q", b.Code)
		}
		seenCode[b.Code] = true
		if seenOrder[b.Order] {
			t.Errorf("duplicate order %d (%s)", b.Order, b.Code)
		}
		seenOrder[b.Order] = true
		if b.Order < 1 || b.Order > 66 {
			t.Errorf("%s: order %d out of range", b.Code, b.Order)
		}
		if b.Code == "" || b.OSIS == "" || b.Dotted == "" || b.Name == "" {
			t.Errorf("%s: empty alias field(s): %+v", b.Code, b)
		}
		switch b.Section {
		case "ot":
			ot++
		case "nt":
			nt++
		default:
			t.Errorf("%s: bad section %q", b.Code, b.Section)
		}
	}
	if ot != 39 || nt != 27 {
		t.Errorf("section split = %d ot / %d nt, want 39 / 27", ot, nt)
	}
}

func TestSeedCounts(t *testing.T) {
	db := newDB(t)
	nb, na, err := books.Seed(db)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if nb != 66 {
		t.Errorf("seeded %d books, want 66", nb)
	}
	if na != 66*4 {
		t.Errorf("seeded %d aliases, want %d (66 x 4 schemes)", na, 66*4)
	}

	var books, names int
	mustScan(t, db, `SELECT COUNT(*) FROM books`, &books)
	mustScan(t, db, `SELECT COUNT(*) FROM book_names`, &names)
	if books != 66 || names != 264 {
		t.Errorf("tables: %d books / %d names, want 66 / 264", books, names)
	}
}

// TestResolveCrossScheme proves the central guarantee: the same physical book
// is reached from every edition's naming scheme. These are the codes pulled
// verbatim from the actual source files (xref OSIS, STEPBible dotted, USFM).
func TestResolveCrossScheme(t *testing.T) {
	db := newDB(t)
	if _, _, err := books.Seed(db); err != nil {
		t.Fatalf("seed: %v", err)
	}

	tests := []struct {
		name   string
		usfm   string
		osis   string
		dotted string
		nameEn string
	}{
		{"Mark", "MRK", "Mark", "Mrk", "Mark"},
		{"John", "JHN", "John", "Jhn", "John"},
		{"Psalms", "PSA", "Ps", "Psa", "Psalms"},
		{"Song", "SNG", "Song", "Sng", "Song of Solomon"},
		{"Philemon", "PHM", "Phlm", "Phm", "Philemon"},
		{"1 Corinthians", "1CO", "1Cor", "1Co", "1 Corinthians"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id := mustResolve(t, db, "usfm", tc.usfm)
			for scheme, value := range map[string]string{
				"osis":    tc.osis,
				"dotted":  tc.dotted,
				"name-en": tc.nameEn,
			} {
				if got := mustResolve(t, db, scheme, value); got != id {
					t.Errorf("%s/%q resolved to %d, want %d (usfm %s)", scheme, value, got, id, tc.usfm)
				}
			}
		})
	}
}

func TestResolveUnknown(t *testing.T) {
	db := newDB(t)
	if _, _, err := books.Seed(db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := books.Resolve(db, "osis", "Nope"); !errors.Is(err, books.ErrUnknownBook) {
		t.Errorf("Resolve(unknown) err = %v, want ErrUnknownBook", err)
	}
}

func mustResolve(t *testing.T, db *sql.DB, scheme, value string) int64 {
	t.Helper()
	id, err := books.Resolve(db, scheme, value)
	if err != nil {
		t.Fatalf("resolve %s/%q: %v", scheme, value, err)
	}
	return id
}

func mustScan(t *testing.T, db *sql.DB, query string, dst *int) {
	t.Helper()
	if err := db.QueryRow(query).Scan(dst); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
}
