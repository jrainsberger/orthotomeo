package corpus_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jrainsberger/orthotomeo/corpus"
	"github.com/jrainsberger/orthotomeo/sources"
)

// mkFixture builds a tiny two-root corpus tree mirroring the real on-disk
// split documented in docs/PLAN.md: bible-text/ and cross_references.txt
// under one parent, STEPBible-Data/ and LXX-Swete-1930/ under another.
func mkFixture(t *testing.T) (claudeRoot, referenceRoot string) {
	t.Helper()
	claudeRoot = t.TempDir()
	referenceRoot = t.TempDir()

	files := map[string]string{
		filepath.Join(claudeRoot, "bible-text", "KJV", "KJV.json"):              "{}",
		filepath.Join(claudeRoot, "bible-text", "WEB", "01-GENeng-web.usfm"):    "x",
		filepath.Join(claudeRoot, "bible-text", "WEB", "02-EXOeng-web.usfm"):    "x",
		filepath.Join(claudeRoot, "cross_references.txt"):                       "From\tTo\tVotes\n",
		filepath.Join(referenceRoot, "STEPBible-Data", "Lexicons", "TBESG.txt"): "x",
		filepath.Join(referenceRoot, "LXX-Swete-1930", "00-versification.csv"):  "x",
	}
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return claudeRoot, referenceRoot
}

func TestLocateResolvesEachRootsTrees(t *testing.T) {
	claudeRoot, referenceRoot := mkFixture(t)
	roots := []string{claudeRoot, referenceRoot}

	tests := []struct {
		name       string
		sourceFile string
		wantCount  int
	}{
		{"single file under first root", "bible-text/KJV/KJV.json", 1},
		{"glob under first root", "bible-text/WEB/*.usfm", 2},
		{"bare filename under first root", "cross_references.txt", 1},
		{"single file under second root", "STEPBible-Data/Lexicons/TBESG*.txt", 1},
		{"glob under second root", "LXX-Swete-1930/*.csv", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := sources.Source{Code: "TEST", SourceFile: tc.sourceFile}
			matches, err := corpus.Locate(src, roots...)
			if err != nil {
				t.Fatalf("locate %s: %v", tc.sourceFile, err)
			}
			if len(matches) != tc.wantCount {
				t.Errorf("matches = %d, want %d (%v)", len(matches), tc.wantCount, matches)
			}
		})
	}
}

func TestLocateIsSortedAndDeterministic(t *testing.T) {
	claudeRoot, referenceRoot := mkFixture(t)
	src := sources.Source{Code: "WEB", SourceFile: "bible-text/WEB/*.usfm"}

	matches, err := corpus.Locate(src, claudeRoot, referenceRoot)
	if err != nil {
		t.Fatalf("locate: %v", err)
	}
	want := []string{
		filepath.Join(claudeRoot, "bible-text", "WEB", "01-GENeng-web.usfm"),
		filepath.Join(claudeRoot, "bible-text", "WEB", "02-EXOeng-web.usfm"),
	}
	for i, m := range matches {
		if m != want[i] {
			t.Errorf("matches[%d] = %q, want %q", i, m, want[i])
		}
	}
}

func TestLocateMissingTreeReturnsErrCorpusMissing(t *testing.T) {
	claudeRoot, referenceRoot := mkFixture(t)
	src := sources.Source{Code: "ASV", SourceFile: "bible-text/ASV/ASV.json"}

	_, err := corpus.Locate(src, claudeRoot, referenceRoot)
	if !errors.Is(err, corpus.ErrCorpusMissing) {
		t.Errorf("err = %v, want wrapping ErrCorpusMissing", err)
	}
}

func TestLocateOneRequiresExactlyOneMatch(t *testing.T) {
	claudeRoot, referenceRoot := mkFixture(t)
	roots := []string{claudeRoot, referenceRoot}

	one := sources.Source{Code: "KJV", SourceFile: "bible-text/KJV/KJV.json"}
	if _, err := corpus.LocateOne(one, roots...); err != nil {
		t.Errorf("LocateOne(KJV): %v", err)
	}

	many := sources.Source{Code: "WEB", SourceFile: "bible-text/WEB/*.usfm"}
	if _, err := corpus.LocateOne(many, roots...); err == nil {
		t.Error("LocateOne(WEB) with 2 matches: want error, got nil")
	}
}
