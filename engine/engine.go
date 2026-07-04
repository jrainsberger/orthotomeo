// Package engine is the single read-only seam every transport (MCP, CLI,
// HTTP, desktop) imports. It owns the DB connection and delegates 1:1 to
// Phase 5 (retriever/concord/parse/attestation/cite); no transport ever
// sees *sql.DB or imports database/sql itself. A transport that needs SQL
// is a design failure, not a feature (Concord spec §4, invariant #2:
// provenance and completeness enforced in exactly one place). Ticket 25.
package engine

import (
	"database/sql"
	"fmt"

	"github.com/jrainsberger/orthotomeo/attestation"
	"github.com/jrainsberger/orthotomeo/books"
	"github.com/jrainsberger/orthotomeo/cite"
	"github.com/jrainsberger/orthotomeo/concord"
	"github.com/jrainsberger/orthotomeo/interlinear"
	"github.com/jrainsberger/orthotomeo/lexicon"
	"github.com/jrainsberger/orthotomeo/parse"
	"github.com/jrainsberger/orthotomeo/retriever"
	"github.com/jrainsberger/orthotomeo/store"

	_ "modernc.org/sqlite"
)

// Engine holds the one DB connection every method below delegates through.
// The field is unexported: no transport can reach into it for a *sql.DB.
type Engine struct {
	db *sql.DB
}

// Open opens the built DB at dbPath READ-ONLY, in two independent layers:
// the SQLite URI "mode=ro" parameter refuses the connection outright if
// the file needs write access, and "PRAGMA query_only = ON" is a second,
// statement-level guard - either alone would already stop a write
// (defense in depth, not redundancy for its own sake). dbPath must already
// exist (cmd/build produces it); Open never creates or migrates a schema.
func Open(dbPath string) (*Engine, error) {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", dbPath, err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA query_only = ON;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("set query_only: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping %s: %w", dbPath, err)
	}
	var version int
	if err := db.QueryRow(`PRAGMA user_version;`).Scan(&version); err != nil {
		db.Close()
		return nil, fmt.Errorf("read schema version: %w", err)
	}
	if version < store.SchemaVersion {
		db.Close()
		return nil, fmt.Errorf(
			"%s was built against an older schema (version %d, want %d) - "+
				"delete it and rebuild via cmd/build (see README.md)",
			dbPath, version, store.SchemaVersion)
	}
	return &Engine{db: db}, nil
}

// Close releases the underlying connection.
func (e *Engine) Close() error { return e.db.Close() }

// --- Book-name normalization ---

// ResolveBookCode turns free-form book input (a USFM code or the full
// English name, any case) into the canonical USFM code retriever.Ref.Book
// requires. Every transport calls this before constructing a Ref, so book
// matching lives in exactly one place.
func (e *Engine) ResolveBookCode(raw string) (string, error) {
	return books.ResolveCode(e.db, raw)
}

// --- T15: Citation + reference resolution ---

// ResolveRef reports, for every per-verse content edition, whether ref has
// a counterpart there and where.
func (e *Engine) ResolveRef(ref retriever.Ref) (retriever.Resolution, error) {
	return retriever.ResolveRef(e.db, ref)
}

// GetVerse returns verbatim text with provenance for ref, one Citation per
// requested edition.
func (e *Engine) GetVerse(ref retriever.Ref, editions []string) ([]retriever.Citation, error) {
	return retriever.GetVerse(e.db, ref, editions)
}

// GetPassage returns GetVerse's result for every canonical verse in rr, in
// order, verse boundaries preserved.
func (e *Engine) GetPassage(rr retriever.RefRange, editions []string) ([]retriever.Citation, error) {
	return retriever.GetPassage(e.db, rr, editions)
}

// --- T16: concordance ---

// ConcordLemma returns every words row in corpus whose lemma, dStrong, or
// surface form matches query - by picks the column explicitly ("lemma",
// "dstrong", "surface"), or "" for the original auto-detect (dStrong shape,
// else lemma) - complete or an error.
func (e *Engine) ConcordLemma(query, corpus, by string) ([]retriever.Citation, error) {
	return concord.ConcordLemma(e.db, query, corpus, by)
}

// ConcordPhrase finds every occurrence, within one verse, of tokens
// appearing in order with at most window intervening words between each
// consecutive pair (window=0 = strictly adjacent).
func (e *Engine) ConcordPhrase(tokens []string, corpus string, window int) ([]retriever.Citation, error) {
	return concord.ConcordPhrase(e.db, tokens, corpus, window)
}

// Count returns the occurrence tally for the same query ConcordLemma would
// match - Count(q, c, by).Total == len(ConcordLemma(q, c, by)) always.
func (e *Engine) Count(query, corpus, by string) (concord.Tally, error) {
	return concord.Count(e.db, query, corpus, by)
}

// --- T17: parse / lemmatize ---

// Parse returns dStrong + expanded morphology for corpus's words at ref -
// every word, or a single word if word is non-nil (1-based word_no).
func (e *Engine) Parse(ref retriever.Ref, word *int, corpus string) ([]retriever.Citation, error) {
	return parse.Parse(e.db, ref, word, corpus)
}

// Lemmatize returns the ordered lemma list for ref in corpus.
func (e *Engine) Lemmatize(ref retriever.Ref, corpus string) ([]retriever.Citation, error) {
	return parse.Lemmatize(e.db, ref, corpus)
}

// Interlinear composes T35's row-aligned original/transliteration/gloss/
// grammar display over a Parse result - a display shape, not a new query:
// every field comes from Parse (T17/T32) or lexicon.Lookup (T34). Also
// returns the T31 per-edition sources map for the underlying Citations,
// same as every other Citation-bearing method's transport wrapper does -
// computed here (not by asking the caller to re-run Parse) since Build
// consumes the Citations directly.
func (e *Engine) Interlinear(ref retriever.Ref, word *int, corpus string) ([]interlinear.Word, map[string]retriever.SourceInfo, error) {
	cs, err := parse.Parse(e.db, ref, word, corpus)
	if err != nil {
		return nil, nil, err
	}
	words, err := interlinear.Build(e.db, cs)
	if err != nil {
		return nil, nil, err
	}
	srcs, err := retriever.SourcesFor(cs)
	if err != nil {
		return nil, nil, err
	}
	return words, srcs, nil
}

// --- T18: attestation ---

// Attestation returns corpus's Type/Editions manuscript-tradition data for
// ref, as neutral text-critical data.
func (e *Engine) Attestation(ref retriever.Ref, word *int, corpus string) ([]retriever.Citation, error) {
	return attestation.Attestation(e.db, ref, word, corpus)
}

// --- T19: Cite renderer ---

// Cite renders citations as quoted, fully-attributed Markdown bullets -
// pure string formatting, no DB access, exposed here so a transport never
// needs its own import of the cite package either.
func (e *Engine) Cite(citations []retriever.Citation) string {
	return cite.Cite(citations)
}

// --- T34: lexicon / Strong's lookup ---

// Lookup resolves dstrong to its lexicon entry - gloss and translit always,
// definition only for a Greek row (a Hebrew row's definition is withheld
// pending permission - lexicon.Entry doc comment, T34).
func (e *Engine) Lookup(dstrong string) (lexicon.Entry, error) {
	return lexicon.Lookup(e.db, dstrong)
}
