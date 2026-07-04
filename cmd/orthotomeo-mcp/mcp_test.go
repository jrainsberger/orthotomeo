package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jrainsberger/orthotomeo/books"
	"github.com/jrainsberger/orthotomeo/engine"
	"github.com/jrainsberger/orthotomeo/retriever"
	"github.com/jrainsberger/orthotomeo/sources"
	"github.com/jrainsberger/orthotomeo/store"
)

// buildFixture writes a small real DB file covering Mat.26.28's real shape
// (G0859/ἄφεσις adjacent to εἰς) so a genuine end-to-end MCP round trip
// (client -> in-memory transport -> server -> tool -> engine -> SQLite ->
// back) can be proven, not just the tool-handler Go functions in isolation.
func buildFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("build open: %v", err)
	}
	if err := store.ApplySchema(db); err != nil {
		t.Fatalf("schema: %v", err)
	}
	if _, err := sources.Seed(db); err != nil {
		t.Fatalf("seed sources: %v", err)
	}
	if _, _, err := books.Seed(db); err != nil {
		t.Fatalf("seed books: %v", err)
	}

	var matBook int64
	if err := db.QueryRow(`SELECT id FROM books WHERE code = 'MAT'`).Scan(&matBook); err != nil {
		t.Fatalf("book lookup: %v", err)
	}
	res, err := db.Exec(`INSERT INTO verses (versification, book_id, chapter, verse) VALUES ('canonical', ?, 26, 28)`, matBook)
	if err != nil {
		t.Fatalf("insert verse: %v", err)
	}
	verseID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}

	insertWord := func(wordNo int, surface, lemma, dstrong, morphCode string) {
		if _, err := db.Exec(`
			INSERT INTO words (verse_id, source_id, word_no, surface, lemma, dstrong, morph_code, attestation, editions, source_locator)
			VALUES (?, (SELECT id FROM sources WHERE code = 'TAGNT'), ?, ?, ?, ?, ?, 'NKO', 'NA28+TR+Byz', ?)`,
			verseID, wordNo, surface, lemma, dstrong, morphCode, "Mat.26.28#"+strconv.Itoa(wordNo)); err != nil {
			t.Fatalf("insert word: %v", err)
		}
	}
	insertWord(1, "εἰς", "εἰς", "G1519", "PREP")
	insertWord(2, "ἄφεσιν", "ἄφεσις", "G0859", "N-ASF")

	if _, err := db.Exec(`
		INSERT INTO lexicon (dstrong, estrong, ustrong, language, lemma, translit, gloss, definition, def_license)
		VALUES ('G0859', 'G0859', 'G0859', 'grc', 'ἄφεσις', 'aphesis', 'forgiveness', 'release, pardon', 'Abbott-Smith PD')`); err != nil {
		t.Fatalf("seed lexicon: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO verse_text (verse_id, source_id, native_ref, text)
		VALUES (?, (SELECT id FROM sources WHERE code = 'KJV'), 'Mat.26.28', 'blood of the new testament')`, verseID); err != nil {
		t.Fatalf("insert verse_text: %v", err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close build handle: %v", err)
	}
	return path
}

// startTestServer wires a real server (this package's registerTools) to a
// real client over the SDK's in-memory transport pair - a genuine MCP
// session, not a bypassed direct function call.
func startTestServer(t *testing.T, dbPath string) *mcp.ClientSession {
	t.Helper()
	e, err := engine.Open(dbPath)
	if err != nil {
		t.Fatalf("engine.Open: %v", err)
	}
	t.Cleanup(func() { e.Close() })

	server := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "0.0.0"}, nil)
	registerTools(server, e)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		if err := server.Run(ctx, serverTransport); err != nil && ctx.Err() == nil {
			t.Errorf("server.Run: %v", err)
		}
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

// callTool issues a real tools/call over the session and unmarshals the
// tool's JSON text content into Out - the same JSON a real MCP client
// (e.g. Claude) would receive.
func callTool[Out any](t *testing.T, session *mcp.ClientSession, name string, args any) Out {
	t.Helper()
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("tool %s returned an error result: %+v", name, res.Content)
	}
	if len(res.Content) == 0 {
		t.Fatalf("tool %s returned no content", name)
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("tool %s content[0] is %T, want *mcp.TextContent", name, res.Content[0])
	}
	var out Out
	if err := json.Unmarshal([]byte(text.Text), &out); err != nil {
		t.Fatalf("unmarshal %s result %q: %v", name, text.Text, err)
	}
	return out
}

func TestConcordLemmaOverMCP(t *testing.T) {
	session := startTestServer(t, buildFixture(t))
	res := callTool[citationsResult](t, session, "concord_lemma", map[string]any{"query": "G0859", "corpus": "TAGNT"})
	cs := res.Citations
	if len(cs) != 1 {
		t.Fatalf("citations = %d, want 1", len(cs))
	}
	if cs[0].Ref.Book != "MAT" || cs[0].Ref.Chapter != 26 || cs[0].Ref.Verse != 28 {
		t.Errorf("ref = %+v, want MAT 26:28", cs[0].Ref)
	}
	if cs[0].DStrong != "G0859" {
		t.Errorf("dstrong = %q, want G0859", cs[0].DStrong)
	}
}

func TestCountAgreesWithConcordLemmaOverMCP(t *testing.T) {
	session := startTestServer(t, buildFixture(t))
	res := callTool[citationsResult](t, session, "concord_lemma", map[string]any{"query": "G0859", "corpus": "TAGNT"})
	tally := callTool[struct {
		Total  int            `json:"total"`
		ByBook map[string]int `json:"by_book"`
	}](t, session, "count", map[string]any{"query": "G0859", "corpus": "TAGNT"})
	if tally.Total != len(res.Citations) {
		t.Errorf("count.Total = %d, len(concord_lemma) = %d - must agree across the MCP boundary", tally.Total, len(res.Citations))
	}
}

func TestConcordPhraseOverMCP(t *testing.T) {
	session := startTestServer(t, buildFixture(t))
	res := callTool[citationsResult](t, session, "concord_phrase", map[string]any{
		"tokens": []string{"εἰς", "ἄφεσις"}, "corpus": "TAGNT", "window": 0,
	})
	cs := res.Citations
	if len(cs) != 1 {
		t.Fatalf("citations = %d, want 1", len(cs))
	}
	if cs[0].Text != "εἰς ἄφεσιν" {
		t.Errorf("text = %q, want the verbatim two-word span", cs[0].Text)
	}
}

func TestGetVerseOverMCP(t *testing.T) {
	session := startTestServer(t, buildFixture(t))
	res := callTool[citationsResult](t, session, "get_verse", map[string]any{
		"book": "MAT", "chapter": 26, "verse": 28, "editions": []string{"KJV"},
	})
	cs := res.Citations
	if len(cs) != 1 || cs[0].Text != "blood of the new testament" {
		t.Fatalf("get_verse result = %+v", cs)
	}
}

func TestResolveRefOverMCP(t *testing.T) {
	session := startTestServer(t, buildFixture(t))
	res := callTool[retriever.Resolution](t, session, "resolve_ref", map[string]any{"book": "MAT", "chapter": 26, "verse": 28})
	if res.Ref.Book != "MAT" {
		t.Errorf("ref.book = %q, want MAT", res.Ref.Book)
	}
	found := false
	for _, a := range res.Addresses {
		if a.Edition == "TAGNT" && a.Exists {
			found = true
		}
	}
	if !found {
		t.Errorf("expected TAGNT address to exist, got %+v", res.Addresses)
	}
}

func TestCiteChainedFromConcordLemmaOverMCP(t *testing.T) {
	session := startTestServer(t, buildFixture(t))
	res := callTool[citationsResult](t, session, "concord_lemma", map[string]any{"query": "G0859", "corpus": "TAGNT"})

	rendered := callTool[citeResult](t, session, "cite", map[string]any{"citations": res.Citations})

	if rendered.Text == "" {
		t.Fatal("cite returned empty text for a non-empty concord_lemma result")
	}
	for _, want := range []string{"MAT.26.28", "TAGNT", "ἄφεσιν"} {
		if !strings.Contains(rendered.Text, want) {
			t.Errorf("cite text missing %q: %q", want, rendered.Text)
		}
	}
}

// TestArraySchemaFieldsAdvertiseArrayNotUnionType is a regression test for a
// real bug found in production use: jsonschema-go's default schema for a Go
// []string/[]T field is a nullable union ("type": ["null", "array"]), which
// at least one real MCP client doesn't parse - it silently treats the
// property as untyped and rejects a real array argument, with no error at
// registration time to catch it. This asserts what a client actually
// receives over ListTools (not just that our own tolerant test client can
// still round-trip requests) for every tool that takes a required slice
// argument: "type" must be the plain string "array", never an array of
// strings, and "items" must be present.
func TestArraySchemaFieldsAdvertiseArrayNotUnionType(t *testing.T) {
	session := startTestServer(t, buildFixture(t))
	res, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	cases := map[string]string{ // tool name -> the slice-typed property to check
		"get_verse":      "editions",
		"get_passage":    "editions",
		"concord_phrase": "tokens",
		"cite":           "citations",
	}
	checked := map[string]bool{}
	for _, tool := range res.Tools {
		propName, want := cases[tool.Name]
		if !want {
			continue
		}
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Errorf("%s: InputSchema is %T, want map[string]any", tool.Name, tool.InputSchema)
			continue
		}
		props, _ := schema["properties"].(map[string]any)
		prop, _ := props[propName].(map[string]any)
		if prop == nil {
			t.Errorf("%s: no %q property in schema %+v", tool.Name, propName, schema)
			continue
		}
		if _, isString := prop["type"].(string); !isString {
			t.Errorf("%s.%s: type = %#v (%T), want the plain string \"array\" - a union type here is exactly what broke a real client", tool.Name, propName, prop["type"], prop["type"])
		} else if prop["type"] != "array" {
			t.Errorf("%s.%s: type = %q, want \"array\"", tool.Name, propName, prop["type"])
		}
		if prop["items"] == nil {
			t.Errorf("%s.%s: missing \"items\"", tool.Name, propName)
		}
		checked[tool.Name] = true
	}
	for name := range cases {
		if !checked[name] {
			t.Errorf("tool %q not found in tools/list response", name)
		}
	}
}

func TestParseRejectsInvalidWordNumberOverMCP(t *testing.T) {
	session := startTestServer(t, buildFixture(t))
	zero := 0
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "parse",
		Arguments: map[string]any{"book": "MAT", "chapter": 26, "verse": 28, "word": zero, "corpus": "TAGNT"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected parse to report a tool error for word=0 (not 1-based)")
	}
}

// TestInterlinearOverMCP is a genuine end-to-end round trip for T35 - not
// just interlinear.Build in isolation, but the real MCP tool registration,
// schema, and JSON marshaling path.
func TestInterlinearOverMCP(t *testing.T) {
	session := startTestServer(t, buildFixture(t))
	res := callTool[interlinearResult](t, session, "interlinear", map[string]any{
		"book": "MAT", "chapter": 26, "verse": 28, "corpus": "TAGNT",
	})
	if len(res.Words) != 2 {
		t.Fatalf("words = %d, want 2", len(res.Words))
	}
	var saw bool
	for _, w := range res.Words {
		if w.DStrong == "G0859" {
			saw = true
			if w.Gloss != "forgiveness" {
				t.Errorf("gloss = %q, want forgiveness (resolved via lexicon_lookup)", w.Gloss)
			}
			if w.Translit != "" {
				t.Errorf("translit = %q, want empty (not seeded in this fixture) - not a reason to fail, just confirming no fabrication", w.Translit)
			}
		}
	}
	if !saw {
		t.Fatal("G0859 word missing from interlinear result")
	}
	if res.Sources["TAGNT"].File == "" {
		t.Error("Sources[TAGNT].File is empty, want the T31 sources map populated same as citationsResult")
	}
}

// TestInterlinearRejectsInvalidWordNumberOverMCP mirrors parse's own guard -
// interlinear shares the same wordScopedArgs validation.
func TestInterlinearRejectsInvalidWordNumberOverMCP(t *testing.T) {
	session := startTestServer(t, buildFixture(t))
	zero := 0
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "interlinear",
		Arguments: map[string]any{"book": "MAT", "chapter": 26, "verse": 28, "word": zero, "corpus": "TAGNT"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected interlinear to report a tool error for word=0 (not 1-based)")
	}
}

// TestLexiconLookupOverMCP confirms the Greek/Hebrew definition license gate
// (T34) holds over the real MCP transport, not just lexicon.Lookup directly.
func TestLexiconLookupOverMCP(t *testing.T) {
	session := startTestServer(t, buildFixture(t))
	entry := callTool[struct {
		DStrong    string  `json:"dstrong"`
		Gloss      string  `json:"gloss"`
		Definition *string `json:"definition"`
	}](t, session, "lexicon_lookup", map[string]any{"dstrong": "G0859"})
	if entry.Gloss != "forgiveness" {
		t.Errorf("gloss = %q, want forgiveness", entry.Gloss)
	}
	if entry.Definition == nil || *entry.Definition != "release, pardon" {
		t.Errorf("definition = %v, want a populated Greek definition", entry.Definition)
	}
}
