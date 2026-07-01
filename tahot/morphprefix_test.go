package tahot

import "testing"

func TestWithLanguagePrefix(t *testing.T) {
	tests := []struct {
		name  string
		parts []string
		idx   int
		want  string
	}{
		{"root is segment 0, already correct", []string{"HVqp3ms"}, 0, "HVqp3ms"},
		{"root at idx 1, prefix must be re-attached (real Gen.1.1#01 shape)", []string{"HR", "Ncfsa"}, 1, "HNcfsa"},
		{"root at idx 2, three-segment field", []string{"HC", "Td", "Ncfsa"}, 2, "HNcfsa"},
		{"Aramaic marker re-attached the same way", []string{"AR", "Ncfsa"}, 1, "ANcfsa"},
		{"Adjective POS code (leading 'A' is NOT a language marker): prefix still prepended", []string{"HTd", "Aampa"}, 1, "HAampa"},
		{"segment 0 has no recognized language letter: left alone", []string{"X", "Ncfsa"}, 1, "Ncfsa"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := withLanguagePrefix(tt.parts, tt.idx)
			if got != tt.want {
				t.Errorf("withLanguagePrefix(%v, %d) = %q, want %q", tt.parts, tt.idx, got, tt.want)
			}
		})
	}
}
