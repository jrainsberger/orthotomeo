package httpapi

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/jrainsberger/orthotomeo/books"
	"github.com/jrainsberger/orthotomeo/retriever"
)

// queryInt parses a required integer query param, or returns an error
// naming the param - every handler's bad-input errors should say which
// field was wrong, not just "invalid request."
func queryInt(r *http.Request, name string) (int, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return 0, fmt.Errorf("missing required query param %q", name)
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("query param %q: %w", name, err)
	}
	return n, nil
}

// optionalWord parses "word" as a 1-based word_no, or nil if omitted -
// same shape every wordScopedArgs-equivalent call needs (Parse/Attestation/
// Interlinear all take *int).
func optionalWord(r *http.Request) (*int, error) {
	v := r.URL.Query().Get("word")
	if v == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return nil, fmt.Errorf(`query param "word": %w`, err)
	}
	if n < 1 {
		return nil, fmt.Errorf(`query param "word" must be >= 1 (1-based), got %d`, n)
	}
	return &n, nil
}

func (s *Server) queryRef(r *http.Request) (retriever.Ref, error) {
	book := r.URL.Query().Get("book")
	if book == "" {
		return retriever.Ref{}, fmt.Errorf(`missing required query param "book"`)
	}
	code, err := s.e.ResolveBookCode(book)
	if err != nil {
		return retriever.Ref{}, err
	}
	chapter, err := queryInt(r, "chapter")
	if err != nil {
		return retriever.Ref{}, err
	}
	verse, err := queryInt(r, "verse")
	if err != nil {
		return retriever.Ref{}, err
	}
	return retriever.Ref{Book: code, Chapter: chapter, Verse: verse}, nil
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return parts
}

func (s *Server) handleVerse(w http.ResponseWriter, r *http.Request) {
	ref, err := s.queryRef(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	editions, err := s.shippableEditions(splitCSV(r.URL.Query().Get("editions")))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cs, err := s.e.GetVerse(ref, editions)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	res, err := citationsPayload(cs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handlePassage(w http.ResponseWriter, r *http.Request) {
	book := r.URL.Query().Get("book")
	if book == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf(`missing required query param "book"`))
		return
	}
	code, err := s.e.ResolveBookCode(book)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	startCh, err := queryInt(r, "start_chapter")
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	startV, err := queryInt(r, "start_verse")
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	endCh, err := queryInt(r, "end_chapter")
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	endV, err := queryInt(r, "end_verse")
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	editions, err := s.shippableEditions(splitCSV(r.URL.Query().Get("editions")))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	rr := retriever.RefRange{
		Start: retriever.Ref{Book: code, Chapter: startCh, Verse: startV},
		End:   retriever.Ref{Book: code, Chapter: endCh, Verse: endV},
	}
	cs, err := s.e.GetPassage(rr, editions)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	res, err := citationsPayload(cs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handleConcord routes to ConcordPhrase when "phrase" is given (a comma-
// separated ordered token list), else ConcordLemma - the same phrase-vs-
// single-query branch T26's CLI concord subcommand makes on --phrase.
func (s *Server) handleConcord(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	corpus := q.Get("corpus")
	if corpus == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf(`missing required query param "corpus"`))
		return
	}
	if err := s.requireShippable(corpus); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	if phrase := q.Get("phrase"); phrase != "" {
		window := 0
		if v := q.Get("window"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				writeError(w, http.StatusBadRequest, fmt.Errorf(`query param "window": %w`, err))
				return
			}
			window = n
		}
		cs, err := s.e.ConcordPhrase(splitCSV(phrase), corpus, window)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		res, err := citationsPayload(cs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
		return
	}

	query := q.Get("query")
	if query == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf(`missing required query param "query" (or "phrase")`))
		return
	}
	cs, err := s.e.ConcordLemma(query, corpus, q.Get("by"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	res, err := citationsPayload(cs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) wordScoped(r *http.Request) (ref retriever.Ref, word *int, corpus string, err error) {
	ref, err = s.queryRef(r)
	if err != nil {
		return
	}
	word, err = optionalWord(r)
	if err != nil {
		return
	}
	corpus = r.URL.Query().Get("corpus")
	if corpus == "" {
		err = fmt.Errorf(`missing required query param "corpus"`)
		return
	}
	err = s.requireShippable(corpus)
	return
}

func (s *Server) handleParse(w http.ResponseWriter, r *http.Request) {
	ref, word, corpus, err := s.wordScoped(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cs, err := s.e.Parse(ref, word, corpus)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	res, err := citationsPayload(cs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleAttest(w http.ResponseWriter, r *http.Request) {
	ref, word, corpus, err := s.wordScoped(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cs, err := s.e.Attestation(ref, word, corpus)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	res, err := citationsPayload(cs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleInterlinear(w http.ResponseWriter, r *http.Request) {
	ref, word, corpus, err := s.wordScoped(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	words, srcs, err := s.e.Interlinear(ref, word, corpus)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, interlinearResponse{Words: words, Sources: srcs})
}

func (s *Server) handleDefine(w http.ResponseWriter, r *http.Request) {
	dstrong := r.URL.Query().Get("dstrong")
	if dstrong == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf(`missing required query param "dstrong"`))
		return
	}
	entry, err := s.e.Lookup(dstrong)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, lookupPayload(entry))
}

// handleBooks serves the canonical 66-book registry (order/code/name) for
// the UI's book-field autocomplete - not a database read, `books.Registry()`
// decodes the same embedded, checked-in books.json every loader already
// treats as ground truth (T2), so the datalist can never drift from the
// real canonical list the way a hand-typed <option> set in index.html
// eventually would.
func handleBooks(w http.ResponseWriter, r *http.Request) {
	reg, err := books.Registry()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, reg)
}
