package app

import (
	"strings"
	"testing"
)

// Errors raised before the config is read take their language from the
// environment and still come from the catalog.
func TestRunUnsupportedLangErrorIsLocalized(t *testing.T) {
	cases := []struct {
		locale string
		want   string
	}{
		{"pt_BR.UTF-8", "não suportada"},
		{"en_US.UTF-8", "unsupported --lang"},
	}
	for _, tc := range cases {
		t.Run(tc.locale, func(t *testing.T) {
			t.Setenv("LC_ALL", tc.locale)
			err := Run(Options{Lang: "fr"})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Run error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}
