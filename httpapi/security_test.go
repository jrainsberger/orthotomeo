package httpapi

import (
	"testing"

	"github.com/jrainsberger/orthotomeo/sources"
)

// fixtureReg fabricates a registry with one shippable and one non-shippable
// source - the real sources.json has zero non-shippable rows today (there
// is no non-shippable source until T23's user-fetched Rahlfs LXX exists),
// so testing the actual gating logic against a real registry can't yet
// prove the non-shippable branch does anything. Testing shippableEditions/
// requireShippable directly (not through Server, which always calls the
// real sources.Registry()) exercises the exact function T27's acceptance
// criteria describes, against a case the real corpus can't provide yet.
func fixtureReg() []sources.Source {
	return []sources.Source{
		{Code: "KJV", Shippable: true},
		{Code: "Rahlfs-LXX", Shippable: false},
	}
}

func TestShippableEditionsDropsNonShippable(t *testing.T) {
	got := shippableEditions(fixtureReg(), []string{"KJV", "Rahlfs-LXX"})
	if len(got) != 1 || got[0] != "KJV" {
		t.Errorf("got %v, want [KJV] (Rahlfs-LXX dropped, not errored)", got)
	}
}

func TestShippableEditionsEmptyRequestPassesThrough(t *testing.T) {
	got := shippableEditions(fixtureReg(), nil)
	if len(got) != 0 {
		t.Errorf("got %v, want empty (an empty request isn't a security question)", got)
	}
}

func TestShippableEditionsUnknownCodeDropped(t *testing.T) {
	// An unknown code isn't in the shippable map at all, so it's dropped -
	// same as a known non-shippable one. Unknown-corpus validation is
	// engine's job, not this gate's (see requireShippable's doc comment).
	got := shippableEditions(fixtureReg(), []string{"NOT-A-REAL-SOURCE"})
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestRequireShippableErrorsOnKnownNonShippable(t *testing.T) {
	if err := requireShippable(fixtureReg(), "Rahlfs-LXX"); err == nil {
		t.Fatal("expected an error for a known, non-shippable source")
	}
}

func TestRequireShippablePassesKnownShippable(t *testing.T) {
	if err := requireShippable(fixtureReg(), "KJV"); err != nil {
		t.Errorf("unexpected error for a shippable source: %v", err)
	}
}

func TestRequireShippablePassesUnknownCode(t *testing.T) {
	// Deliberately not this gate's job to reject an unknown corpus - that
	// must surface as engine's own validation error, not a shippable-gate
	// error with a worse message.
	if err := requireShippable(fixtureReg(), "NOT-A-REAL-SOURCE"); err != nil {
		t.Errorf("unexpected error for an unknown code: %v", err)
	}
}
