package store

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCategoriasLegadoEGravacaoAtomica(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"favorites":[],"recents":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	s := New(path)
	st, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.FavoriteCategories) != 0 {
		t.Fatal(st.FavoriteCategories)
	}
	st.FavoriteCategories = []Category{{ID: "42", Name: "VALORANT"}, {Platform: "kick", ID: "42", Name: "Kick"}}
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.FavoriteCategories) != 2 || got.FavoriteCategories[0].Platform != "twitch" {
		t.Fatal(got.FavoriteCategories)
	}
	entries, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".state-*.json.tmp"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporários = %v: %v", entries, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// O Windows não tem bits de permissão Unix: os.Stat devolve 0666.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	// O rename falha quando o destino é um diretório, e writeState apaga o temporário.
	blocked := filepath.Join(t.TempDir(), "state.json")
	if err := os.Mkdir(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeState(blocked, st); err == nil {
		t.Fatal("rename deveria falhar")
	}
	entries, err = filepath.Glob(filepath.Join(filepath.Dir(blocked), ".state-*.json.tmp"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporários após erro = %v: %v", entries, err)
	}
}

func TestCategoriasMergeEntreInstancias(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	a, b := New(path), New(path)
	sa, err := a.Load()
	if err != nil {
		t.Fatal(err)
	}
	sb, err := b.Load()
	if err != nil {
		t.Fatal(err)
	}
	sa.FavoriteCategories = []Category{{Platform: "twitch", ID: "42", Name: "A"}}
	sb.FavoriteCategories = []Category{{Platform: "kick", ID: "42", Name: "B"}}
	if err := a.Save(sa); err != nil {
		t.Fatal(err)
	}
	if err := b.Save(sb); err != nil {
		t.Fatal(err)
	}
	sa.FavoriteCategories = nil
	if err := a.Save(sa); err != nil {
		t.Fatal(err)
	}
	if err := b.Save(sb); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.FavoriteCategories) != 1 || got.FavoriteCategories[0].Platform != "kick" {
		t.Fatal(got.FavoriteCategories)
	}
}

func TestCategoriasSemNome(t *testing.T) {
	got := normalizeCategories([]Category{{ID: "42"}, {}, {Name: "Sem ID"}})
	if len(got) != 1 || got[0].Name != "42" || got[0].ID != "42" {
		t.Fatalf("categorias normalizadas = %+v", got)
	}
}
