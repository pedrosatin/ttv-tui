package ui

import (
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

func TestChannelDensityIdentity(t *testing.T) {
	m := New(mustCatalog(t, i18n.LangPTBR))
	title := "0123456789abcdefghijklmnopqrstuv"
	ch := catalog.Channel{Login: "alanzoka", DisplayName: "Alanzoka", Title: &title, Viewers: catalog.IntPtr(999)}
	row := ansi.Strip(m.formatChannelRow("  ", ch, catalog.AvailabilityLive, 40))
	if strings.Contains(row, "(alanzoka)") || !strings.Contains(row, "0123456789") {
		t.Fatalf("80-column split must retain 10+ title cells: %q", row)
	}
	m.list.mode = ModeChannels
	m.list.channels = []catalog.Channel{ch}
	m.chat.panel.channel = ch.Login
	m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	view := ansi.Strip(m.View())
	if strings.Contains(view, "(alanzoka)") || !strings.Contains(view, "Alanzoka — 0123456789") {
		t.Fatalf("80-column split lost title cells: %s", view)
	}
	ch.DisplayName = "Outro nome"
	row = ansi.Strip(m.formatChannelRow("  ", ch, catalog.AvailabilityLive, 80))
	if !strings.Contains(row, "Outro nome (alanzoka)") {
		t.Fatal(row)
	}
	ch.DisplayName = ""
	row = ansi.Strip(m.formatChannelRow("  ", ch, catalog.AvailabilityLive, 80))
	if strings.Contains(row, "(alanzoka)") || !strings.Contains(row, "alanzoka") {
		t.Fatal(row)
	}
}

func TestChannelDensityViewers(t *testing.T) {
	m := New(mustCatalog(t, i18n.LangPTBR))
	for _, tc := range []struct {
		n    int
		want string
	}{{999, "999"}, {1000, "1.0k"}, {99950, "100k"}, {100000, "100k"}, {999949, "999k"}, {999950, "1.0M"}, {1000000, "1.0M"}, {100000000, "100M"}, {999950000, "1.0B"}} {
		got := m.formatViewers(&tc.n)
		if got != tc.want {
			t.Errorf("%d: got %s want %s", tc.n, got, tc.want)
		}
		cell := ansi.Strip(formatLiveViewersCell(got))
		if ansi.StringWidth(cell) != 7 || !strings.HasPrefix(cell, "● ") || !strings.Contains(cell, tc.want) {
			t.Errorf("broken cell: %q", cell)
		}
	}
	var nameAt int
	for i, v := range []*int{nil, catalog.IntPtr(999), catalog.IntPtr(1000), catalog.IntPtr(100000), catalog.IntPtr(999950)} {
		row := ansi.Strip(m.formatChannelRow("  ", catalog.Channel{Login: "Canal", DisplayName: "Canal", Viewers: v}, catalog.AvailabilityLive, 40))
		at := ansi.StringWidth(row[:strings.Index(row, "Canal")])
		if i == 0 {
			nameAt = at
		} else if at != nameAt {
			t.Errorf("name column moved: %q", row)
		}
		if strings.Contains(row, "— —") {
			t.Error(row)
		}
	}
}

func TestChannelDensitySelection(t *testing.T) {
	for _, noColor := range []bool{false, true} {
		t.Run(map[bool]string{false: "color", true: "NO_COLOR"}[noColor], func(t *testing.T) {
			if noColor {
				t.Setenv("NO_COLOR", "1")
				configureOrientationColors()
			} else {
				forceOrientationColorsForTest(true)
			}
			defer resetOrientationColorsForTest()
			m := New(mustCatalog(t, i18n.LangPTBR))
			m.list.mode = ModeChannels
			m.list.channels = []catalog.Channel{{ID: "1", Login: "Canal", DisplayName: "Canal", Viewers: catalog.IntPtr(999)}}
			m.list.highlightedID = "1"
			m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: 80, Height: 24})
			for _, row := range strings.Split(m.View(), "\n") {
				if strings.Contains(row, "Canal") && strings.Contains(row, "▸") {
					// No reset may end the selection before the name or the padding.
					name := strings.Index(row, "Canal")
					if !strings.Contains(row[:name], "\x1b[7m") && !strings.Contains(row[:name], ";7m") {
						t.Fatalf("no full-row reverse: %q", row)
					}
					if strings.Contains(row[:name], "\x1b[0m") {
						t.Fatalf("selection ended before name: %q", row)
					}
					if noColor && sgrColorRe.MatchString(row) {
						t.Fatalf("color under NO_COLOR: %q", row)
					}
					return
				}
			}
			t.Fatal("selected row missing")
		})
	}
}

func TestChannelDensityMarkers(t *testing.T) {
	m := New(mustCatalog(t, i18n.LangPTBR))
	m.list.mode = ModeSearch
	language := "pt"
	m.list.prefs.languages = []string{"pt"}
	m.list.channels = []catalog.Channel{{ID: "1", Login: "Short", DisplayName: "Short", Language: &language, Availability: catalog.AvailabilityOffline}, {ID: "2", Login: "LongerName", DisplayName: "LongerName", Language: &language}, {ID: "3", Login: "Live", DisplayName: "Live", Viewers: catalog.IntPtr(10)}}
	rows := m.renderListBodyLines("", 80)
	positions := []int{}
	for _, row := range rows {
		s := ansi.Strip(row)
		for _, tag := range []string{"[offline]", "[não verificado]", "[idioma?]"} {
			if at := strings.Index(s, tag); at >= 0 {
				positions = append(positions, ansi.StringWidth(s[:at]))
			}
		}
	}
	if len(positions) != 3 || positions[0] != positions[1] || positions[1] != positions[2] {
		t.Fatalf("unaligned markers %v: %v", positions, rows)
	}
}
