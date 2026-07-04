package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jrainsberger/orthotomeo/books"
	"github.com/jrainsberger/orthotomeo/engine"
	"github.com/jrainsberger/orthotomeo/httpapi"
	"github.com/jrainsberger/orthotomeo/sources"
	"github.com/jrainsberger/orthotomeo/store"
)

// buildFixture mirrors the same real-shape fixture engine_test.go and
// mcp_test.go use: G0859/ἄφεσις at Mat.26.28, adjacent to εἰς, plus a
// lexicon row so /define and /interlinear have something real to resolve.
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

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	e, err := engine.Open(buildFixture(t))
	if err != nil {
		t.Fatalf("engine.Open: %v", err)
	}
	t.Cleanup(func() { e.Close() })
	ts := httptest.NewServer(httpapi.New(e).Handler())
	t.Cleanup(ts.Close)
	return ts
}

func getJSON(t *testing.T, ts *httptest.Server, path string, out any) *http.Response {
	t.Helper()
	res, err := http.Get(ts.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return res
}

func TestVerseEndpoint(t *testing.T) {
	ts := newTestServer(t)
	var body struct {
		Citations []struct {
			Text string `json:"text"`
		} `json:"citations"`
	}
	res := getJSON(t, ts, "/verse?book=MAT&chapter=26&verse=28&editions=KJV", &body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if len(body.Citations) != 1 || body.Citations[0].Text != "blood of the new testament" {
		t.Errorf("citations = %+v, want the seeded KJV text", body.Citations)
	}
}

func TestPassageEndpoint(t *testing.T) {
	ts := newTestServer(t)
	var body struct {
		Citations []any `json:"citations"`
	}
	res := getJSON(t, ts, "/passage?book=MAT&start_chapter=26&start_verse=28&end_chapter=26&end_verse=28&editions=KJV", &body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if len(body.Citations) != 1 {
		t.Errorf("citations = %d, want 1", len(body.Citations))
	}
}

func TestConcordEndpointByQuery(t *testing.T) {
	ts := newTestServer(t)
	var body struct {
		Citations []struct {
			DStrong string `json:"dstrong"`
		} `json:"citations"`
		Sources map[string]struct {
			File string `json:"file"`
		} `json:"sources"`
	}
	res := getJSON(t, ts, "/concord?query=G0859&corpus=TAGNT", &body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if len(body.Citations) != 1 || body.Citations[0].DStrong != "G0859" {
		t.Errorf("citations = %+v, want one G0859 row", body.Citations)
	}
	if body.Sources["TAGNT"].File == "" {
		t.Error("Sources[TAGNT].File is empty, want it populated (T31)")
	}
}

func TestConcordEndpointByPhrase(t *testing.T) {
	ts := newTestServer(t)
	var body struct {
		Citations []struct {
			Text string `json:"text"`
		} `json:"citations"`
	}
	res := getJSON(t, ts, "/concord?phrase=εἰς,ἄφεσις&corpus=TAGNT&window=0", &body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if len(body.Citations) != 1 || body.Citations[0].Text != "εἰς ἄφεσιν" {
		t.Errorf("citations = %+v, want the verbatim two-word span", body.Citations)
	}
}

func TestParseEndpoint(t *testing.T) {
	ts := newTestServer(t)
	var body struct {
		Citations []struct {
			DStrong string `json:"dstrong"`
			Grammar string `json:"grammar"`
		} `json:"citations"`
	}
	res := getJSON(t, ts, "/parse?book=MAT&chapter=26&verse=28&word=2&corpus=TAGNT", &body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if len(body.Citations) != 1 || body.Citations[0].DStrong != "G0859" {
		t.Errorf("citations = %+v, want G0859", body.Citations)
	}
}

func TestAttestEndpoint(t *testing.T) {
	ts := newTestServer(t)
	var body struct {
		Citations []struct {
			Attestation string `json:"attestation"`
		} `json:"citations"`
	}
	res := getJSON(t, ts, "/attest?book=MAT&chapter=26&verse=28&corpus=TAGNT", &body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if len(body.Citations) != 2 || body.Citations[0].Attestation != "NKO" {
		t.Errorf("citations = %+v, want 2 rows with Type=NKO", body.Citations)
	}
}

func TestInterlinearEndpoint(t *testing.T) {
	ts := newTestServer(t)
	var body struct {
		Words []struct {
			DStrong string `json:"dstrong"`
			Gloss   string `json:"gloss"`
		} `json:"words"`
	}
	res := getJSON(t, ts, "/interlinear?book=MAT&chapter=26&verse=28&corpus=TAGNT", &body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	var saw bool
	for _, w := range body.Words {
		if w.DStrong == "G0859" {
			saw = true
			if w.Gloss != "forgiveness" {
				t.Errorf("gloss = %q, want forgiveness", w.Gloss)
			}
		}
	}
	if !saw {
		t.Fatal("G0859 word missing from interlinear response")
	}
}

func TestDefineEndpoint(t *testing.T) {
	ts := newTestServer(t)
	var body struct {
		Gloss      string  `json:"gloss"`
		Definition *string `json:"definition"`
	}
	res := getJSON(t, ts, "/define?dstrong=G0859", &body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if body.Gloss != "forgiveness" {
		t.Errorf("gloss = %q, want forgiveness", body.Gloss)
	}
	if body.Definition == nil {
		t.Error("Definition = nil, want the Greek definition populated")
	}
}

func TestDefineEndpointUnknownDStrongIs404(t *testing.T) {
	ts := newTestServer(t)
	res, err := http.Get(ts.URL + "/define?dstrong=G9999999")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", res.StatusCode)
	}
}

// TestBooksEndpoint is the direct test for the book-field autocomplete's
// data source: the real 66-book canonical registry (books.Registry(),
// backed by the same books.json every loader treats as ground truth), not
// a hand-typed list that could drift from it.
func TestBooksEndpoint(t *testing.T) {
	ts := newTestServer(t)
	var list []struct {
		Order int    `json:"order"`
		Code  string `json:"code"`
		Name  string `json:"name"`
	}
	res := getJSON(t, ts, "/books", &list)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if len(list) != 66 {
		t.Fatalf("books = %d, want 66 (the canonical Protestant registry)", len(list))
	}
	if list[0].Code != "GEN" || list[0].Name != "Genesis" || list[0].Order != 1 {
		t.Errorf("list[0] = %+v, want Genesis first (order 1)", list[0])
	}
}

func TestMissingRequiredParamIs400(t *testing.T) {
	ts := newTestServer(t)
	res, err := http.Get(ts.URL + "/parse?book=MAT&chapter=26&verse=28") // corpus missing
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", res.StatusCode)
	}
}

// TestOnlyGETIsAllowed is the GET-only security requirement: every API
// route is registered as "GET /path" (Go 1.22+ ServeMux method-prefixed
// patterns), so the mux itself rejects any other method with 405 - there is
// no handler code path that could ever mutate anything.
func TestOnlyGETIsAllowed(t *testing.T) {
	ts := newTestServer(t)
	for _, path := range []string{"/verse", "/passage", "/concord", "/parse", "/attest", "/interlinear", "/define", "/books"} {
		res, err := http.Post(ts.URL+path, "application/json", strings.NewReader("{}"))
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("POST %s status = %d, want 405", path, res.StatusCode)
		}
	}
}

func TestIndexServesHTML(t *testing.T) {
	ts := newTestServer(t)
	res, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
}

func TestStaticAssetsServed(t *testing.T) {
	ts := newTestServer(t)
	for _, path := range []string{"/static/app.js", "/static/style.css"} {
		res, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200", path, res.StatusCode)
		}
	}
}
