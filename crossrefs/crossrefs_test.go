package crossrefs_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jrainsberger/orthotomeo/books"
	"github.com/jrainsberger/orthotomeo/crossrefs"
	"github.com/jrainsberger/orthotomeo/sources"
	"github.com/jrainsberger/orthotomeo/store"
	"github.com/jrainsberger/orthotomeo/verses"
)

const miniKJV = `{"books":[
  {"name":"I Samuel","chapters":[{"chapter":1,"verses":[{"verse":1},{"verse":2}]},{"chapter":2,"verses":[{"verse":1}]}]},
  {"name":"Mark","chapters":[{"chapter":1,"verses":[{"verse":1}]}]}
]}`

// Row 3 is a range; row 4 points at Gen, which is not in the mini-spine, so it
// must be skipped (not silently inserted, not a crash).
const fixtureTSV = "From Verse\tTo Verse\tVotes\t#cc-by\n" +
	"1Sam.1.1\tMark.1.1\t10\n" +
	"1Sam.1.2\t1Sam.2.1\t-5\n" +
	"1Sam.1.1\t1Sam.1.1-1Sam.1.2\t7\n" +
	"1Sam.1.1\tGen.1.1\t99\n"

func setup(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.ApplySchema(db); err != nil {
		t.Fatalf("schema: %v", err)
	}
	if _, err := sources.Seed(db); err != nil {
		t.Fatalf("seed sources: %v", err)
	}
	if _, _, err := books.Seed(db); err != nil {
		t.Fatalf("seed books: %v", err)
	}
	if _, err := verses.BuildSpine(db, strings.NewReader(miniKJV)); err != nil {
		t.Fatalf("build spine: %v", err)
	}
	return db
}

func TestLoadResolvesAndSkips(t *testing.T) {
	db := setup(t)

	ins, skip, err := crossrefs.Load(db, strings.NewReader(fixtureTSV))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if ins != 3 {
		t.Errorf("inserted %d, want 3", ins)
	}
	if skip != 1 {
		t.Errorf("skipped %d, want 1 (Gen.1.1 not in spine)", skip)
	}

	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM cross_references`).Scan(&rows); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 3 {
		t.Errorf("table has %d rows, want 3", rows)
	}
}

func TestLoadKeepsNegativeVotesAndRanges(t *testing.T) {
	db := setup(t)
	if _, _, err := crossrefs.Load(db, strings.NewReader(fixtureTSV)); err != nil {
		t.Fatalf("load: %v", err)
	}

	// Negative votes are data, not noise - they must be preserved.
	var negatives int
	if err := db.QueryRow(`SELECT COUNT(*) FROM cross_references WHERE votes < 0`).Scan(&negatives); err != nil {
		t.Fatalf("count negatives: %v", err)
	}
	if negatives != 1 {
		t.Errorf("negative-vote rows = %d, want 1", negatives)
	}

	// Exactly one row is a range (to_verse_end non-null).
	var ranged int
	if err := db.QueryRow(`SELECT COUNT(*) FROM cross_references WHERE to_verse_end IS NOT NULL`).Scan(&ranged); err != nil {
		t.Fatalf("count ranged: %v", err)
	}
	if ranged != 1 {
		t.Errorf("ranged rows = %d, want 1", ranged)
	}

	// Every row attributes to the OpenBible source.
	var bad int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM cross_references c
		JOIN sources s ON s.id = c.source_id
		WHERE s.code <> 'OpenBible-xref'`).Scan(&bad); err != nil {
		t.Fatalf("provenance check: %v", err)
	}
	if bad != 0 {
		t.Errorf("%d rows not attributed to OpenBible-xref", bad)
	}
}
