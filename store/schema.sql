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

-- Ticket 4: canonical verse spine + versification divergence map.
-- Canonical versification = KJV/English Protestant, enumerated from KJV.json.
-- versification_map holds only the rows where an edition's native ref differs
-- from canonical (populated by the LXX loaders via TVTMS; T4b).

CREATE TABLE IF NOT EXISTS verses (
    id       INTEGER PRIMARY KEY,
    book_id  INTEGER NOT NULL REFERENCES books(id),
    chapter  INTEGER NOT NULL,
    verse    INTEGER NOT NULL,
    UNIQUE (book_id, chapter, verse)
);

CREATE INDEX IF NOT EXISTS idx_verses_book ON verses(book_id);

CREATE TABLE IF NOT EXISTS versification_map (
    id              INTEGER PRIMARY KEY,
    source_id       INTEGER NOT NULL REFERENCES sources(id),
    native_book     TEXT    NOT NULL,           -- book code in that source's scheme
    native_chapter  INTEGER NOT NULL,
    native_verse    INTEGER NOT NULL,
    verse_id        INTEGER NOT NULL REFERENCES verses(id),
    UNIQUE (source_id, native_book, native_chapter, native_verse)
);

-- Ticket 21: deterministic cross-references (OpenBible.info / TSK, CC-BY).
-- to_verse_end is non-null only for ranged targets (e.g. Col.1.16-Col.1.17).
-- votes is the signed community weight (negatives = disputed; kept as data).

CREATE TABLE IF NOT EXISTS cross_references (
    id            INTEGER PRIMARY KEY,
    from_verse    INTEGER NOT NULL REFERENCES verses(id),
    to_verse      INTEGER NOT NULL REFERENCES verses(id),
    to_verse_end  INTEGER REFERENCES verses(id),   -- nullable: ranged target
    votes         INTEGER NOT NULL,
    kind          TEXT    NOT NULL DEFAULT 'thematic',
    source_id     INTEGER NOT NULL REFERENCES sources(id)
);

CREATE INDEX IF NOT EXISTS idx_xref_from ON cross_references(from_verse);

-- Ticket 5: Strong's lemma/definition dictionary (TBESG + TBESH), the bridge
-- words.dstrong joins to. ustrong is a self-reference column enabling the
-- deterministic synonym layer later (not collapsed here).

CREATE TABLE IF NOT EXISTS lexicon (
    dstrong     TEXT PRIMARY KEY,
    estrong     TEXT NOT NULL,
    ustrong     TEXT NOT NULL,
    language    TEXT NOT NULL,            -- grc | he
    lemma       TEXT NOT NULL,
    translit    TEXT NOT NULL,
    gloss       TEXT NOT NULL,
    definition  TEXT NOT NULL,
    def_license TEXT NOT NULL
);

-- Ticket 6: human-readable expansions of the morphology codes used in the
-- tagged texts (words.morph_code), from TEGMC (Greek) + TEHMC (Hebrew).

CREATE TABLE IF NOT EXISTS morph_codes (
    code        TEXT PRIMARY KEY,
    language    TEXT NOT NULL,            -- grc | he
    description TEXT NOT NULL
);
