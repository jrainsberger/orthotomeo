package lexnorm

import "testing"

func TestNFC(t *testing.T) {
	// The exact pair this package exists to fix, confirmed against the real
	// TAGNT corpus (Mrk.16.16's stored lemma is the oxia form): Greek
	// polytonic oxia (U+1F77) and monotonic tonos (U+03AF) are canonically
	// equivalent - NFC must collapse both to the same bytes.
	oxia := "βαπτίζω"
	tonos := "βαπτίζω"

	tests := []struct {
		name string
		in   string
	}{
		{"oxia form", oxia},
		{"tonos form", tonos},
		{"empty", ""},
		{"plain ascii", "abc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NFC(tt.in)
			if tt.in == "" && got != "" {
				t.Errorf("NFC(%q) = %q, want empty unchanged", tt.in, got)
			}
		})
	}

	if NFC(oxia) != NFC(tonos) {
		t.Errorf("NFC(oxia) = %q, NFC(tonos) = %q - want equal (canonically equivalent accents)", NFC(oxia), NFC(tonos))
	}
}
