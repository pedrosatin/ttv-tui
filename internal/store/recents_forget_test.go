package store

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

// recentLogins são os logins dos recentes, na ordem.
func recentLogins(st State) []string {
	out := make([]string, len(st.Recents))
	for i, ch := range st.Recents {
		out[i] = ch.Login
	}
	return out
}

func seedRecents(t *testing.T, path string, logins ...string) {
	t.Helper()
	var st State
	for i := len(logins) - 1; i >= 0; i-- {
		st.AddRecent(Channel{Login: logins[i]}, DefaultMaxRecents)
	}
	if err := SaveTo(path, st); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadFrom(path); err != nil || !slices.Equal(recentLogins(got), logins) {
		t.Fatalf("preparação: recentes %q, %v; quer %q", recentLogins(got), err, logins)
	}
}

// Esquecer um recente e gravar tira o canal do disco: reabrir o app não o
// traz de volta, e o resto fica na mesma ordem.
func TestStore_ForgetRecentSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	seedRecents(t, path, "alpha", "xqc", "beta")

	s := New(path)
	st, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !st.ForgetRecent(Channel{Login: "XQC"}) {
		t.Fatal("ForgetRecent não achou xqc pelo login em maiúsculas")
	}
	if st.ForgetRecent(Channel{Login: "xqc"}) {
		t.Fatal("ForgetRecent achou xqc depois de tirá-lo")
	}
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}

	reopened, err := New(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := recentLogins(reopened); !slices.Equal(got, []string{"alpha", "beta"}) {
		t.Fatalf("recentes depois de reabrir = %q, quer [alpha beta]", got)
	}
}

// O mesmo vale para quem grava pelo SaveTo um estado vindo de LoadFrom.
func TestStore_ForgetRecentWithSaveTo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	seedRecents(t, path, "alpha", "xqc", "beta")
	st, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	st.ForgetRecent(Channel{Login: "xqc"})
	if err := SaveTo(path, st); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if logins := recentLogins(got); !slices.Equal(logins, []string{"alpha", "beta"}) {
		t.Fatalf("recentes = %q, quer [alpha beta]", logins)
	}
}

// Outro processo carregou a lista antes da remoção e grava depois dela, com o
// xqc ainda na memória. A cópia velha não ressuscita o recente esquecido; o que
// esse processo fez desde a carga dele (abrir outro canal, favoritar) entra.
// Se ele abre o xqc de novo, aí sim o canal volta, na frente.
func TestStore_ForgottenRecentSurvivesStaleWriter(t *testing.T) {
	if os.Getenv("TTV_STORE_RECENT_HELPER") == "1" {
		s := New(os.Getenv("TTV_STORE_PATH"))
		st, err := s.Load()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stdout.Write([]byte{1}); err != nil {
			t.Fatal(err)
		}
		var signal [1]byte
		if _, err := os.Stdin.Read(signal[:]); err != nil {
			t.Fatal(err)
		}
		switch op := os.Getenv("TTV_STORE_OP"); op {
		case "favorite":
			st.ToggleFavorite(Channel{Login: "gamma"})
		default:
			st.AddRecent(Channel{Login: op}, s.MaxRecents())
		}
		if err := s.Save(st); err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, tc := range []struct {
		op   string
		want []string
	}{
		{"favorite", []string{"alpha", "beta"}},
		{"gamma", []string{"gamma", "alpha", "beta"}},
		{"beta", []string{"beta", "alpha"}},
		{"xqc", []string{"xqc", "alpha", "beta"}},
	} {
		t.Run(tc.op, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			seedRecents(t, path, "alpha", "xqc", "beta")

			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestStore_ForgottenRecentSurvivesStaleWriter$")
			cmd.Env = append(os.Environ(), "TTV_STORE_RECENT_HELPER=1", "TTV_STORE_PATH="+path, "TTV_STORE_OP="+tc.op)
			cmd.Stdin = r
			cmd.Stderr = os.Stderr
			ready, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = cmd.Process.Kill(); _ = w.Close(); _ = ready.Close() })
			_ = r.Close()
			var signal [1]byte
			if _, err := ready.Read(signal[:]); err != nil {
				t.Fatal(err)
			}

			// O outro processo já carregou [alpha xqc beta]; este esquece o xqc.
			s := New(path)
			st, err := s.Load()
			if err != nil {
				t.Fatal(err)
			}
			st.ForgetRecent(Channel{Login: "xqc"})
			if err := s.Save(st); err != nil {
				t.Fatal(err)
			}

			if _, err := w.Write([]byte{1}); err != nil {
				t.Fatal(err)
			}
			_ = w.Close()
			if err := cmd.Wait(); err != nil {
				t.Fatal(err)
			}
			got, err := LoadFrom(path)
			if err != nil {
				t.Fatal(err)
			}
			if logins := recentLogins(got); !slices.Equal(logins, tc.want) {
				t.Fatalf("recentes = %q, quer %q", logins, tc.want)
			}
			if tc.op == "favorite" && !got.IsFavorite(Channel{Login: "gamma"}) {
				t.Fatalf("o favorito do outro processo se perdeu: %+v", got.Favorites)
			}
		})
	}
}

// Uma gravação que falha deixa a remoção pendente para a próxima. Depois de
// confirmada, ela não apaga o canal que outro processo abriu em seguida.
func TestStore_ForgetRecentRetriedAfterFailedSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	seedRecents(t, path, "alpha", "xqc")
	s := New(path)
	st, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	st.ForgetRecent(Channel{Login: "xqc"})
	if err := os.WriteFile(path+".lock", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(st); err == nil {
		t.Fatal("quer erro de trava")
	}
	if err := os.Remove(path + ".lock"); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if logins := recentLogins(got); !slices.Equal(logins, []string{"alpha"}) {
		t.Fatalf("recentes depois da nova tentativa = %q, quer [alpha]", logins)
	}

	other, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	other.AddRecent(Channel{Login: "xqc"}, DefaultMaxRecents)
	if err := SaveTo(path, other); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	got, err = LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if logins := recentLogins(got); !slices.Equal(logins, []string{"xqc", "alpha"}) {
		t.Fatalf("a remoção antiga apagou o xqc aberto depois: %q", logins)
	}
}

// Com o arquivo corrompido durante a sessão, a memória é a única fonte: a
// gravação guarda os recentes locais, sem o esquecido.
func TestStore_ForgetRecentWhenDiskCorrupted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	seedRecents(t, path, "alpha", "xqc", "beta")
	s := New(path)
	st, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	st.ForgetRecent(Channel{Login: "xqc"})
	if err := os.WriteFile(path, []byte("{broken json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if logins := recentLogins(got); !slices.Equal(logins, []string{"alpha", "beta"}) {
		t.Fatalf("recentes = %q, quer [alpha beta]", logins)
	}
}

// Os recentes abertos desde a carga vêm na frente e o disco completa a lista,
// na ordem dele. Um estado montado na mão, sem carga, só acrescenta.
func TestMergeRecents(t *testing.T) {
	ch := func(logins ...string) []Channel {
		out := make([]Channel, len(logins))
		for i, l := range logins {
			out[i] = Channel{Login: l}
		}
		return out
	}
	for _, tc := range []struct {
		name               string
		local, acked, disk []Channel
		want               []string
	}{
		{"sem carga", ch("new", "b"), nil, ch("a", "b", "c"), []string{"new", "b", "a", "c"}},
		{"nada mudou", ch("a", "b"), ch("a", "b"), ch("d", "a", "b"), []string{"d", "a", "b"}},
		{"subiu o segundo", ch("b", "a", "c"), ch("a", "b", "c"), ch("d", "a", "b", "c"), []string{"b", "d", "a", "c"}},
		{"esqueceu no meio", ch("a", "c"), ch("a", "b", "c"), ch("a", "b", "c"), []string{"a", "c"}},
		{"outro esqueceu", ch("a", "b", "c"), ch("a", "b", "c"), ch("a", "c"), []string{"a", "c"}},
		{"abriu o que outro esqueceu", ch("b", "a", "c"), ch("a", "b", "c"), ch("a", "c"), []string{"b", "a", "c"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := recentLogins(State{Recents: mergeRecents(tc.local, tc.acked, tc.disk)})
			if !slices.Equal(got, tc.want) {
				t.Fatalf("mergeRecents = %q, quer %q", got, tc.want)
			}
		})
	}
}
