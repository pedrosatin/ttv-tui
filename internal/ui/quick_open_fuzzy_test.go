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
		got, ok := fuzzyScore(foldForFuzzy(tc.query), tc.text)
		if tc.wantMatch && !ok {
			t.Errorf("fuzzyScore(%q, %q) = %d/%v, quer casamento", tc.query, tc.text, got, ok)
		}
		if !tc.wantMatch && ok {
			t.Errorf("fuzzyScore(%q, %q) = %d/%v, quer sem casamento", tc.query, tc.text, got, ok)
		}
	}

	// Prefixo vence subsequência espalhada; início de palavra vence meio.
	prefix, _ := fuzzyScore(foldForFuzzy("chat"), "Just Chatting")
	mid, _ := fuzzyScore(foldForFuzzy("chat"), "xchattery")
	if prefix <= mid {
		t.Fatalf("prefixo/palavra %d deveria superar meio %d", prefix, mid)
	}
	start, _ := fuzzyScore(foldForFuzzy("val"), "VALORANT")
	buried, _ := fuzzyScore(foldForFuzzy("val"), "xxvalyy")
	if start <= buried {
		t.Fatalf("início %d deveria superar enterrado %d", start, buried)
	}
}

func TestFuzzyScoreOrdemEntreCandidatos(t *testing.T) {
	query := foldForFuzzy("gau")
	type row struct {
		label string
		score int
		ok    bool
	}
	rows := []row{
		{"Gaules", 0, false},
		{"GaulesTV", 0, false},
		{"alanzoka", 0, false},
	}
	for i := range rows {
		rows[i].score, rows[i].ok = fuzzyScore(query, rows[i].label)
	}
	slices.SortFunc(rows, func(a, b row) int {
		if a.ok != b.ok {
			if a.ok {
				return -1
			}
			return 1
		}
		if a.score != b.score {
			return b.score - a.score
		}
		return 0
	})
	if !rows[0].ok || rows[0].label != "Gaules" {
		t.Fatalf("primeiro = %q ok=%v score=%d, quer Gaules; lista=%v", rows[0].label, rows[0].ok, rows[0].score, rows)
	}
	if rows[len(rows)-1].ok {
		t.Fatalf("alanzoka não deveria casar gau: %v", rows)
	}
}

func TestFuzzyScoreCasaTextoLongo(t *testing.T) {
	long := "The Legend of Zelda: Tears of the Kingdom"
	score, ok := fuzzyScore(foldForFuzzy("z"), long)
	if !ok {
		t.Fatalf("z contra %q deveria casar; score=%d", long, score)
	}
	if score < 0 {
		t.Fatalf("casamento válido não pode ser negativo: %d", score)
	}
	desc := "A very long category description that mentions Zelda somewhere in the middle of plenty of other words"
	score, ok = fuzzyScore(foldForFuzzy("zelda"), desc)
	if !ok || score < 0 {
		t.Fatalf("zelda contra descrição longa: ok=%v score=%d", ok, score)
	}
	_, ok = fuzzyScore(foldForFuzzy("qqq"), long)
	if ok {
		t.Fatal("qqq não deveria casar o título longo")
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
