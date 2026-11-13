package player

import "testing"

func TestArgvCarriesEndpoint(t *testing.T) {
	const sock = "/run/user/1000/ttv-tui/1-123/i"
	const spaced = "/tmp/a b/ttv-tui-1-9/i"
	for _, tc := range []struct {
		nome     string
		argv     []string
		endpoint string
		want     bool
	}{
		{"mpv com o argumento exato", []string{"mpv", "--no-config", "--input-ipc-server=" + sock}, sock, true},
		{"streamlink com --player-args", []string{"python3", "/usr/bin/streamlink", "--player-args", buildPlayerArgs(mpvArgs(sock, ""))}, sock, true},
		{"streamlink com --player-args=", []string{"streamlink", "--player-args=" + buildPlayerArgs(mpvArgs(sock, ""))}, sock, true},
		{"--player-args com caminho citado", []string{"streamlink", "--player-args", buildPlayerArgs(mpvArgs(spaced, ""))}, spaced, true},
		{"socket em --title", []string{"mpv", "--title=" + sock}, sock, false},
		{"socket solto", []string{"cat", sock}, sock, false},
		{"socket como prefixo de outro caminho", []string{"mpv", "--input-ipc-server=" + sock + ".bak"}, sock, false},
		{"outro socket com o nosso como sufixo", []string{"mpv", "--input-ipc-server=/x" + sock}, sock, false},
		{"--input-ipc-server em duas palavras", []string{"mpv", "--input-ipc-server", sock}, sock, false},
		{"socket dentro de --player-args sem a opção", []string{"streamlink", "--player-args", "--title=" + sock}, sock, false},
		{"--player-args com aspas sem fim", []string{"streamlink", "--player-args", "'--input-ipc-server=" + sock}, sock, false},
		{"--input-ipc-server fora de --player-args", []string{"streamlink", "--title", "--input-ipc-server=" + sock + " x"}, sock, false},
		{"--player-args no fim do argv", []string{"streamlink", "--player-args"}, sock, false},
		{"endpoint vazio", []string{"mpv", "--input-ipc-server="}, "", false},
	} {
		t.Run(tc.nome, func(t *testing.T) {
			if got := argvCarriesEndpoint(tc.argv, tc.endpoint); got != tc.want {
				t.Fatalf("argvCarriesEndpoint(%q, %q) = %v, want %v", tc.argv, tc.endpoint, got, tc.want)
			}
		})
	}
}
