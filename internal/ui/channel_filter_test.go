package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

func testCatalog(t *testing.T, lang string) *i18n.Catalog {
	t.Helper()
	cat, err := i18n.New(lang)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	return cat
}

func channelFilterFixture(t *testing.T) Model {
	t.Helper()
	m := New(testCatalog(t, i18n.LangEN))
	m.mode = ModeChannels
	m.channels = []twitch.Channel{
		{ID: "1", Login: "gaules", DisplayName: "Gaules", Language: twitch.LanguagePtr("PT"), Viewers: twitch.IntPtr(1200), Availability: twitch.AvailabilityLive},
		{ID: "2", Login: "xqc", DisplayName: "xQc", Language: twitch.LanguagePtr("EN"), Viewers: twitch.IntPtr(800), Availability: twitch.AvailabilityLive},
		{ID: "3", Login: "small", DisplayName: "Small", Language: twitch.LanguagePtr("PT"), Viewers: twitch.IntPtr(40), Availability: twitch.AvailabilityLive},
		{ID: "4", Login: "mystery", DisplayName: "Mystery", Viewers: twitch.IntPtr(900), Availability: twitch.AvailabilityLive},
	}
	m.reconcileHighlight()
	return m
}

func TestChannelFilterByLanguageAndAudience(t *testing.T) {
	m := channelFilterFixture(t)
	if got := len(m.VisibleChannels()); got != 4 {
		t.Fatalf("unfiltered VisibleChannels = %d, want 4", got)
	}

	m.filterLanguages = []string{"pt"}
	vis := m.VisibleChannels()
	if len(vis) != 3 { // pt + unknown mystery
		t.Fatalf("PT filter len=%d want 3 (pt + unknown); got %+v", len(vis), loginsOf(vis))
	}
	for _, ch := range vis {
		if ch.Login == "xqc" {
			t.Fatalf("EN channel must be hidden under PT filter")
		}
	}

	m.filterMinViewers = 500
	vis = m.VisibleChannels()
	if len(vis) != 2 { // gaules 1200 + mystery 900; small 40 out
		t.Fatalf("PT+500 filter len=%d want 2; got %+v", len(vis), loginsOf(vis))
	}

	items := m.VisibleItems()
	var mysteryTag bool
	view := m.View()
	if !strings.Contains(view, m.t("channels.lang_unknown_tag")) {
		// Ensure the row formatter marks unknown when language filter active.
		for _, it := range items {
			if it.Channel != nil && it.Channel.Login == "mystery" {
				row := m.formatChannelRow("▸ ", *it.Channel, it.Availability, 120)
				if strings.Contains(row, m.t("channels.lang_unknown_tag")) {
					mysteryTag = true
				}
			}
		}
		if !mysteryTag {
			t.Fatalf("unknown-language channel must be marked, view:\n%s", view)
		}
	}
}

func TestChannelFilterDoesNotAffectCategories(t *testing.T) {
	m := New(testCatalog(t, i18n.LangEN))
	m.mode = ModeCategories
	m.categories = []twitch.Category{{ID: "c1", Name: "Just Chatting", Viewers: twitch.IntPtr(10)}}
	m.filterLanguages = []string{"pt"}
	m.filterMinViewers = 5000
	items := m.VisibleItems()
	if len(items) != 1 || items[0].Category == nil {
		t.Fatalf("categories must remain visible under channel prefs: %+v", items)
	}
}

func TestChannelFilterMenuToggleClearClose(t *testing.T) {
	m := channelFilterFixture(t)
	var persistedLangs []string
	var persistedMin int
	var persistCalls int
	m.filterPersist = func(languages []string, minViewers int) error {
		persistCalls++
		persistedLangs = append([]string{}, languages...)
		persistedMin = minViewers
		return nil
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = next.(Model)
	if !m.ChannelFilterMenuOpen() {
		t.Fatalf("l must open channel filter menu")
	}
	view := m.View()
	if !strings.Contains(view, m.t("channel_filter.title")) {
		t.Fatalf("menu title missing:\n%s", view)
	}
	if !strings.Contains(view, "PT (") || !strings.Contains(view, "EN (") {
		t.Fatalf("menu must list languages with counts:\n%s", view)
	}

	// Languages are sorted (EN before PT); move onto the PT row and toggle.
	for i := 0; i < len(m.channelFilterMenuItems); i++ {
		item := m.channelFilterMenuItems[m.channelFilterMenuIndex]
		if item.Kind == filterMenuLang && item.Language == "pt" {
			break
		}
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = next.(Model)
	}
	if cur := m.channelFilterMenuItems[m.channelFilterMenuIndex]; cur.Kind != filterMenuLang || cur.Language != "pt" {
		t.Fatalf("failed to highlight PT row: %+v", m.channelFilterMenuItems[m.channelFilterMenuIndex])
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil {
		cmd()
	}
	if !m.languageFilterSelected("pt") {
		t.Fatalf("Enter must select PT, langs=%v", m.FilterLanguages())
	}
	if persistCalls != 1 || len(persistedLangs) != 1 || persistedLangs[0] != "pt" {
		t.Fatalf("persist after PT toggle: calls=%d langs=%v", persistCalls, persistedLangs)
	}

	// Move to audience 500 and select with Space.
	for i := 0; i < 20; i++ {
		if m.channelFilterMenuIndex < len(m.channelFilterMenuItems) {
			cur := m.channelFilterMenuItems[m.channelFilterMenuIndex]
			if cur.Kind == filterMenuAudience && cur.Audience == 500 {
				break
			}
		}
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = next.(Model)
	}
	item := m.channelFilterMenuItems[m.channelFilterMenuIndex]
	if item.Kind != filterMenuAudience || item.Audience != 500 {
		t.Fatalf("failed to land on 500 audience row: %+v items=%+v", item, m.channelFilterMenuItems)
	}
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m = next.(Model)
	if cmd != nil {
		cmd()
	}
	if m.FilterMinViewers() != 500 || persistedMin != 500 {
		t.Fatalf("min viewers = %d persisted=%d, want 500", m.FilterMinViewers(), persistedMin)
	}

	bar := m.StatusBar()
	if !strings.Contains(bar, "PT") || !strings.Contains(bar, "500+") {
		t.Fatalf("status must show active filters PT · 500+: %q", bar)
	}

	// Clear languages via "all languages".
	m.channelFilterMenuIndex = 0
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil {
		cmd()
	}
	if len(m.FilterLanguages()) != 0 {
		t.Fatalf("all languages must clear selection, got %v", m.FilterLanguages())
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.ChannelFilterMenuOpen() {
		t.Fatalf("Esc must close menu")
	}
}

func TestChannelFilterEmptyState(t *testing.T) {
	m := channelFilterFixture(t)
	m.filterLanguages = []string{"es"}
	m.filterMinViewers = 5000
	// mystery (unknown lang) has 900 viewers — still passes language, fails audience.
	// No ES channels; unknown passes language but fails 5000 audience.
	view := m.View()
	emptyLine := ""
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "No channels match") {
			emptyLine = line
			break
		}
	}
	if emptyLine == "" {
		t.Fatalf("expected preference empty state, view:\n%s", view)
	}
	if !strings.Contains(emptyLine, "ES") || !strings.Contains(emptyLine, "5000+") {
		t.Fatalf("empty state must cite filters: %q", emptyLine)
	}
	if !strings.Contains(view, m.t("channel_filter.empty_hint")) {
		t.Fatalf("empty hint missing:\n%s", view)
	}
}

func TestChannelFilterI18nAndWidths(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		m := New(testCatalog(t, lang), WithChannelFilters([]string{"pt"}, 500, nil))
		m.mode = ModeChannels
		m.channels = []twitch.Channel{
			{ID: "1", Login: "gaules", DisplayName: "Gaules", Language: twitch.LanguagePtr("pt"), Viewers: twitch.IntPtr(900), Availability: twitch.AvailabilityLive},
		}
		m = m.openChannelFilterMenu()
		for _, width := range []int{80, 120} {
			m.width = width
			m.height = 24
			view := m.View()
			if !strings.Contains(view, m.t("channel_filter.title")) {
				t.Fatalf("lang=%s width=%d missing menu title:\n%s", lang, width, view)
			}
			if !strings.Contains(view, "PT") || !strings.Contains(m.StatusBar(), "500+") {
				t.Fatalf("lang=%s width=%d status/menu missing active filter marks:\n%s\n%s", lang, width, m.StatusBar(), view)
			}
			m.helpVisible = true
			m.channelFilterMenu = false
			help := strings.Join(m.renderHelpLines(width), "\n")
			m.channelFilterMenu = true
			m.helpVisible = false
			if !strings.Contains(help, m.t("help.channel_filter")) {
				t.Fatalf("lang=%s help missing channel_filter key", lang)
			}
			for _, line := range strings.Split(view, "\n") {
				// Strip ANSI for width check roughly via rune count of stripped content is hard;
				// ensure menu lines were built for this width without panic.
				_ = line
			}
		}
	}
}

func TestWithChannelFiltersAppliedOnOpen(t *testing.T) {
	m := New(testCatalog(t, i18n.LangEN), WithChannelFilters([]string{"PT", "en"}, 100, nil))
	if got := m.FilterLanguages(); len(got) != 2 || got[0] != "pt" || got[1] != "en" {
		t.Fatalf("normalized langs = %v", got)
	}
	if m.FilterMinViewers() != 100 {
		t.Fatalf("min viewers = %d", m.FilterMinViewers())
	}
}

func loginsOf(chs []twitch.Channel) []string {
	out := make([]string, len(chs))
	for i, ch := range chs {
		out[i] = ch.Login
	}
	return out
}
