package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jrainsberger/orthotomeo/books"
	"github.com/jrainsberger/orthotomeo/engine"
	"github.com/jrainsberger/orthotomeo/sources"
	"github.com/jrainsberger/orthotomeo/store"
)

// buildFixture writes a minimal real DB (one lexicon entry, no words/verses
// needed) - just enough for lexicon_lookup, the simplest tool, to prove a
// real MCP round trip through newHandler's actual routing, not a bypassed
// direct call.
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
	if _, err := db.Exec(`
		INSERT INTO lexicon (dstrong, estrong, ustrong, language, lemma, translit, gloss, definition, def_license)
		VALUES ('G0859', 'G859', 'G0859', 'grc', 'ἄφεσις', 'aphesis', 'forgiveness', 'release, pardon', 'Public Domain')`); err != nil {
		t.Fatalf("insert lexicon: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return path
}

// TestNewHandlerServesBothMCPAndREST is T43's acceptance criterion: /mcp
// (Streamable HTTP, for a remote LLM client) and the REST API must both be
// reachable off one handler/one Cloud Run service, through the real
// rate-limit/timeout/security middleware chain - not two isolated pieces
// that happen to look right individually.
func TestNewHandlerServesBothMCPAndREST(t *testing.T) {
	e, err := engine.Open(buildFixture(t))
	if err != nil {
		t.Fatalf("engine.Open: %v", err)
	}
	defer e.Close()

	ts := httptest.NewServer(newHandler(e))
	defer ts.Close()

	t.Run("REST", func(t *testing.T) {
		res, err := http.Get(ts.URL + "/books")
		if err != nil {
			t.Fatalf("GET /books: %v", err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("GET /books status = %d, want 200", res.StatusCode)
		}
	})

	t.Run("UI is not published", func(t *testing.T) {
		res, err := http.Get(ts.URL + "/")
		if err != nil {
			t.Fatalf("GET /: %v", err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("GET / status = %d, want 404 (UI must stay unpublished on this deployment)", res.StatusCode)
		}
	})

	t.Run("MCP", func(t *testing.T) {
		client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
		transport := &mcp.StreamableClientTransport{
			Endpoint:             ts.URL + "/mcp",
			DisableStandaloneSSE: true, // Stateless server: no server-initiated push to receive
		}
		session, err := client.Connect(context.Background(), transport, nil)
		if err != nil {
			t.Fatalf("connect: %v", err)
		}
		defer session.Close()

		res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
			Name:      "lexicon_lookup",
			Arguments: map[string]any{"dstrong": "G0859"},
		})
		if err != nil {
			t.Fatalf("CallTool lexicon_lookup: %v", err)
		}
		if res.IsError {
			t.Fatalf("lexicon_lookup returned an error result: %+v", res.Content)
		}
		text, ok := res.Content[0].(*mcp.TextContent)
		if !ok {
			t.Fatalf("content[0] is %T, want *mcp.TextContent", res.Content[0])
		}
		var entry struct {
			Gloss string `json:"gloss"`
		}
		if err := json.Unmarshal([]byte(text.Text), &entry); err != nil {
			t.Fatalf("unmarshal result %q: %v", text.Text, err)
		}
		if entry.Gloss != "forgiveness" {
			t.Errorf("gloss = %q, want forgiveness", entry.Gloss)
		}
	})
}
