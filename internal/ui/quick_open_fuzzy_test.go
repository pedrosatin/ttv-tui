package ui

import (
	"slices"
	"testing"
)

func TestFuzzyScoreOrdemECasamento(t *testing.T) {
	cases := []struct {
		query, text string
		wantMatch   bool
	}{
		{"gau", "Gaules", true},
		{"gls", "Gaules", true},
		{"xyz", "Gaules", false},
		{"", "Gaules", true},
		{"GAU", "gaules", true},
		{"jose", "José", true},
		{"JOSE", "josé", true},
		{"acao", "Ação", true},
	}
	for _, tc := range cases {
		got := fuzzyScore(tc.query, tc.text)
		if tc.wantMatch && got < 0 {
			t.Errorf("fuzzyScore(%q, %q) = %d, quer casamento", tc.query, tc.text, got)
		}
		if !tc.wantMatch && got >= 0 {
			t.Errorf("fuzzyScore(%q, %q) = %d, quer sem casamento", tc.query, tc.text, got)
		}
	}

	// Prefixo vence subsequência espalhada; início de palavra vence meio.
	prefix := fuzzyScore("chat", "Just Chatting")
	mid := fuzzyScore("chat", "xchattery")
	if prefix <= mid {
		t.Fatalf("prefixo/palavra %d deveria superar meio %d", prefix, mid)
	}
	start := fuzzyScore("val", "VALORANT")
	buried := fuzzyScore("val", "xxvalyy")
	if start <= buried {
		t.Fatalf("início %d deveria superar enterrado %d", start, buried)
	}
}

func TestFuzzyScoreOrdemEntreCandidatos(t *testing.T) {
	query := "gau"
	type row struct {
		label string
		score int
	}
	rows := []row{
		{"Gaules", fuzzyScore(query, "Gaules")},
		{"GaulesTV", fuzzyScore(query, "GaulesTV")},
		{"alanzoka", fuzzyScore(query, "alanzoka")},
	}
	slices.SortFunc(rows, func(a, b row) int {
		if a.score != b.score {
			return b.score - a.score
		}
		return 0
	})
	if rows[0].label != "Gaules" {
		t.Fatalf("primeiro = %q (score %d), quer Gaules; lista=%v", rows[0].label, rows[0].score, rows)
	}
	if rows[len(rows)-1].label != "alanzoka" || rows[len(rows)-1].score >= 0 {
		// alanzoka não casa "gau"
		if fuzzyScore(query, "alanzoka") >= 0 {
			t.Fatalf("alanzoka não deveria casar gau")
		}
	}
}

func TestFoldForFuzzyAcentosEMaiusculas(t *testing.T) {
	if foldForFuzzy("José") != foldForFuzzy("JOSE") {
		t.Fatalf("José e JOSE deveriam ser iguais: %q vs %q", foldForFuzzy("José"), foldForFuzzy("JOSE"))
	}
	if foldForFuzzy("Ação") != foldForFuzzy("acao") {
		t.Fatalf("Ação e acao deveriam ser iguais: %q vs %q", foldForFuzzy("Ação"), foldForFuzzy("acao"))
	}
}
