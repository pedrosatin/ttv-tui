package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPanelColumnsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := SaveTo(path, Config{ListColumns: 52, ChatColumns: 60, ChatOverlay: true, PanelsSwapped: true}); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ListColumns != 52 || got.ChatColumns != 60 || !got.ChatOverlay || !got.PanelsSwapped {
		t.Fatalf("voltou lista %d, chat %d", got.ListColumns, got.ChatColumns)
	}
}

func TestPanelColumnsBadValuesMeanAutomatic(t *testing.T) {
	for _, body := range []string{
		"list_columns = 5\nchat_columns = 900\n",
		"list_columns = \"largo\"\nchat_columns = 4.5\n",
		"list_columns = -3\nchat_columns = 0\n",
	} {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := LoadFrom(path)
		if err != nil {
			t.Fatalf("%q: valor ruim não pode impedir a abertura: %v", body, err)
		}
		if got.ListColumns != 0 || got.ChatColumns != 0 {
			t.Fatalf("%q: lista %d, chat %d", body, got.ListColumns, got.ChatColumns)
		}
	}
}

func TestAutomaticPanelColumnsAreNotWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := SaveTo(path, Config{}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "_columns") {
		t.Fatalf("largura automática foi gravada: %q", body)
	}
}

func TestChatOverlayAcceptsQuotedTrue(t *testing.T) {
	for body, want := range map[string]bool{
		"chat_overlay = true\n":     true,
		"chat_overlay = \"true\"\n": true,
		"chat_overlay = 'true'\n":   true,
		"chat_overlay = false\n":    false,
		"chat_overlay = 7\n":        false,
	} {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := LoadFrom(path)
		if err != nil {
			t.Fatalf("%q: %v", body, err)
		}
		if got.ChatOverlay != want {
			t.Fatalf("%q: overlay %v", body, got.ChatOverlay)
		}
	}
}

func TestChatOverlayOpacityRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := SaveTo(path, Config{ChatOverlayOpacity: 45}); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ChatOverlayOpacity != 45 {
		t.Fatalf("voltou %d", got.ChatOverlayOpacity)
	}
}

func TestChatOverlayOpacityBadValuesMeanDefault(t *testing.T) {
	for _, body := range []string{"chat_overlay_opacity = 5\n", "chat_overlay_opacity = 140\n", "chat_overlay_opacity = \"forte\"\n"} {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := LoadFrom(path)
		if err != nil {
			t.Fatalf("%q: %v", body, err)
		}
		if got.ChatOverlayOpacity != 0 {
			t.Fatalf("%q: %d", body, got.ChatOverlayOpacity)
		}
	}
}
