package store

import (
	"path/filepath"
	"slices"
	"testing"
)

// clearPlatformRecents esquece cada recente de plat, como a UI faz ao limpar a
// lista de uma plataforma: uma remoção por canal no diário.
func clearPlatformRecents(st *State, plat string) {
	for _, ch := range slices.Clone(st.Recents) {
		if ch.CanonicalPlatform() == plat {
			st.ForgetRecent(ch)
		}
	}
}

// recentSpecsOf são os recentes no formato plataforma:login, na ordem.
func recentSpecsOf(st State) []string {
	out := make([]string, len(st.Recents))
	for i, ch := range st.Recents {
		out[i] = ch.CanonicalPlatform() + ":" + ch.Login
	}
	return out
}

// seedMixedRecents grava três recentes da Twitch e um da Kick, nesta ordem:
// alpha, kickzinho (Kick), beta, gamma.
func seedMixedRecents(t *testing.T, path string) {
	t.Helper()
	var st State
	for _, ch := range []Channel{
		{Login: "gamma"},
		{Login: "beta"},
		{Platform: PlatformKick, Login: "kickzinho"},
		{Login: "alpha"},
	} {
		st.AddRecent(ch, DefaultMaxRecents)
	}
	if err := SaveTo(path, st); err != nil {
		t.Fatal(err)
	}
}

func loadSpecs(t *testing.T, path string) []string {
	t.Helper()
	st, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	return recentSpecsOf(st)
}

// Limpar os recentes da Twitch e gravar deixa só os da Kick, também depois de
// reabrir o app.
func TestStore_ClearRecentsSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	seedMixedRecents(t, path)

	s := New(path)
	st, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	clearPlatformRecents(&st, PlatformTwitch)
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	if got := loadSpecs(t, path); !slices.Equal(got, []string{"kick:kickzinho"}) {
		t.Fatalf("recentes depois de reabrir = %q, quer só o da Kick", got)
	}
}

// Outra instância carregou a lista antes da limpeza e grava depois dela, ainda
// com os canais na memória. A cópia velha não traz nenhum de volta; o que ela
// fez depois da limpeza fica: o favorito, o canal novo que abriu e até um dos
// limpos, se ela o abrir de novo. Gravações seguintes da instância que limpou
// não apagam o que a outra abriu.
func TestStore_ClearRecentsSurvivesStaleWriter(t *testing.T) {
	for _, tc := range []struct {
		name string
		op   func(st *State)
		want []string
	}{
		{"favorito", func(st *State) { st.ToggleFavorite(Channel{Login: "delta"}) }, []string{"kick:kickzinho"}},
		{"abre canal novo", func(st *State) { st.AddRecent(Channel{Login: "delta"}, DefaultMaxRecents) }, []string{"twitch:delta", "kick:kickzinho"}},
		{"reabre canal limpo", func(st *State) { st.AddRecent(Channel{Login: "beta"}, DefaultMaxRecents) }, []string{"twitch:beta", "kick:kickzinho"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			seedMixedRecents(t, path)

			other := New(path)
			stale, err := other.Load()
			if err != nil {
				t.Fatal(err)
			}

			s := New(path)
			st, err := s.Load()
			if err != nil {
				t.Fatal(err)
			}
			clearPlatformRecents(&st, PlatformTwitch)
			if err := s.Save(st); err != nil {
				t.Fatal(err)
			}

			tc.op(&stale)
			if err := other.Save(stale); err != nil {
				t.Fatal(err)
			}
			if got := loadSpecs(t, path); !slices.Equal(got, tc.want) {
				t.Fatalf("recentes depois da outra instância = %q, quer %q", got, tc.want)
			}

			// A instância que limpou grava outra coisa; a limpeza já confirmada
			// não vale de novo.
			st.ToggleFavorite(Channel{Login: "epsilon"})
			if err := s.Save(st); err != nil {
				t.Fatal(err)
			}
			got, err := LoadFrom(path)
			if err != nil {
				t.Fatal(err)
			}
			if specs := recentSpecsOf(got); !slices.Equal(specs, tc.want) {
				t.Fatalf("recentes depois de nova gravação de quem limpou = %q, quer %q", specs, tc.want)
			}
			if !got.IsFavorite(Channel{Login: "epsilon"}) {
				t.Fatalf("favorito de quem limpou se perdeu: %+v", got.Favorites)
			}
			if tc.name == "favorito" && !got.IsFavorite(Channel{Login: "delta"}) {
				t.Fatalf("favorito da outra instância se perdeu: %+v", got.Favorites)
			}
		})
	}
}

// A outra instância abre um canal e grava enquanto a limpeza ainda está só na
// memória desta. Quem limpou não conhecia esse canal: ele fica no disco.
func TestStore_ClearRecentsKeepsChannelOpenedBeforeItsSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	seedMixedRecents(t, path)

	s := New(path)
	st, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	clearPlatformRecents(&st, PlatformTwitch)

	other := New(path)
	ost, err := other.Load()
	if err != nil {
		t.Fatal(err)
	}
	ost.AddRecent(Channel{Login: "delta"}, DefaultMaxRecents)
	if err := other.Save(ost); err != nil {
		t.Fatal(err)
	}

	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	if got := loadSpecs(t, path); !slices.Equal(got, []string{"twitch:delta", "kick:kickzinho"}) {
		t.Fatalf("recentes = %q, quer delta e o da Kick", got)
	}
}
