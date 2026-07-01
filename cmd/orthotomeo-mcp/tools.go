// Tool registration for the orthotomeo MCP server (T20). Every tool is a
// direct, typed delegation to one engine.Engine method - no tool builds
// SQL, and no tool does anything an engine caller couldn't already do
// (Concord spec: "the MCP server is the engine; the LLM client is the
// analysis layer"). mcp.AddTool's generic handler already validates input
// against the inferred schema and marshals a non-nil Out value as both
// StructuredContent and human-readable JSON text, so every handler here is
// just "call the engine method, return its result" - no bespoke response
// building.
package main

import (
	"context"
	"fmt"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jrainsberger/orthotomeo/cite"
	"github.com/jrainsberger/orthotomeo/concord"
	"github.com/jrainsberger/orthotomeo/engine"
	"github.com/jrainsberger/orthotomeo/retriever"
)

// schemaFor computes T's JSON schema via reflection, then simplifies any
// nullable-array union (jsonschema-go's default "type": ["null", "array"]
// for a Go slice field, so a nil slice validates too) down to plain
// "array". A required slice field never needs to also accept a literal
// JSON null - "required" already demands the property be present with a
// real value - and at least one real-world MCP client doesn't parse a
// "type" that's an array of strings, silently treating the property as
// untyped and rejecting a real array argument (found live: get_verse,
// get_passage, concord_phrase, and cite all take a required slice
// argument and were all affected). Panics on a reflection error, since
// that would mean an arg struct contains a type jsonschema-go can't
// represent at all - a build-time-verifiable condition, not a runtime one.
func schemaFor[T any]() *jsonschema.Schema {
	s, err := jsonschema.ForType(reflect.TypeFor[T](), &jsonschema.ForOptions{})
	if err != nil {
		panic(fmt.Sprintf("schemaFor[%T]: %v", *new(T), err))
	}
	simplifyNullableArrays(s)
	return s
}

func simplifyNullableArrays(s *jsonschema.Schema) {
	if s == nil {
		return
	}
	if len(s.Types) > 0 {
		for _, t := range s.Types {
			if t != "null" {
				s.Type = t
				break
			}
		}
		s.Types = nil
	}
	simplifyNullableArrays(s.Items)
	for _, p := range s.Properties {
		simplifyNullableArrays(p)
	}
}

func ref(book string, chapter, verse int) retriever.Ref {
	return retriever.Ref{Book: book, Chapter: chapter, Verse: verse}
}

type refArgs struct {
	Book    string `json:"book" jsonschema:"canonical USFM book code, e.g. GEN, PSA, MAT, REV"`
	Chapter int    `json:"chapter" jsonschema:"chapter number"`
	Verse   int    `json:"verse" jsonschema:"verse number"`
}

type getVerseArgs struct {
	Book     string   `json:"book" jsonschema:"canonical USFM book code, e.g. GEN, PSA, MAT, REV"`
	Chapter  int      `json:"chapter" jsonschema:"chapter number"`
	Verse    int      `json:"verse" jsonschema:"verse number"`
	Editions []string `json:"editions" jsonschema:"verse-text editions to fetch verbatim text from: KJV, ASV, WEB, Brenton"`
}

type getPassageArgs struct {
	Book         string   `json:"book" jsonschema:"canonical USFM book code, e.g. GEN, PSA, MAT, REV"`
	StartChapter int      `json:"start_chapter" jsonschema:"first chapter of the range, inclusive"`
	StartVerse   int      `json:"start_verse" jsonschema:"first verse of the range, inclusive"`
	EndChapter   int      `json:"end_chapter" jsonschema:"last chapter of the range, inclusive"`
	EndVerse     int      `json:"end_verse" jsonschema:"last verse of the range, inclusive"`
	Editions     []string `json:"editions" jsonschema:"verse-text editions to fetch verbatim text from: KJV, ASV, WEB, Brenton"`
}

type concordLemmaArgs struct {
	Query  string `json:"query" jsonschema:"a lemma (e.g. ἄφεσις) or a disambiguated Strong's number (e.g. G0859, H7225G)"`
	Corpus string `json:"corpus" jsonschema:"word-tagged corpus to search: TAGNT (Greek NT), TAHOT (Hebrew OT), Swete (LXX surface), OSS-LXX-lemma (LXX lemma)"`
}

type concordPhraseArgs struct {
	Tokens []string `json:"tokens" jsonschema:"ordered lemma strings to find co-occurring within one verse, e.g. [\"εἰς\",\"ἄφεσις\"]"`
	Corpus string   `json:"corpus" jsonschema:"word-tagged corpus to search: TAGNT, TAHOT, Swete, OSS-LXX-lemma"`
	Window int      `json:"window" jsonschema:"max words allowed between consecutive tokens; 0 = strictly adjacent"`
}

type countArgs struct {
	Query  string `json:"query" jsonschema:"a lemma or a disambiguated Strong's number, same as concord_lemma"`
	Corpus string `json:"corpus" jsonschema:"word-tagged corpus: TAGNT, TAHOT, Swete, OSS-LXX-lemma"`
}

type wordScopedArgs struct {
	Book    string `json:"book" jsonschema:"canonical USFM book code, e.g. GEN, PSA, MAT, REV"`
	Chapter int    `json:"chapter" jsonschema:"chapter number"`
	Verse   int    `json:"verse" jsonschema:"verse number"`
	Word    *int   `json:"word,omitempty" jsonschema:"1-based word_no within the verse; omit for every word in the verse"`
	Corpus  string `json:"corpus" jsonschema:"word-tagged corpus: TAGNT, TAHOT, Swete, OSS-LXX-lemma"`
}

type lemmatizeArgs struct {
	Book    string `json:"book" jsonschema:"canonical USFM book code, e.g. GEN, PSA, MAT, REV"`
	Chapter int    `json:"chapter" jsonschema:"chapter number"`
	Verse   int    `json:"verse" jsonschema:"verse number"`
	Corpus  string `json:"corpus" jsonschema:"word-tagged corpus: TAGNT, TAHOT, Swete, OSS-LXX-lemma"`
}

type citeArgs struct {
	Citations []retriever.Citation `json:"citations" jsonschema:"Citations previously returned by another tool call, to render as a pastable reference block"`
}

type citeResult struct {
	Text string `json:"text"`
}

// citationsResult wraps []retriever.Citation for tools that return it: the
// MCP spec requires an object-typed output schema, and a bare JSON array
// isn't one - every tool below that hands back a Citation slice wraps it
// in this one field instead of inventing a bespoke wrapper each time.
type citationsResult struct {
	Citations []retriever.Citation `json:"citations"`
}

// registerTools wires every engine.Engine method to a typed MCP tool. Each
// handler returns (nil, out, err): mcp.AddTool's ToolHandlerFor populates
// CallToolResult.Content/StructuredContent from out automatically, and
// wraps a returned err as a tool-level error (invariant #3's "raise, don't
// silently truncate" reaching the MCP boundary unchanged).
func registerTools(s *mcp.Server, e *engine.Engine) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "resolve_ref",
		Description: "Reports, for every per-verse content edition (KJV, ASV, WEB, Brenton, TAGNT, TAHOT, Swete, OSS-LXX-lemma), " +
			"whether a canonical reference has a counterpart there and where. Cross-edition divergence (a T4b merge/renumber/divide, " +
			"or a reference simply missing from an edition) is reported as Caveats - never a silent shift.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in refArgs) (*mcp.CallToolResult, retriever.Resolution, error) {
		res, err := e.ResolveRef(ref(in.Book, in.Chapter, in.Verse))
		return nil, res, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_verse",
		Description: "Returns verbatim verse text with provenance for one canonical reference, one Citation per requested edition (KJV, ASV, WEB, Brenton).",
		InputSchema: schemaFor[getVerseArgs](),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in getVerseArgs) (*mcp.CallToolResult, citationsResult, error) {
		cs, err := e.GetVerse(ref(in.Book, in.Chapter, in.Verse), in.Editions)
		return nil, citationsResult{Citations: cs}, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_passage",
		Description: "Returns get_verse's result for every canonical verse in a contiguous, single-book range, in order - verse boundaries preserved, never concatenated into one blob.",
		InputSchema: schemaFor[getPassageArgs](),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in getPassageArgs) (*mcp.CallToolResult, citationsResult, error) {
		rr := retriever.RefRange{
			Start: ref(in.Book, in.StartChapter, in.StartVerse),
			End:   ref(in.Book, in.EndChapter, in.EndVerse),
		}
		cs, err := e.GetPassage(rr, in.Editions)
		return nil, citationsResult{Citations: cs}, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "concord_lemma",
		Description: "Complete-or-fail concordance: every words row in corpus whose lemma or dStrong matches query. " +
			"Route lemma/Strong's-number lookups here, never by writing SQL or guessing occurrences from memory.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in concordLemmaArgs) (*mcp.CallToolResult, citationsResult, error) {
		cs, err := e.ConcordLemma(in.Query, in.Corpus)
		return nil, citationsResult{Citations: cs}, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "concord_phrase",
		Description: "Complete-or-fail multi-word concordance: every occurrence, within one verse, of tokens (lemma strings) " +
			"appearing in order within window intervening words of each other (window=0 = strictly adjacent). " +
			"This is the tool for a phrase query like εἰς ἄφεσιν.",
		InputSchema: schemaFor[concordPhraseArgs](),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in concordPhraseArgs) (*mcp.CallToolResult, citationsResult, error) {
		cs, err := e.ConcordPhrase(in.Tokens, in.Corpus, in.Window)
		return nil, citationsResult{Citations: cs}, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "count",
		Description: "Occurrence tally (total + per-book breakdown) for the identical query concord_lemma would match. count.Total always equals len(concord_lemma(...)) - use this to sanity-check a concordance result, or when only the number matters.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in countArgs) (*mcp.CallToolResult, concord.Tally, error) {
		t, err := e.Count(in.Query, in.Corpus)
		return nil, t, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "parse",
		Description: "Returns dStrong + expanded morphology (via the T6 morph_codes table) for every word in a verse, or one word if word is given. LXX corpora (Swete, OSS-LXX-lemma) are always Flagged - neither carries morphology.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in wordScopedArgs) (*mcp.CallToolResult, citationsResult, error) {
		cs, err := parseTool(e, in)
		return nil, citationsResult{Citations: cs}, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "lemmatize",
		Description: "Returns the ordered lemma list for a verse (words with no lemma are omitted, not fabricated).",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in lemmatizeArgs) (*mcp.CallToolResult, citationsResult, error) {
		cs, err := e.Lemmatize(ref(in.Book, in.Chapter, in.Verse), in.Corpus)
		return nil, citationsResult{Citations: cs}, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "attestation",
		Description: "Returns the WHNT-style Type/Editions manuscript-tradition columns as neutral text-critical data " +
			"(e.g. Mark 16:9-20 = Type KO) - which editions carry a word, with no argument for or against a variant.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in wordScopedArgs) (*mcp.CallToolResult, citationsResult, error) {
		cs, err := attestationTool(e, in)
		return nil, citationsResult{Citations: cs}, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "cite",
		Description: "Renders Citations (from any of the above tools) as quoted, fully-attributed Markdown bullets - the only sanctioned bridge from a query result to pastable study-document text.",
		InputSchema: schemaFor[citeArgs](),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in citeArgs) (*mcp.CallToolResult, citeResult, error) {
		return nil, citeResult{Text: cite.Cite(in.Citations)}, nil
	})
}

// parseTool/attestationTool exist only to turn wordScopedArgs' *int Word
// into the pointer engine.Parse/Attestation expect, with a small guard so
// an obviously-wrong (non-positive) word number fails loudly instead of
// silently matching nothing.
func parseTool(e *engine.Engine, in wordScopedArgs) ([]retriever.Citation, error) {
	if in.Word != nil && *in.Word < 1 {
		return nil, fmt.Errorf("word must be >= 1 (1-based), got %d", *in.Word)
	}
	return e.Parse(ref(in.Book, in.Chapter, in.Verse), in.Word, in.Corpus)
}

func attestationTool(e *engine.Engine, in wordScopedArgs) ([]retriever.Citation, error) {
	if in.Word != nil && *in.Word < 1 {
		return nil, fmt.Errorf("word must be >= 1 (1-based), got %d", *in.Word)
	}
	return e.Attestation(ref(in.Book, in.Chapter, in.Verse), in.Word, in.Corpus)
}
