-- orthotomeo derived SQLite schema.
-- Source of truth = the corpus files; this DB is a build artifact (see docs/erd-v1.svg).
-- Tables are added per import ticket. Ticket 1: sources (provenance registry).

CREATE TABLE IF NOT EXISTS sources (
    id           INTEGER PRIMARY KEY,
    code         TEXT    NOT NULL UNIQUE,           -- KJV, TAGNT, Swete, ...
    full_name    TEXT    NOT NULL,
    language     TEXT,                              -- en | grc | he | he+arc | mul
    type         TEXT    NOT NULL,                  -- translation|original|lemma|lexicon|morph-codes|versification
    license      TEXT    NOT NULL,
    attribution  TEXT,
    source_file  TEXT,                              -- logical/relative path, not an absolute machine path
    format       TEXT,                              -- json|usfm|html|tsv|csv|js|txt
    shippable    INTEGER NOT NULL DEFAULT 1,        -- 0/1: redistributable in the open-source release
    fetch_url    TEXT                               -- set only for user-fetched (non-shippable) sources
);

-- Ticket 2: canonical book registry + per-scheme name aliases.
-- Every verse/text load resolves a native book name to a books.id through
-- book_names. Schemes are shared across sources (OSIS is used by the xref file
-- and others), so aliases key on (scheme, value), not on a single source.

CREATE TABLE IF NOT EXISTS books (
    id          INTEGER PRIMARY KEY,
    code        TEXT    NOT NULL UNIQUE,            -- canonical USFM code: GEN, MRK, REV
    full_name   TEXT    NOT NULL,                   -- English name (KJV/ASV form)
    section     TEXT    NOT NULL,                   -- ot | nt
    canon       TEXT    NOT NULL,                   -- protestant (deuterocanon added later)
    sort_order  INTEGER NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS book_names (
    book_id     INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    scheme      TEXT    NOT NULL,                   -- usfm | osis | dotted | name-en
    value       TEXT    NOT NULL,
    PRIMARY KEY (scheme, value)                     -- a (scheme,value) resolves to exactly one book
);

CREATE INDEX IF NOT EXISTS idx_book_names_book ON book_names(book_id);
