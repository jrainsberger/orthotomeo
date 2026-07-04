package httpapi

import (
	"fmt"

	"github.com/jrainsberger/orthotomeo/sources"
)

// shippableEditions and requireShippable take the registry as a parameter
// (rather than calling sources.Registry() internally) so the actual gating
// logic - not a reimplementation of it - is what a test exercises against a
// fabricated non-shippable row. Today every real sources.json entry is
// shippable=true (there is no non-shippable source until T23's user-fetched
// Rahlfs LXX exists), so this gate is presently a no-op in production but a
// real, tested guard for when that changes.

// shippableEditions filters requested down to editions the live registry
// marks shippable=1 - a non-shippable edition is silently dropped from the
// served set, not an error (a caller who asked for KJV,ASV,SomeNCSource
// still gets a valid partial response, matching how GetVerse already
// tolerates a caller asking for more editions than exist for a verse).
func shippableEditions(reg []sources.Source, requested []string) []string {
	if len(requested) == 0 {
		return requested
	}
	shippable := make(map[string]bool, len(reg))
	for _, src := range reg {
		shippable[src.Code] = src.Shippable
	}
	out := make([]string, 0, len(requested))
	for _, e := range requested {
		if shippable[e] {
			out = append(out, e)
		}
	}
	return out
}

// requireShippable errors if code names a KNOWN, non-shippable source. An
// unknown code is deliberately not this function's concern - it passes
// through so engine's own "not a word-tagged corpus" (or equivalent)
// validation produces the real error, rather than this gate duplicating
// that check with a worse message.
func requireShippable(reg []sources.Source, code string) error {
	for _, src := range reg {
		if src.Code == code {
			if !src.Shippable {
				return fmt.Errorf("%s is not available in this build (non-shippable source)", code)
			}
			return nil
		}
	}
	return nil
}

func (s *Server) shippableEditions(requested []string) ([]string, error) {
	if len(requested) == 0 {
		return requested, nil
	}
	reg, err := sources.Registry()
	if err != nil {
		return nil, fmt.Errorf("shippableEditions: %w", err)
	}
	return shippableEditions(reg, requested), nil
}

func (s *Server) requireShippable(code string) error {
	reg, err := sources.Registry()
	if err != nil {
		return fmt.Errorf("requireShippable: %w", err)
	}
	return requireShippable(reg, code)
}
