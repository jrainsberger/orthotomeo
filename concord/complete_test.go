package concord

import "testing"

// TestCheckCompleteRaisesOnPartialRead proves the invariant #3 guard itself:
// ConcordLemma/ConcordPhrase call this after every count-then-scan pair, so
// if a driver or a future refactor ever let the scanned set diverge from
// the independently counted total, this is what turns that into a raised
// error instead of a silently truncated result.
func TestCheckCompleteRaisesOnPartialRead(t *testing.T) {
	if err := checkComplete("op", 5, 3); err == nil {
		t.Fatal("expected an error when scanned rows (3) don't match COUNT() (5)")
	}
}

func TestCheckCompleteAllowsAgreement(t *testing.T) {
	if err := checkComplete("op", 5, 5); err != nil {
		t.Errorf("unexpected error for a complete read: %v", err)
	}
}
