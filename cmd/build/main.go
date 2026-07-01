// Command build assembles the derived orthotomeo SQLite database from the
// corpus. It is regenerable: delete the output and re-run. Tables are filled
// per import ticket.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/jrainsberger/orthotomeo/books"
	"github.com/jrainsberger/orthotomeo/brentontext"
	"github.com/jrainsberger/orthotomeo/corpus"
	"github.com/jrainsberger/orthotomeo/crossrefs"
	"github.com/jrainsberger/orthotomeo/lexicon"
	"github.com/jrainsberger/orthotomeo/morph"
	"github.com/jrainsberger/orthotomeo/osswords"
	"github.com/jrainsberger/orthotomeo/sources"
	"github.com/jrainsberger/orthotomeo/store"
	"github.com/jrainsberger/orthotomeo/swetewords"
	"github.com/jrainsberger/orthotomeo/tagnt"
	"github.com/jrainsberger/orthotomeo/tahot"
	"github.com/jrainsberger/orthotomeo/verify"
	"github.com/jrainsberger/orthotomeo/versealign"
	"github.com/jrainsberger/orthotomeo/verses"
	"github.com/jrainsberger/orthotomeo/versetext"
	"github.com/jrainsberger/orthotomeo/webtext"
)

func main() {
	out := flag.String("out", "data/orthotomeo.db", "output SQLite path")
	// The corpus is split across two parent roots (docs/PLAN.md "Corpus
	// locations"): --corpus holds bible-text/ + cross_references.txt,
	// --reference holds STEPBible-Data/ + LXX-Swete-1930/. corpus.Locate tries
	// each root in turn, so either tree may also be symlinked under the other.
	// No default path: these are external inputs outside this repo, and a
	// machine-specific default would silently point at nothing on anyone
	// else's checkout.
	root := flag.String("corpus", "", "corpus root (bible-text, cross_references.txt) - required")
	reference := flag.String("reference", "", "reference root (STEPBible-Data, LXX-Swete-1930) - required")
	doVerify := flag.Bool("verify", false, "run the T14 completeness self-test after building; exit non-zero on any failure")
	flag.Parse()

	if *root == "" || *reference == "" {
		log.Fatal("both --corpus and --reference are required (see docs/PLAN.md \"Corpus locations\")")
	}

	if err := run(*out, *root, *reference, *doVerify); err != nil {
		log.Fatalf("build: %v", err)
	}
}

func run(out, root, reference string, doVerify bool) error {
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	db, err := store.Open(out)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := store.ApplySchema(db); err != nil {
		return err
	}

	nSrc, err := sources.Seed(db)
	if err != nil {
		return err
	}
	nBook, nAlias, err := books.Seed(db)
	if err != nil {
		return err
	}

	roots := []string{root, reference}

	nVerse, err := loadSpine(db, roots)
	if err != nil {
		return err
	}
	nXref, nSkip, err := loadXrefs(db, roots)
	if err != nil {
		return err
	}
	nLex, err := loadLexicon(db, roots)
	if err != nil {
		return err
	}
	nMorph, err := loadMorphCodes(db, roots)
	if err != nil {
		return err
	}
	nText, err := loadVerseText(db, roots)
	if err != nil {
		return err
	}
	nWeb, nWebSkip, nWebBooks, err := loadWebText(db, roots)
	if err != nil {
		return err
	}
	nBrenton, nBrentonFiles, err := loadBrentonText(db, roots)
	if err != nil {
		return err
	}
	nWords, nWordsSkip, nCompound, err := loadTAGNT(db, roots)
	if err != nil {
		return err
	}
	nHebWords, nHebSkip, nUntagged, err := loadTAHOT(db, roots)
	if err != nil {
		return err
	}
	nSwete, nSweteSkip, err := loadSweteWords(db, roots)
	if err != nil {
		return err
	}
	nOSS, nOSSSkip, nOSSMalformed, err := loadOSSWords(db, roots)
	if err != nil {
		return err
	}

	fmt.Printf("seeded %d sources, %d books (%d aliases), %d verses, %d cross-refs (%d skipped), %d lexicon entries, %d morph codes, %d verse texts, %d WEB verses (%d books, %d skipped), %d Brenton verses (%d chapter files), %d TAGNT words (%d skipped, %d compound), %d TAHOT words (%d skipped, %d untagged), %d Swete words (%d deuterocanon verses skipped), %d OSS words (%d out-of-scope rows skipped, %d malformed keys) -> %s\n",
		nSrc, nBook, nAlias, nVerse, nXref, nSkip, nLex, nMorph, nText, nWeb, nWebBooks, nWebSkip, nBrenton, nBrentonFiles, nWords, nWordsSkip, nCompound, nHebWords, nHebSkip, nUntagged, nSwete, nSweteSkip, nOSS, nOSSSkip, nOSSMalformed, out)

	if err := alignAllEditions(db); err != nil {
		return err
	}

	if doVerify {
		return runVerify(db)
	}
	return nil
}

// runVerify runs the T14 completeness self-test (invariant #3) against the
// just-built DB and reports every issue before returning an error - a
// verify failure means the build produced something wrong, so it is
// reported loudly rather than silently accepted.
func runVerify(db *sql.DB) error {
	report, err := verify.Run(db, verify.DefaultExpectations)
	if err != nil {
		return fmt.Errorf("verify: %w", err)
	}
	for _, n := range report.Notes {
		fmt.Printf("verify NOTE [%s]: %s\n", n.Check, n.Detail)
	}
	if !report.OK() {
		for _, iss := range report.Issues {
			fmt.Printf("verify FAIL [%s]: %s\n", iss.Check, iss.Detail)
		}
		return fmt.Errorf("verify: %d issue(s) found", len(report.Issues))
	}
	fmt.Println("verify: OK (all completeness checks passed)")
	return nil
}

// alignAllEditions runs the deterministic verse aligner (T4b) for every LXX
// edition loaded above, against the canonical spine. Each edition is
// independent (its own versification tag, its own source_id).
func alignAllEditions(db *sql.DB) error {
	editions := []struct {
		versification, sourceCode string
	}{
		{brentontext.Versification, "Brenton"},
		{swetewords.Versification, "Swete"},
		{osswords.Versification, "OSS-LXX-lemma"},
	}
	for _, e := range editions {
		counts, err := versealign.Align(db, e.versification, e.sourceCode)
		if err != nil {
			return fmt.Errorf("align %s: %w", e.versification, err)
		}
		fmt.Printf("aligned %s: %d exact, %d renumber, %d merge, %d divide, %d canonical-only, %d edition-only\n",
			e.versification, counts.Exact, counts.Renumber, counts.Merge, counts.Divide, counts.UnalignedCanonical, counts.UnalignedEdition)
	}
	return nil
}

// sourceByCode looks up the sources.json row by code; build wiring never
// hard-codes a corpus path itself, only the source code it wants to load.
func sourceByCode(code string) (sources.Source, error) {
	reg, err := sources.Registry()
	if err != nil {
		return sources.Source{}, err
	}
	for _, s := range reg {
		if s.Code == code {
			return s, nil
		}
	}
	return sources.Source{}, fmt.Errorf("source %q not in registry", code)
}

// openSource locates and opens the single file for a sources.json code via
// corpus.LocateOne, the only path-aware step in each loader below.
func openSource(code string, roots []string) (*os.File, error) {
	src, err := sourceByCode(code)
	if err != nil {
		return nil, err
	}
	path, err := corpus.LocateOne(src, roots...)
	if err != nil {
		return nil, fmt.Errorf("locate %s: %w", code, err)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return f, nil
}

func loadSpine(db *sql.DB, roots []string) (int, error) {
	f, err := openSource("KJV", roots)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return verses.BuildSpine(db, f)
}

func loadXrefs(db *sql.DB, roots []string) (inserted, skipped int, err error) {
	f, err := openSource("OpenBible-xref", roots)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	return crossrefs.Load(db, f)
}

// loadLexicon loads TBESG (Greek) and TBESH (Hebrew).
func loadLexicon(db *sql.DB, roots []string) (int, error) {
	greek, err := openSource("TBESG", roots)
	if err != nil {
		return 0, err
	}
	defer greek.Close()
	nGreek, err := lexicon.Load(db, greek, "grc", "Abbott-Smith PD")
	if err != nil {
		return 0, err
	}

	hebrew, err := openSource("TBESH", roots)
	if err != nil {
		return 0, err
	}
	defer hebrew.Close()
	nHebrew, err := lexicon.Load(db, hebrew, "he", "BDB/Online Bible - permission")
	if err != nil {
		return 0, err
	}

	return nGreek + nHebrew, nil
}

// loadVerseText loads KJV and ASV (identical JSON shape).
func loadVerseText(db *sql.DB, roots []string) (int, error) {
	kjv, err := openSource("KJV", roots)
	if err != nil {
		return 0, err
	}
	defer kjv.Close()
	nKJV, err := versetext.Load(db, kjv, "KJV")
	if err != nil {
		return 0, err
	}

	asv, err := openSource("ASV", roots)
	if err != nil {
		return 0, err
	}
	defer asv.Close()
	nASV, err := versetext.Load(db, asv, "ASV")
	if err != nil {
		return 0, err
	}

	return nKJV + nASV, nil
}

// loadWebText loads every WEB USFM file (one per book, plus front matter,
// glossary, and deuterocanon files outside v1 scope - webtext.Load reports
// those as not loaded rather than an error).
func loadWebText(db *sql.DB, roots []string) (inserted, skipped, nBooks int, err error) {
	src, err := sourceByCode("WEB")
	if err != nil {
		return 0, 0, 0, err
	}
	paths, err := corpus.Locate(src, roots...)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("locate WEB: %w", err)
	}

	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("open %s: %w", path, err)
		}
		code, n, skip, loaded, err := webtext.Load(db, f)
		f.Close()
		if err != nil {
			return 0, 0, 0, fmt.Errorf("%s: %w", path, err)
		}
		if !loaded {
			continue
		}
		inserted += n
		skipped += skip
		nBooks++
		if skip > 0 {
			fmt.Printf("  WEB %s: %d verses skipped (unresolved against canonical spine)\n", code, skip)
		}
	}
	return inserted, skipped, nBooks, nil
}

// loadBrentonText loads every Brenton LXX chapter file. Index/TOC pages
// (PSA000.htm, GEN.htm, index.htm), front matter, deuterocanon outside the
// 66-book registry, and the explicitly-skipped combined-book EZR.htm are
// reported as not loaded rather than an error (brentontext.skipBooks; see
// docs/PLAN.md T9 for the open Ezra/Nehemiah book-identity question).
func loadBrentonText(db *sql.DB, roots []string) (inserted, nFiles int, err error) {
	src, err := sourceByCode("Brenton")
	if err != nil {
		return 0, 0, err
	}
	paths, err := corpus.Locate(src, roots...)
	if err != nil {
		return 0, 0, fmt.Errorf("locate Brenton: %w", err)
	}

	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return 0, 0, fmt.Errorf("open %s: %w", path, err)
		}
		_, _, n, loaded, err := brentontext.Load(db, f, filepath.Base(path))
		f.Close()
		if err != nil {
			return 0, 0, fmt.Errorf("%s: %w", path, err)
		}
		if !loaded {
			continue
		}
		inserted += n
		nFiles++
	}
	return inserted, nFiles, nil
}

// loadTAGNT loads both TAGNT TSVs (Mat-Jhn, Act-Rev), both under the single
// "TAGNT" source code.
func loadTAGNT(db *sql.DB, roots []string) (inserted, skipped, compound int, err error) {
	src, err := sourceByCode("TAGNT")
	if err != nil {
		return 0, 0, 0, err
	}
	paths, err := corpus.Locate(src, roots...)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("locate TAGNT: %w", err)
	}

	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("open %s: %w", path, err)
		}
		n, skip, comp, err := tagnt.Load(db, f)
		f.Close()
		if err != nil {
			return 0, 0, 0, fmt.Errorf("%s: %w", path, err)
		}
		inserted += n
		skipped += skip
		compound += comp
	}
	return inserted, skipped, compound, nil
}

// loadTAHOT loads all four TAHOT TSVs (Gen-Deu, Jos-Est, Job-Sng, Isa-Mal),
// all under the single "TAHOT" source code.
func loadTAHOT(db *sql.DB, roots []string) (inserted, skipped, untagged int, err error) {
	src, err := sourceByCode("TAHOT")
	if err != nil {
		return 0, 0, 0, err
	}
	paths, err := corpus.Locate(src, roots...)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("locate TAHOT: %w", err)
	}

	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("open %s: %w", path, err)
		}
		n, skip, unt, err := tahot.Load(db, f)
		f.Close()
		if err != nil {
			return 0, 0, 0, fmt.Errorf("%s: %w", path, err)
		}
		inserted += n
		skipped += skip
		untagged += unt
	}
	return inserted, skipped, untagged, nil
}

// loadSweteWords loads the Swete LXX Greek surface-form word stream. The
// "Swete" source_file is a glob over LXX-Swete-1930/*.csv (versification,
// two word variants, transliteration); only the versification file and the
// with-punctuation word file are the ones this loader needs.
func loadSweteWords(db *sql.DB, roots []string) (inserted, skipped int, err error) {
	src, err := sourceByCode("Swete")
	if err != nil {
		return 0, 0, err
	}
	paths, err := corpus.Locate(src, roots...)
	if err != nil {
		return 0, 0, fmt.Errorf("locate Swete: %w", err)
	}

	var versificationPath, wordsPath string
	for _, p := range paths {
		switch filepath.Base(p) {
		case "00-Swete_versification.csv":
			versificationPath = p
		case "01-Swete_word_with_punctuations.csv":
			wordsPath = p
		}
	}
	if versificationPath == "" || wordsPath == "" {
		return 0, 0, fmt.Errorf("Swete: expected versification + with-punctuation word CSVs, found %v", paths)
	}

	vf, err := os.Open(versificationPath)
	if err != nil {
		return 0, 0, fmt.Errorf("open %s: %w", versificationPath, err)
	}
	defer vf.Close()
	wf, err := os.Open(wordsPath)
	if err != nil {
		return 0, 0, fmt.Errorf("open %s: %w", wordsPath, err)
	}
	defer wf.Close()

	return swetewords.Load(db, vf, wf)
}

// loadOSSWords loads every Open Scriptures LxxLemmas/<Book>.js file.
// Multi-recension and deuterocanon/extra-biblical files are reported as
// out-of-scope rows via osswords.Load's bookAlias allow-list, not an error
// (see osswords.bookAlias doc for the full list and rationale).
func loadOSSWords(db *sql.DB, roots []string) (inserted, skippedBook, malformed int, err error) {
	src, err := sourceByCode("OSS-LXX-lemma")
	if err != nil {
		return 0, 0, 0, err
	}
	paths, err := corpus.Locate(src, roots...)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("locate OSS-LXX-lemma: %w", err)
	}

	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("open %s: %w", path, err)
		}
		n, skip, mal, err := osswords.Load(db, f)
		f.Close()
		if err != nil {
			return 0, 0, 0, fmt.Errorf("%s: %w", path, err)
		}
		inserted += n
		skippedBook += skip
		malformed += mal
	}
	return inserted, skippedBook, malformed, nil
}

// loadMorphCodes loads TEGMC (Greek) and TEHMC (Hebrew).
func loadMorphCodes(db *sql.DB, roots []string) (int, error) {
	greek, err := openSource("TEGMC", roots)
	if err != nil {
		return 0, err
	}
	defer greek.Close()
	nGreek, err := morph.Load(db, greek, "grc")
	if err != nil {
		return 0, err
	}

	hebrew, err := openSource("TEHMC", roots)
	if err != nil {
		return 0, err
	}
	defer hebrew.Close()
	nHebrew, err := morph.Load(db, hebrew, "he")
	if err != nil {
		return 0, err
	}

	return nGreek + nHebrew, nil
}
