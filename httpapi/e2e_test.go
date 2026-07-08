package httpapi_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/jrainsberger/orthotomeo/engine"
	"github.com/jrainsberger/orthotomeo/httpapi"
)

// newE2EContext starts a real (headless) Chrome/Chromium instance via
// chromedp - one context, one browser tab, for the whole test. Deliberately
// does NOT run a separate short-lived "probe" call first: chromedp ties a
// tab's lifetime to whichever context first navigated it, and cancelling
// even a child WithTimeout context used only for a probe tears down that
// tab - confirmed directly (a two-call probe-then-act version failed every
// real action afterward with "context canceled", even though the second
// call used its own fresh child context). One context, one Run call's worth
// of actions, is the shape that actually works.
func newE2EContext(t *testing.T) context.Context {
	t.Helper()
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), chromedp.DefaultExecAllocatorOptions[:]...)
	t.Cleanup(allocCancel)
	ctx, cancel := chromedp.NewContext(allocCtx)
	t.Cleanup(cancel)
	return ctx
}

// skipIfNoBrowser classifies a chromedp error: a browser-launch failure
// (no Chrome/Chromium found - the case a machine without a browser install
// hits) is skipped, not failed, so `go test ./...` stays green there; any
// other error is a real test failure and must not be silently swallowed.
func skipIfNoBrowser(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	msg := err.Error()
	for _, s := range []string{"executable file not found", "no such file or directory", "cannot find the file", "exec:"} {
		if strings.Contains(msg, s) {
			t.Skipf("no usable Chrome/Chromium found, skipping E2E test: %v", err)
		}
	}
	t.Fatalf("chromedp run: %v", err)
}

// TestE2EIndexPageLoadsSearchForm is the baseline smoke check: the real
// embedded index.html actually renders a working search form in a real
// browser, not just "the HTTP response was 200."
func TestE2EIndexPageLoadsSearchForm(t *testing.T) {
	e, err := engine.Open(buildFixture(t))
	if err != nil {
		t.Fatalf("engine.Open: %v", err)
	}
	defer e.Close()
	ts := httptest.NewServer(httpapi.New(e).Handler())
	defer ts.Close()

	runCtx, cancel := context.WithTimeout(newE2EContext(t), 20*time.Second)
	defer cancel()

	var title string
	err = chromedp.Run(runCtx,
		chromedp.Navigate(ts.URL+"/"),
		chromedp.WaitVisible(`#search`, chromedp.ByID),
		chromedp.Title(&title),
	)
	skipIfNoBrowser(t, err)
	if title != "orthotomeo" {
		t.Errorf("title = %q, want orthotomeo", title)
	}
}

// TestE2EParseSearchRendersResultsTable drives the real UI end to end: pick
// "parse" mode (which shows/hides fields via app.js's real change listener,
// not simulated), fill the ref + corpus fields, submit, and confirm the
// results table that comes back from a real fetch() to /parse actually
// renders the expected word. This is the one thing httpapi_test.go's
// handler-level tests structurally cannot prove - that the DOM, the mode
// switching, and the fetch/render JS in app.js actually work together in a
// real browser.
func TestE2EParseSearchRendersResultsTable(t *testing.T) {
	e, err := engine.Open(buildFixture(t))
	if err != nil {
		t.Fatalf("engine.Open: %v", err)
	}
	defer e.Close()
	ts := httptest.NewServer(httpapi.New(e).Handler())
	defer ts.Close()

	runCtx, cancel := context.WithTimeout(newE2EContext(t), 20*time.Second)
	defer cancel()

	var resultsText string
	err = chromedp.Run(runCtx,
		chromedp.Navigate(ts.URL+"/"),
		chromedp.WaitVisible(`#search`, chromedp.ByID),
		// Real change event, not just a property set - app.js's mode
		// listener (showFieldsFor) must actually fire to reveal the
		// corpus/word fields, same as a real user picking the dropdown.
		chromedp.Evaluate(`(() => {
			const sel = document.querySelector('#mode');
			sel.value = 'parse';
			sel.dispatchEvent(new Event('change'));
		})()`, nil),
		chromedp.WaitVisible(`[data-field="corpus"].active`),
		chromedp.SetValue(`input[name="book"]`, "MAT", chromedp.ByQuery),
		chromedp.SetValue(`input[name="chapter"]`, "26", chromedp.ByQuery),
		chromedp.SetValue(`input[name="verse"]`, "28", chromedp.ByQuery),
		chromedp.SetValue(`select[name="corpus"]`, "TAGNT", chromedp.ByQuery),
		chromedp.Click(`#search button[type="submit"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#results table`, chromedp.ByQuery),
		chromedp.Text(`#results`, &resultsText, chromedp.ByQuery),
	)
	skipIfNoBrowser(t, err)
	if !strings.Contains(resultsText, "G0859") {
		t.Errorf("results table text = %q, want it to contain G0859", resultsText)
	}
	if !strings.Contains(resultsText, "MAT.26.28") {
		t.Errorf("results table text = %q, want it to contain MAT.26.28", resultsText)
	}
}

// TestE2EVerseSearchReadsMultiSelectEditions is the direct test for the
// editions checkbox group - app.js's submit handler gathers every CHECKED
// box sharing name="editions", not a <select multiple> (switched away from
// that: a plain click on a native multi-select silently deselects
// everything else, confirmed live - a real trap for a user trying to pick
// more than one edition, and confusing enough on its own screenshot review
// that it looked like a stale-build bug at first). Clicks the ASV checkbox
// (KJV is checked by default in index.html) via a real click - the same
// interaction a user performs - then confirms BOTH editions round-trip
// through a real fetch to /verse.
func TestE2EVerseSearchReadsMultiSelectEditions(t *testing.T) {
	e, err := engine.Open(buildFixture(t))
	if err != nil {
		t.Fatalf("engine.Open: %v", err)
	}
	defer e.Close()
	ts := httptest.NewServer(httpapi.New(e).Handler())
	defer ts.Close()

	runCtx, cancel := context.WithTimeout(newE2EContext(t), 20*time.Second)
	defer cancel()

	var resultsText string
	err = chromedp.Run(runCtx,
		chromedp.Navigate(ts.URL+"/"),
		chromedp.WaitVisible(`#search`, chromedp.ByID),
		// mode defaults to "verse", so its fields are already active - just
		// check a second edition (KJV is already checked by default).
		chromedp.Click(`input[name="editions"][value="ASV"]`, chromedp.ByQuery),
		chromedp.SetValue(`input[name="book"]`, "MAT", chromedp.ByQuery),
		chromedp.SetValue(`input[name="chapter"]`, "26", chromedp.ByQuery),
		chromedp.SetValue(`input[name="verse"]`, "28", chromedp.ByQuery),
		chromedp.Click(`#search button[type="submit"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#results table`, chromedp.ByQuery),
		chromedp.Text(`#results`, &resultsText, chromedp.ByQuery),
	)
	skipIfNoBrowser(t, err)
	// buildFixture only seeds KJV verse_text (no ASV row exists for
	// Mat.26.28), so ASV must come back as its own Flagged placeholder
	// Citation (T15's "an edition with no counterpart still produces a
	// Citation" contract) - both edition codes appearing in the results
	// table proves both were actually sent, not just the first.
	if !strings.Contains(resultsText, "KJV") {
		t.Errorf("results table text = %q, want it to contain KJV", resultsText)
	}
	if !strings.Contains(resultsText, "ASV") {
		t.Errorf("results table text = %q, want it to contain ASV - the multi-select must have sent both editions, not just the first", resultsText)
	}
}

// TestE2EVerseRefCellIsPlainTextNotACrossLink is the direct regression test
// for a real bug found in use: a /verse citation's edition is a verse-text
// edition (KJV/ASV/WEB/Brenton), never one of the four word-tagged corpora
// /interlinear accepts. refCell used to build an interlinear cross-link
// from ANY truthy corpus/edition, so clicking a KJV ref set the corpus
// <select> to "KJV" - an option that doesn't exist - leaving it blank and
// making the follow-up request fail with `missing required query param
// "corpus"`. The ref cell must render as plain text for a non-word-corpus
// edition instead of a broken link.
func TestE2EVerseRefCellIsPlainTextNotACrossLink(t *testing.T) {
	e, err := engine.Open(buildFixture(t))
	if err != nil {
		t.Fatalf("engine.Open: %v", err)
	}
	defer e.Close()
	ts := httptest.NewServer(httpapi.New(e).Handler())
	defer ts.Close()

	runCtx, cancel := context.WithTimeout(newE2EContext(t), 20*time.Second)
	defer cancel()

	var xlinkCount int
	var refText string
	err = chromedp.Run(runCtx,
		chromedp.Navigate(ts.URL+"/"),
		chromedp.WaitVisible(`#search`, chromedp.ByID),
		chromedp.SetValue(`input[name="book"]`, "MAT", chromedp.ByQuery),
		chromedp.SetValue(`input[name="chapter"]`, "26", chromedp.ByQuery),
		chromedp.SetValue(`input[name="verse"]`, "28", chromedp.ByQuery),
		chromedp.Click(`#search button[type="submit"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#results table`, chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelectorAll('#results tbody tr td:nth-child(2) a.xlink').length`, &xlinkCount),
		chromedp.Text(`#results tbody tr td:nth-child(2)`, &refText, chromedp.ByQuery),
	)
	skipIfNoBrowser(t, err)
	if xlinkCount != 0 {
		t.Errorf("ref cell has %d cross-link(s), want 0 - a KJV verse-text citation has no valid interlinear corpus to link into", xlinkCount)
	}
	if !strings.Contains(refText, "MAT.26.28") {
		t.Errorf("ref cell text = %q, want it to still show the plain ref", refText)
	}
}

// TestE2EDStrongLinkNavigatesToDefine is the direct test for the flagged
// UX gap: a dStrong cell in a parse/concord result must be a real,
// clickable cross-link into define mode, landing on the full gloss +
// definition, not a dead-end piece of text a reader has to retype
// elsewhere.
func TestE2EDStrongLinkNavigatesToDefine(t *testing.T) {
	e, err := engine.Open(buildFixture(t))
	if err != nil {
		t.Fatalf("engine.Open: %v", err)
	}
	defer e.Close()
	ts := httptest.NewServer(httpapi.New(e).Handler())
	defer ts.Close()

	runCtx, cancel := context.WithTimeout(newE2EContext(t), 20*time.Second)
	defer cancel()

	var mode, entryText string
	err = chromedp.Run(runCtx,
		chromedp.Navigate(ts.URL+"/"),
		chromedp.WaitVisible(`#search`, chromedp.ByID),
		chromedp.Evaluate(`(() => {
			const sel = document.querySelector('#mode');
			sel.value = 'parse';
			sel.dispatchEvent(new Event('change'));
		})()`, nil),
		chromedp.WaitVisible(`[data-field="corpus"].active`),
		chromedp.SetValue(`input[name="book"]`, "MAT", chromedp.ByQuery),
		chromedp.SetValue(`input[name="chapter"]`, "26", chromedp.ByQuery),
		chromedp.SetValue(`input[name="verse"]`, "28", chromedp.ByQuery),
		chromedp.SetValue(`select[name="corpus"]`, "TAGNT", chromedp.ByQuery),
		chromedp.SetValue(`input[name="word"]`, "2", chromedp.ByQuery), // scope to the single G0859 word, so the row selectors below are unambiguous
		chromedp.Click(`#search button[type="submit"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#results table`, chromedp.ByQuery),
		// parse row columns: ordinal, ref, edition, text, lemma, dstrong, ...
		chromedp.Click(`#results tbody tr td:nth-child(6) a.xlink`, chromedp.ByQuery),
		chromedp.WaitVisible(`.entry-card`, chromedp.ByQuery),
		chromedp.Value(`#mode`, &mode, chromedp.ByID),
		chromedp.Text(`.entry-card`, &entryText, chromedp.ByQuery),
	)
	skipIfNoBrowser(t, err)
	if mode != "define" {
		t.Errorf("mode = %q, want define after clicking the dStrong link", mode)
	}
	if !strings.Contains(entryText, "forgiveness") {
		t.Errorf("entry card text = %q, want it to contain the resolved gloss", entryText)
	}
}

// TestE2ELemmaLinkNavigatesToConcord is the concordance-workflow cross-link:
// clicking a lemma must search every other occurrence of it, not require
// retyping the lemma into concord mode by hand.
func TestE2ELemmaLinkNavigatesToConcord(t *testing.T) {
	e, err := engine.Open(buildFixture(t))
	if err != nil {
		t.Fatalf("engine.Open: %v", err)
	}
	defer e.Close()
	ts := httptest.NewServer(httpapi.New(e).Handler())
	defer ts.Close()

	runCtx, cancel := context.WithTimeout(newE2EContext(t), 20*time.Second)
	defer cancel()

	var mode, resultsText string
	err = chromedp.Run(runCtx,
		chromedp.Navigate(ts.URL+"/"),
		chromedp.WaitVisible(`#search`, chromedp.ByID),
		chromedp.Evaluate(`(() => {
			const sel = document.querySelector('#mode');
			sel.value = 'parse';
			sel.dispatchEvent(new Event('change'));
		})()`, nil),
		chromedp.WaitVisible(`[data-field="corpus"].active`),
		chromedp.SetValue(`input[name="book"]`, "MAT", chromedp.ByQuery),
		chromedp.SetValue(`input[name="chapter"]`, "26", chromedp.ByQuery),
		chromedp.SetValue(`input[name="verse"]`, "28", chromedp.ByQuery),
		chromedp.SetValue(`select[name="corpus"]`, "TAGNT", chromedp.ByQuery),
		chromedp.SetValue(`input[name="word"]`, "2", chromedp.ByQuery), // scope to the single G0859 word, so the row selectors below are unambiguous
		chromedp.Click(`#search button[type="submit"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#results table`, chromedp.ByQuery),
		// parse row columns: ordinal, ref, edition, text, lemma, dstrong, ...
		chromedp.Click(`#results tbody tr td:nth-child(5) a.xlink`, chromedp.ByQuery),
		chromedp.WaitVisible(`#results table`, chromedp.ByQuery),
		chromedp.Value(`#mode`, &mode, chromedp.ByID),
		chromedp.Text(`#results`, &resultsText, chromedp.ByQuery),
	)
	skipIfNoBrowser(t, err)
	if mode != "concord" {
		t.Errorf("mode = %q, want concord after clicking the lemma link", mode)
	}
	if !strings.Contains(resultsText, "G0859") {
		t.Errorf("results text = %q, want the concordance result for the clicked lemma", resultsText)
	}
}

// TestE2EBackLinkRestoresPriorResults proves the one-level history actually
// undoes a cross-link jump - without it, clicking into a definition from a
// result row would strand the reader with no way back except re-running
// the original search from scratch.
func TestE2EBackLinkRestoresPriorResults(t *testing.T) {
	e, err := engine.Open(buildFixture(t))
	if err != nil {
		t.Fatalf("engine.Open: %v", err)
	}
	defer e.Close()
	ts := httptest.NewServer(httpapi.New(e).Handler())
	defer ts.Close()

	runCtx, cancel := context.WithTimeout(newE2EContext(t), 20*time.Second)
	defer cancel()

	var modeAfterBack, resultsAfterBack string
	err = chromedp.Run(runCtx,
		chromedp.Navigate(ts.URL+"/"),
		chromedp.WaitVisible(`#search`, chromedp.ByID),
		chromedp.Evaluate(`(() => {
			const sel = document.querySelector('#mode');
			sel.value = 'parse';
			sel.dispatchEvent(new Event('change'));
		})()`, nil),
		chromedp.WaitVisible(`[data-field="corpus"].active`),
		chromedp.SetValue(`input[name="book"]`, "MAT", chromedp.ByQuery),
		chromedp.SetValue(`input[name="chapter"]`, "26", chromedp.ByQuery),
		chromedp.SetValue(`input[name="verse"]`, "28", chromedp.ByQuery),
		chromedp.SetValue(`select[name="corpus"]`, "TAGNT", chromedp.ByQuery),
		chromedp.SetValue(`input[name="word"]`, "2", chromedp.ByQuery), // scope to the single G0859 word, so the row selectors below are unambiguous
		chromedp.Click(`#search button[type="submit"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#results table`, chromedp.ByQuery),
		// parse row columns: ordinal, ref, edition, text, lemma, dstrong, ...
		chromedp.Click(`#results tbody tr td:nth-child(6) a.xlink`, chromedp.ByQuery),
		chromedp.WaitVisible(`.entry-card`, chromedp.ByQuery),
		chromedp.WaitVisible(`#backLink`, chromedp.ByID),
		chromedp.Click(`#backLink`, chromedp.ByID),
		chromedp.WaitVisible(`#results table`, chromedp.ByQuery),
		chromedp.Value(`#mode`, &modeAfterBack, chromedp.ByID),
		chromedp.Text(`#results`, &resultsAfterBack, chromedp.ByQuery),
	)
	skipIfNoBrowser(t, err)
	if modeAfterBack != "parse" {
		t.Errorf("mode after back = %q, want parse (the mode before the cross-link jump)", modeAfterBack)
	}
	if !strings.Contains(resultsAfterBack, "G0859") {
		t.Errorf("results after back = %q, want the original parse results restored", resultsAfterBack)
	}
}

// TestE2EDStrongHoverShowsGlossTooltip is the direct test for the hover
// preview - dispatches a real "mouseenter" event (the same technique used
// elsewhere in this file for the mode <select>'s change listener) rather
// than attempting a literal mouse move, since what's under test is
// app.js's event-handling logic, not the browser's own pointer simulation.
func TestE2EDStrongHoverShowsGlossTooltip(t *testing.T) {
	e, err := engine.Open(buildFixture(t))
	if err != nil {
		t.Fatalf("engine.Open: %v", err)
	}
	defer e.Close()
	ts := httptest.NewServer(httpapi.New(e).Handler())
	defer ts.Close()

	runCtx, cancel := context.WithTimeout(newE2EContext(t), 20*time.Second)
	defer cancel()

	var tooltipText string
	err = chromedp.Run(runCtx,
		chromedp.Navigate(ts.URL+"/"),
		chromedp.WaitVisible(`#search`, chromedp.ByID),
		chromedp.Evaluate(`(() => {
			const sel = document.querySelector('#mode');
			sel.value = 'parse';
			sel.dispatchEvent(new Event('change'));
		})()`, nil),
		chromedp.WaitVisible(`[data-field="corpus"].active`),
		chromedp.SetValue(`input[name="book"]`, "MAT", chromedp.ByQuery),
		chromedp.SetValue(`input[name="chapter"]`, "26", chromedp.ByQuery),
		chromedp.SetValue(`input[name="verse"]`, "28", chromedp.ByQuery),
		chromedp.SetValue(`select[name="corpus"]`, "TAGNT", chromedp.ByQuery),
		chromedp.SetValue(`input[name="word"]`, "2", chromedp.ByQuery), // scope to the single G0859 word, so the row selectors below are unambiguous
		chromedp.Click(`#search button[type="submit"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#results table`, chromedp.ByQuery),
		// parse row columns: ordinal, ref, edition, text, lemma, dstrong, ...
		chromedp.Evaluate(`document.querySelector('#results tbody tr td:nth-child(6) a.xlink').dispatchEvent(new Event('mouseenter'))`, nil),
		chromedp.Sleep(600*time.Millisecond), // past the 250ms debounce + a real fetch round trip
		chromedp.Text(`.gloss-tooltip`, &tooltipText, chromedp.ByQuery),
	)
	skipIfNoBrowser(t, err)
	if !strings.Contains(tooltipText, "forgiveness") {
		t.Errorf("tooltip text = %q, want it to contain the resolved gloss", tooltipText)
	}
}

// TestE2EBookDatalistPopulatesFromRealRegistry is the direct test for the
// book-field autocomplete: confirms the <datalist> is actually populated by
// the real GET /books fetch on page load (not just that the endpoint
// returns data - httpapi_test.go's TestBooksEndpoint already covers that),
// and that a known code from the real registry is present.
func TestE2EBookDatalistPopulatesFromRealRegistry(t *testing.T) {
	e, err := engine.Open(buildFixture(t))
	if err != nil {
		t.Fatalf("engine.Open: %v", err)
	}
	defer e.Close()
	ts := httptest.NewServer(httpapi.New(e).Handler())
	defer ts.Close()

	runCtx, cancel := context.WithTimeout(newE2EContext(t), 20*time.Second)
	defer cancel()

	var optionCount int
	var firstOptionValue string
	err = chromedp.Run(runCtx,
		chromedp.Navigate(ts.URL+"/"),
		chromedp.WaitVisible(`#search`, chromedp.ByID),
		chromedp.Poll(`document.querySelectorAll('#books option').length === 66`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelectorAll('#books option').length`, &optionCount),
		chromedp.Evaluate(`document.querySelector('#books option').value`, &firstOptionValue),
	)
	skipIfNoBrowser(t, err)
	if optionCount != 66 {
		t.Errorf("datalist option count = %d, want 66", optionCount)
	}
	if firstOptionValue != "GEN" {
		t.Errorf("first option value = %q, want GEN", firstOptionValue)
	}
}
