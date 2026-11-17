package store

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// specChannel lê "login" como canal da Twitch e "kick/login" como da Kick.
func specChannel(spec string) Channel {
	if plat, login, ok := strings.Cut(spec, "/"); ok {
		return Channel{Platform: plat, Login: login}
	}
	return Channel{Login: spec}
}

// recentSpecs são os recentes no formato de specChannel, na ordem.
func recentSpecs(st State) []string {
	out := make([]string, len(st.Recents))
	for i, ch := range st.Recents {
		out[i] = ch.Login
		if ch.CanonicalPlatform() != PlatformTwitch {
			out[i] = ch.CanonicalPlatform() + "/" + ch.Login
		}
	}
	return out
}

func seedSpecs(t *testing.T, path string, specs ...string) {
	t.Helper()
	var st State
	for i := len(specs) - 1; i >= 0; i-- {
		st.Recents = append([]Channel{specChannel(specs[i])}, st.Recents...)
	}
	if err := SaveTo(path, st); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadFrom(path); err != nil || !slices.Equal(recentSpecs(got), specs) {
		t.Fatalf("preparação: recentes %q, %v; quer %q", recentSpecs(got), err, specs)
	}
}

// numbered devolve prefix00, prefix01… até n-1, do mais novo ao mais antigo.
func numbered(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s%02d", prefix, i)
	}
	return out
}

// applyOps aplica "open:canal" e "forget:canal", separados por vírgula.
func applyOps(t *testing.T, st *State, ops string) {
	t.Helper()
	for op := range strings.SplitSeq(ops, ",") {
		verb, spec, _ := strings.Cut(op, ":")
		switch verb {
		case "open":
			st.AddRecent(specChannel(spec), DefaultMaxRecents)
		case "forget":
			if !st.ForgetRecent(specChannel(spec)) {
				t.Fatalf("ForgetRecent(%q): o canal não estava nos recentes", spec)
			}
		default:
			t.Fatalf("operação desconhecida %q", op)
		}
	}
}

// Duas instâncias carregam a mesma lista. Este processo e um subprocesso real
// mexem nos recentes e gravam, um depois do outro; o disco guarda o que cada
// um fez, sem que a cópia velha de um desfaça o que o outro gravou.
func TestStore_RecentsTwoInstances(t *testing.T) {
	if os.Getenv("TTV_STORE_TWO_HELPER") == "1" {
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
		applyOps(t, &st, os.Getenv("TTV_STORE_OPS"))
		if err := s.Save(st); err != nil {
			t.Fatal(err)
		}
		return
	}
	full := numbered("c", DefaultMaxRecents)
	for _, tc := range []struct {
		name        string
		seed        []string
		local, peer string
		// localFirst: este processo grava antes do subprocesso.
		localFirst bool
		want       []string
	}{
		{
			// O limite tirou c49 da lista local, mas o outro o reabriu e há
			// lugar para ele depois de c00 sair.
			name: "limite local não apaga o que o outro reabriu",
			seed: full, local: "open:novo,forget:c00", peer: "open:c49",
			want: slices.Concat([]string{"novo", "c49"}, full[1:49]),
		},
		{
			// O outro abre alpha, que já era o primeiro da cópia dele.
			name: "abrir o primeiro devolve o que o outro tirou",
			seed: []string{"alpha", "beta"}, local: "forget:alpha", peer: "open:alpha", localFirst: true,
			want: []string{"alpha", "beta"},
		},
		{
			name: "remoções independentes",
			seed: []string{"a", "b", "c", "d"}, local: "forget:b", peer: "forget:c", localFirst: true,
			want: []string{"a", "d"},
		},
		{
			name: "remoções independentes, ordem inversa",
			seed: []string{"a", "b", "c", "d"}, local: "forget:b", peer: "forget:c",
			want: []string{"a", "d"},
		},
		{
			// Mesmo login nas duas plataformas: tirar um não tira o outro, e
			// abrir o da Kick não devolve o da Twitch.
			name: "twitch e kick com o mesmo login",
			seed: []string{"kick/xqc", "xqc", "beta"}, local: "forget:xqc", peer: "open:kick/xqc", localFirst: true,
			want: []string{"kick/xqc", "beta"},
		},
		{
			name: "twitch e kick com o mesmo login, o outro reabre o da twitch",
			seed: []string{"kick/xqc", "xqc", "beta"}, local: "forget:kick/xqc", peer: "open:xqc", localFirst: true,
			want: []string{"xqc", "beta"},
		},
		{
			// Lista cheia nas duas: cada um abre um canal novo. Os dois
			// entram, e só os mais antigos saem pelo limite.
			name: "lista cheia, cada um abre um canal",
			seed: full, local: "open:a1", peer: "open:b1", localFirst: true,
			want: slices.Concat([]string{"b1", "a1"}, full[:48]),
		},
		{
			name: "lista cheia, um abre e tira, o outro abre",
			seed: full, local: "open:a1,forget:c10", peer: "open:b1", localFirst: true,
			want: slices.Concat([]string{"b1", "a1"}, full[:10], full[11:49]),
		},
		{
			name: "lista cheia, o outro grava primeiro",
			seed: full, local: "open:a1,forget:c10", peer: "open:b1",
			want: slices.Concat([]string{"a1", "b1"}, full[:10], full[11:49]),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			seedSpecs(t, path, tc.seed...)

			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestStore_RecentsTwoInstances$")
			cmd.Env = append(os.Environ(), "TTV_STORE_TWO_HELPER=1", "TTV_STORE_PATH="+path, "TTV_STORE_OPS="+tc.peer)
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

			// O subprocesso já carregou a lista; este carrega a mesma.
			s := New(path)
			st, err := s.Load()
			if err != nil {
				t.Fatal(err)
			}
			applyOps(t, &st, tc.local)
			if tc.localFirst {
				if err := s.Save(st); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := w.Write([]byte{1}); err != nil {
				t.Fatal(err)
			}
			_ = w.Close()
			if err := cmd.Wait(); err != nil {
				t.Fatal(err)
			}
			if !tc.localFirst {
				if err := s.Save(st); err != nil {
					t.Fatal(err)
				}
			}

			got, err := LoadFrom(path)
			if err != nil {
				t.Fatal(err)
			}
			if specs := recentSpecs(got); !slices.Equal(specs, tc.want) {
				t.Fatalf("recentes = %q\nquer       %q", specs, tc.want)
			}
		})
	}
}

// Na mesma sessão, o último gesto sobre o canal vale: aberto e depois tirado,
// ele não chega ao disco; tirado e depois aberto, volta na frente.
func TestStore_RecentOpenedAndForgottenInOneSession(t *testing.T) {
	for _, tc := range []struct {
		name string
		seed []string
		ops  string
		want []string
	}{
		{"abriu e tirou", []string{"a", "b"}, "open:x,forget:x", []string{"a", "b"}},
		{"tirou e abriu", []string{"a", "x", "b"}, "forget:x,open:x", []string{"x", "a", "b"}},
		{"abriu, tirou e abriu outro", []string{"a", "x"}, "open:y,forget:x", []string{"y", "a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			seedSpecs(t, path, tc.seed...)
			s := New(path)
			st, err := s.Load()
			if err != nil {
				t.Fatal(err)
			}
			applyOps(t, &st, tc.ops)
			if err := s.Save(st); err != nil {
				t.Fatal(err)
			}
			got, err := LoadFrom(path)
			if err != nil {
				t.Fatal(err)
			}
			if specs := recentSpecs(got); !slices.Equal(specs, tc.want) {
				t.Fatalf("recentes = %q, quer %q", specs, tc.want)
			}
		})
	}
}

// Um canal que o limite tirou da lista local continua no disco enquanto
// couber: abrir um canal novo com a lista cheia e tirar outro não apaga o
// mais antigo.
func TestStore_LimitEvictionIsNotARemoval(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	full := numbered("c", DefaultMaxRecents)
	seedSpecs(t, path, full...)
	s := New(path)
	st, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	applyOps(t, &st, "open:novo,forget:c00")
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := slices.Concat([]string{"novo"}, full[1:]); !slices.Equal(recentSpecs(got), want) {
		t.Fatalf("recentes = %q\nquer       %q", recentSpecs(got), want)
	}
}
