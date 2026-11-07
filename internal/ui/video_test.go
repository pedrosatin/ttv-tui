package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"strings"
	"testing"
)

func TestVideoLayoutCycle(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
		reason  string
		video   bool
	}{
		{"opt-in supported", true, "", true},
		{"no flag", false, "", false},
		{"no terminal", true, "video.no_terminal", false},
		{"no mpv", true, "video.no_mpv", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _, _ := setupTwoPanelModel(t)
			WithVideo(tc.enabled, tc.reason)(&m)
			m.width, m.height = 120, 32
			want := []LayoutMode{LayoutChat, LayoutList, LayoutFocus, LayoutSplit}
			if tc.video {
				want[3] = LayoutVideo
				want = append(want, LayoutSplit)
			}
			for range 2 {
				for _, mode := range want {
					m = sendLayoutKey(t, m, tea.KeyCtrlW)
					if m.LayoutMode() != mode {
						t.Fatalf("got %q want %q", m.LayoutMode(), mode)
					}
					if mode == LayoutVideo {
						plain := ansi.Strip(m.View())
						lines := strings.Split(plain, "\n")
						if !strings.Contains(lines[0], m.t("video.placeholder")) || !strings.Contains(lines[18], "Chat") || m.listVisible() || m.Focus() != FocusChat {
							t.Fatalf("invalid video layout:\n%s", plain)
						}
						if len(lines) != 32 {
							t.Fatalf("height=%d", len(lines))
						}
					}
				}
			}
			if tc.reason != "" {
				if !strings.Contains(ansi.Strip(m.StatusBar()), m.t(tc.reason)) {
					t.Fatalf("missing reason: %s", m.StatusBar())
				}
				m.helpVisible = true
				if !strings.Contains(strings.Join(m.renderHelpLines(120), "\n"), m.t(tc.reason)) {
					t.Fatal("help missing reason")
				}
			}
		})
	}
}

func TestVideoResizeAndToggles(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		for _, height := range []int{1, 2, 7, 14, 24, 40} {
			m, _, _ := setupTwoPanelModel(t)
			m.catalog, _ = i18n.New(lang)
			WithVideo(true, "")(&m)
			for range 4 {
				m = sendLayoutKey(t, m, tea.KeyCtrlW)
			}
			updated, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: height})
			m = updated.(Model)
			if len(strings.Split(m.View(), "\n")) != height {
				t.Fatalf("height %d: %s", height, m.View())
			}
			for _, key := range []tea.KeyType{tea.KeyCtrlH, tea.KeyCtrlL} {
				next := sendLayoutKey(t, m, key)
				if next.LayoutMode() == LayoutVideo {
					t.Fatal("toggle did not leave video")
				}
			}
		}
	}
}
