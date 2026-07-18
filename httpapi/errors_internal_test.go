package httpapi

// White-box (package httpapi, not httpapi_test) because writeError is the
// unexported translation seam under test. What it guards is that
// engine-shaped error text never reaches a browser unchanged.

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jrainsberger/orthotomeo/engine"
)

func TestWriteErrorTranslatesOversizeQueryForPeople(t *testing.T) {
	w := httptest.NewRecorder()
	writeError(w, http.StatusBadRequest, &engine.ResultTooLargeError{Op: "ConcordLemma", Matched: 20705, Limit: 2000})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 - an over-broad query is a client error, not a server fault", w.Code)
	}

	var body struct {
		Error   string `json:"error"`
		Matched int    `json:"matched"`
		Limit   int    `json:"limit"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if body.Matched != 20705 || body.Limit != 2000 {
		t.Errorf("matched/limit = %d/%d, want 20705/2000 - the counts must travel as structured fields, not only inside prose a client would have to parse",
			body.Matched, body.Limit)
	}
	if !strings.Contains(body.Error, "20705") {
		t.Errorf("message %q omits the match count - knowing how far over you are is the actionable part", body.Error)
	}
	for _, leak := range []string{"ConcordLemma", "Count", "engine:"} {
		if strings.Contains(body.Error, leak) {
			t.Errorf("message %q leaks %q - that is text for a developer or tool caller, not for someone who typed a common word into a search box",
				body.Error, leak)
		}
	}
}

// The translation has to be specific to the one error it rewrites - a
// catch-all that reshaped every error would bury genuine validation messages.
func TestWriteErrorLeavesOtherErrorsAlone(t *testing.T) {
	const original = `missing required query param "corpus"`
	w := httptest.NewRecorder()
	writeError(w, http.StatusBadRequest, errors.New(original))

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error"] != original {
		t.Errorf("error = %v, want the original message unchanged", body["error"])
	}
	if _, ok := body["matched"]; ok {
		t.Error("a non-oversize error must not carry matched/limit fields")
	}
}
