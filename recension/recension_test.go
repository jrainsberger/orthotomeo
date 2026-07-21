package recension_test

import (
	"testing"

	"github.com/jrainsberger/orthotomeo/recension"
)

func TestIsDivergent(t *testing.T) {
	cases := []struct {
		name       string
		book       string
		sourceCode string
		want       bool
	}{
		{"Jeremiah in Brenton is a different recension", "JER", "Brenton", true},
		{"Jeremiah in Swete too", "JER", "Swete", true},
		{"Jeremiah in the OSS LXX lemma stream too", "JER", "OSS-LXX-lemma", true},
		{"Jeremiah in KJV is not - KJV is the canonical tradition", "JER", "KJV", false},
		{"Jeremiah in WEB is not", "JER", "WEB", false},
		{"Psalms in Brenton is renumbered, not a different recension", "PSA", "Brenton", false},
		{"Esther in Brenton has Greek additions, not a reordering", "EST", "Brenton", false},
		{"unknown book", "NON", "Brenton", false},
		{"unknown source", "JER", "Nonesuch", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := recension.IsDivergent(tc.book, tc.sourceCode); got != tc.want {
				t.Errorf("IsDivergent(%q, %q) = %v, want %v", tc.book, tc.sourceCode, got, tc.want)
			}
		})
	}
}
