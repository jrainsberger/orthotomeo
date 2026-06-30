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
