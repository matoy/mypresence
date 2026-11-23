package models

import "testing"

func TestFormatDisplayName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "standard", in: "Jonathan BECKER", want: "JBE - Jonathan BECKER"},
		{name: "lowercase", in: "jonathan becker", want: "JBE - jonathan becker"},
		{name: "extra_spaces", in: "  Jonathan   BECKER  ", want: "JBE - Jonathan   BECKER"},
		{name: "single_word", in: "admin", want: "admin"},
		{name: "single_word_trimmed", in: "  Alice  ", want: "Alice"},
		{name: "empty", in: "   ", want: ""},
		{name: "multi_word", in: "Marie De Gaulle", want: "MGA - Marie De Gaulle"},
		{name: "hyphenated_first", in: "Jean-Luc BECKER", want: "JBE - Jean-Luc BECKER"},
		{name: "short_last", in: "Luc A", want: "LA - Luc A"},
		{name: "utf8_accents", in: "Élodie Çelik", want: "ÉÇE - Élodie Çelik"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := FormatDisplayName(tc.in); got != tc.want {
				t.Errorf("FormatDisplayName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
