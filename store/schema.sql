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
    fetch_url    TEXT,                              -- set only for user-fetched (non-shippable) sources
    homepage_url TEXT                               -- human-facing reference link (project site/repo), T36
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

-- Ticket 4: per-edition verse rows + deterministic verse alignment.
-- Canonical versification = KJV/English Protestant, enumerated from KJV.json
-- (versification='canonical'). Each LXX edition (Brenton, Swete, OSS) loads
-- completely into its OWN versification tag (lxx-brenton, lxx-swete,
-- lxx-oss) - never forced onto the canonical spine at load time (invariant
-- #4: never assume 1:1 across editions). verse_alignment (T4b) is the
-- separate, deterministic cross-reference between an edition's verse rows
-- and the canonical rows; this supersedes the original versification_map
-- design, which could only represent a 1:1 remap and not LXX-only content
-- (e.g. Psalm 151), canonical-only content (e.g. verses absent from the
-- shorter LXX Jeremiah), or genuine merges/divides.

CREATE TABLE IF NOT EXISTS verses (
    id             INTEGER PRIMARY KEY,
    versification  TEXT    NOT NULL DEFAULT 'canonical', -- canonical | lxx-brenton | lxx-swete | lxx-oss
    book_id        INTEGER NOT NULL REFERENCES books(id),
    chapter        INTEGER NOT NULL,
    verse          INTEGER NOT NULL,
    UNIQUE (versification, book_id, chapter, verse)
);

CREATE INDEX IF NOT EXISTS idx_verses_book ON verses(book_id);

-- T4b: typed many-to-many alignment between an edition's own verse rows and
-- the canonical rows. relation in {exact, renumber, merge, divide, title,
-- moved}; group_id ties the members of an n:1 merge or 1:n divide. Absence
-- of an alignment row is itself data - LXX-only and canonical-only content
-- are not skips or bugs, they simply have no counterpart to align to.

CREATE TABLE IF NOT EXISTS verse_alignment (
    id                  INTEGER PRIMARY KEY,
    canonical_verse_id  INTEGER NOT NULL REFERENCES verses(id),
    edition_verse_id    INTEGER NOT NULL REFERENCES verses(id),
    relation            TEXT    NOT NULL,   -- exact | renumber | merge | divide | title | moved
    group_id            INTEGER,            -- ties members of a merge/divide group
    confidence          REAL    NOT NULL,
    source_id           INTEGER NOT NULL REFERENCES sources(id)
);

CREATE INDEX IF NOT EXISTS idx_verse_alignment_canonical ON verse_alignment(canonical_verse_id);
CREATE INDEX IF NOT EXISTS idx_verse_alignment_edition ON verse_alignment(edition_verse_id);

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

-- Ticket 7: verbatim per-edition English prose, FK to verses + sources.
-- native_ref is the human-readable reference as the edition itself would
-- cite it (book name + chapter:verse), independent of the internal verse_id.

CREATE TABLE IF NOT EXISTS verse_text (
    id        INTEGER PRIMARY KEY,
    verse_id  INTEGER NOT NULL REFERENCES verses(id),
    source_id INTEGER NOT NULL REFERENCES sources(id),
    native_ref TEXT   NOT NULL,
    text      TEXT    NOT NULL,
    UNIQUE (verse_id, source_id)
);

CREATE INDEX IF NOT EXISTS idx_verse_text_verse ON verse_text(verse_id);

-- Ticket 10: tagged original-language words, one row per word instance per
-- source (TAGNT Greek NT; TAHOT Hebrew OT and the LXX word streams follow
-- in T11-T13). source_locator (the source file's own ref, e.g.
-- "Mat.1.1#01=NKO") is the row key, NOT (verse_id, source_id, word_no) -
-- variant readings legitimately share a word_no. dstrong/morph_code are
-- plain TEXT, not hard FKs: STEPBible's TAGNT and TBESG/morph-code files
-- have a small number of known cross-file gaps (confirmed: 5 of 5,575
-- distinct TAGNT dStrongs are absent from TBESG), and a compound-tagged
-- word (one surface token spanning two Strong's numbers, eg μήποτε = μή +
-- ποτε) has no single dStrong to store - both are left NULL rather than
-- failing the whole load or guessing. The Strong's/morph bridge is a
-- join-time lookup (invariant #5), not a load-time constraint.

CREATE TABLE IF NOT EXISTS words (
    id             INTEGER PRIMARY KEY,
    verse_id       INTEGER NOT NULL REFERENCES verses(id),
    source_id      INTEGER NOT NULL REFERENCES sources(id),
    word_no        INTEGER NOT NULL,
    surface        TEXT,
    lemma          TEXT,
    dstrong        TEXT,
    morph_code     TEXT,
    attestation    TEXT    NOT NULL,   -- N/K/O Type from the ref, eg "NKO", "K", "N(k)O"
    editions       TEXT    NOT NULL,   -- eg "NA28+NA27+Tyn+SBL+WH+Treg+TR+Byz"
    source_locator TEXT    NOT NULL UNIQUE,
    translit       TEXT                -- per-occurrence transliteration (T32); NULL where the
                                        -- source has none (Swete, OSS-LXX-lemma)
);

CREATE INDEX IF NOT EXISTS idx_words_verse ON words(verse_id);
CREATE INDEX IF NOT EXISTS idx_words_dstrong ON words(dstrong);
