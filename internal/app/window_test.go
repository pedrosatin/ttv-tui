package app

import "testing"

func TestOpenWindowStaysInTheTerminalWhenAsked(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")
	t.Setenv(videoHostEnv, "")
	for name, opts := range map[string]Options{
		"--terminal":   {Terminal: true},
		"sem a janela": {}, // o binário de teste não tem ttv-tui-window ao lado
	} {
		if openWindow(opts) {
			t.Fatalf("%s: abriu a janela", name)
		}
	}
}

func TestOpenWindowNeverLoopsInsideTheWindow(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")
	t.Setenv(videoHostEnv, "/tmp/qualquer.sock")
	if openWindow(Options{}) {
		t.Fatal("dentro da janela o TUI não pode abrir outra")
	}
}

func TestOpenWindowNeedsWayland(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv(videoHostEnv, "")
	if openWindow(Options{}) {
		t.Fatal("sem Wayland o app fica no terminal")
	}
}
