package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Recente ou favorito com identificador fora da gramática sai no carregamento:
// reaberto, viraria JOIN e PRIVMSG com linhas ou alvos extras.
func TestLoadFromDescartaIdentificadorInvalido(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	const fixture = `{
		"favorites": [
			{"platform": "twitch", "id": "1", "login": "gaules"},
			{"platform": "twitch", "id": "a,#b", "login": "a,#b"},
			{"platform": "kick", "id": "..", "login": ".."},
			{"platform": "kick", "id": "xqc", "login": "xqc"}
		],
		"recents": [
			{"platform": "twitch", "id": "x", "login": "dummychan\r\nprivmsg #victimchan :injected text\r\nping"},
			{"platform": "twitch", "id": "y", "login": "a b"},
			{"platform": "twitch", "id": "z", "login": "a\u0000b"},
			{"platform": "", "id": "Alanzoka", "login": "Alanzoka"},
			{"platform": "twitch", "id": "twitch:xqc", "login": ""},
			{"platform": "youtube", "id": "v\u001b[2J", "login": "v\u001b[2J"},
			{"platform": "youtube", "id": "dQw4w9WgXcQ", "login": "dQw4w9WgXcQ"}
		]
	}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}

	st, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	logins := func(chs []Channel) []string {
		out := make([]string, 0, len(chs))
		for _, c := range chs {
			out = append(out, c.CanonicalPlatform()+"/"+c.ToChannel().StableID())
		}
		return out
	}
	if got, want := strings.Join(logins(st.Favorites), " "), "twitch/gaules kick/xqc"; got != want {
		t.Fatalf("favoritos = %q, want %q", got, want)
	}
	if got, want := strings.Join(logins(st.Recents), " "), "twitch/Alanzoka twitch/xqc youtube/dQw4w9WgXcQ"; got != want {
		t.Fatalf("recentes = %q, want %q", got, want)
	}
	if got := st.Discarded(); got != 6 {
		t.Fatalf("Discarded = %d, want 6", got)
	}

	// A próxima gravação reescreve o arquivo sem os descartados.
	if err := SaveTo(path, st); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"victimchan", "a,#b", `".."`, "a b", `\u0000`, `\u001b`} {
		if strings.Contains(string(data), bad) {
			t.Fatalf("state.json ainda tem %q:\n%s", bad, data)
		}
	}
	again, err := LoadFrom(path)
	if err != nil || again.Discarded() != 0 {
		t.Fatalf("recarga: Discarded = %d, err = %v; want 0, nil", again.Discarded(), err)
	}
}

// A Kick guarda gravação e clip com subcaminho no identificador; o carregamento
// os mantém e descarta só o que foge da gramática do slug ou leva controle.
func TestLoadFromKickComSubcaminho(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	const fixture = `{
		"favorites": [],
		"recents": [
			{"platform": "kick", "id": "xqc/videos/1234", "login": "xqc/videos/1234"},
			{"platform": "kick", "id": "xqc/clip/abc", "login": "xqc/clip/abc"},
			{"platform": "kick", "id": "xqc/../x", "login": "xqc/../x"},
			{"platform": "kick", "id": "xqc/a\u001bb", "login": "xqc/a\u001bb"},
			{"platform": "kick", "id": "x.y/videos/1", "login": "x.y/videos/1"}
		]
	}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	var got []string
	for _, ch := range st.Recents {
		got = append(got, ch.Login)
	}
	if want := "xqc/videos/1234,xqc/clip/abc"; strings.Join(got, ",") != want {
		t.Fatalf("recentes = %v; quero %s", got, want)
	}
	if st.Discarded() != 3 {
		t.Fatalf("Discarded = %d; quero 3", st.Discarded())
	}
}
