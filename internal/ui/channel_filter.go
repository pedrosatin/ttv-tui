package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

// Audience thresholds offered in the channel filter menu. 0 clears the floor.
var channelAudienceThresholds = []int{0, 100, 500, 1000, 5000}

// ChannelFilterPersistFunc writes the active channel preference filters to
// durable config. A nil persist skips disk writes (tests).
type ChannelFilterPersistFunc func(languages []string, minViewers int) error

// WithChannelFilters sets the initial language/audience preferences and an
// optional persist callback used when the user changes them in the menu.
func WithChannelFilters(languages []string, minViewers int, persist ChannelFilterPersistFunc) Option {
	return func(m *Model) {
		m.filterLanguages = normalizeUIFilterLanguages(languages)
		if minViewers < 0 {
			minViewers = 0
		}
		m.filterMinViewers = minViewers
		m.filterPersist = persist
	}
}

// FilterLanguages returns a copy of the active language filter (lowercase).
func (m Model) FilterLanguages() []string {
	if len(m.filterLanguages) == 0 {
		return nil
	}
	out := make([]string, len(m.filterLanguages))
	copy(out, m.filterLanguages)
	return out
}

// FilterMinViewers returns the active minimum audience filter (0 = none).
func (m Model) FilterMinViewers() int {
	return m.filterMinViewers
}

// ChannelFilterMenuOpen reports whether the channel filter menu is visible.
func (m Model) ChannelFilterMenuOpen() bool {
	return m.channelFilterMenu
}

func normalizeUIFilterLanguages(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		lang := twitch.NormalizeLanguage(raw)
		if lang == "" {
			continue
		}
		if _, ok := seen[lang]; ok {
			continue
		}
		seen[lang] = struct{}{}
		out = append(out, lang)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// channelPrefsActive reports whether any channel preference filter is set.
func (m Model) channelPrefsActive() bool {
	return len(m.filterLanguages) > 0 || m.filterMinViewers > 0
}

// channelLanguageUnknown reports whether ch has no usable language metadata.
func channelLanguageUnknown(ch twitch.Channel) bool {
	return ch.Language == nil || twitch.NormalizeLanguage(*ch.Language) == ""
}

// channelPassesPrefs reports whether ch survives the language and audience
// preference filters.
//
// Choice for unknown language (documented for ticket 01): when one or more
// languages are selected, channels without a reported language stay visible
// and are marked in the row as unknown. Hiding them would look like a silent
// data loss when the API omits language.
func (m Model) channelPassesPrefs(ch twitch.Channel) bool {
	if m.filterMinViewers > 0 {
		if ch.Viewers == nil || *ch.Viewers < m.filterMinViewers {
			return false
		}
	}
	if len(m.filterLanguages) == 0 {
		return true
	}
	if channelLanguageUnknown(ch) {
		return true
	}
	lang := twitch.NormalizeLanguage(*ch.Language)
	for _, want := range m.filterLanguages {
		if want == lang {
			return true
		}
	}
	return false
}

// languageFilterSelected reports whether lang (already normalized) is selected.
func (m Model) languageFilterSelected(lang string) bool {
	for _, l := range m.filterLanguages {
		if l == lang {
			return true
		}
	}
	return false
}

// activeChannelFilterSummary returns a short status fragment such as "PT · 500+".
func (m Model) activeChannelFilterSummary() string {
	var parts []string
	for _, lang := range m.filterLanguages {
		parts = append(parts, strings.ToUpper(lang))
	}
	if m.filterMinViewers > 0 {
		parts = append(parts, fmt.Sprintf("%d", m.filterMinViewers)+string('+'))
	}
	return strings.Join(parts, " · ")
}

type langCount struct {
	Code  string
	Count int
}

// languagesInCurrentList returns language codes present among currently loaded
// channel rows (listing + favorites + recents), with counts, sorted by code.
// Unknown-language channels are omitted from the menu counts.
func (m Model) languagesInCurrentList() []langCount {
	counts := make(map[string]int)
	add := func(ch twitch.Channel) {
		if channelLanguageUnknown(ch) {
			return
		}
		counts[twitch.NormalizeLanguage(*ch.Language)]++
	}
	for _, ch := range m.channels {
		add(ch)
	}
	for _, ch := range m.favorites {
		add(ch)
	}
	baseRecents := m.recents
	if m.pendingRecents != nil {
		baseRecents = m.pendingRecents
	}
	for _, ch := range baseRecents {
		add(ch)
	}
	out := make([]langCount, 0, len(counts))
	for code, n := range counts {
		out = append(out, langCount{Code: code, Count: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

type channelFilterMenuKind int

const (
	filterMenuAllLangs channelFilterMenuKind = iota
	filterMenuLang
	filterMenuAudience
)

type channelFilterMenuItem struct {
	Kind     channelFilterMenuKind
	Language string // normalized, for filterMenuLang
	Audience int    // for filterMenuAudience
}

func (m Model) buildChannelFilterMenuItems() []channelFilterMenuItem {
	langs := m.languagesInCurrentList()
	items := make([]channelFilterMenuItem, 0, 1+len(langs)+len(channelAudienceThresholds))
	items = append(items, channelFilterMenuItem{Kind: filterMenuAllLangs})
	for _, lc := range langs {
		items = append(items, channelFilterMenuItem{Kind: filterMenuLang, Language: lc.Code})
	}
	for _, n := range channelAudienceThresholds {
		items = append(items, channelFilterMenuItem{Kind: filterMenuAudience, Audience: n})
	}
	return items
}

func (m Model) openChannelFilterMenu() Model {
	m.focus = FocusList
	m.helpVisible = false
	m.filtering = false
	m.searching = false
	m.channelFilterMenu = true
	m.channelFilterMenuItems = m.buildChannelFilterMenuItems()
	if m.channelFilterMenuIndex >= len(m.channelFilterMenuItems) {
		m.channelFilterMenuIndex = 0
	}
	if len(m.channelFilterMenuItems) == 0 {
		m.channelFilterMenuIndex = 0
	}
	return m
}

func (m Model) closeChannelFilterMenu() Model {
	m.channelFilterMenu = false
	return m
}

func (m Model) persistChannelFilters() tea.Cmd {
	if m.filterPersist == nil {
		return nil
	}
	langs := m.FilterLanguages()
	minV := m.filterMinViewers
	persist := m.filterPersist
	return func() tea.Msg {
		_ = persist(langs, minV)
		return nil
	}
}

func (m Model) toggleChannelFilterMenuItem() (Model, tea.Cmd) {
	items := m.channelFilterMenuItems
	if len(items) == 0 {
		items = m.buildChannelFilterMenuItems()
		m.channelFilterMenuItems = items
	}
	if len(items) == 0 {
		return m, nil
	}
	idx := m.channelFilterMenuIndex
	if idx < 0 {
		idx = 0
	}
	if idx >= len(items) {
		idx = len(items) - 1
	}
	m.channelFilterMenuIndex = idx
	item := items[idx]
	switch item.Kind {
	case filterMenuAllLangs:
		m.filterLanguages = nil
	case filterMenuLang:
		if m.languageFilterSelected(item.Language) {
			next := make([]string, 0, len(m.filterLanguages))
			for _, l := range m.filterLanguages {
				if l != item.Language {
					next = append(next, l)
				}
			}
			m.filterLanguages = normalizeUIFilterLanguages(next)
		} else {
			m.filterLanguages = normalizeUIFilterLanguages(append(append([]string{}, m.filterLanguages...), item.Language))
		}
	case filterMenuAudience:
		m.filterMinViewers = item.Audience
	}
	m.reconcileHighlight()
	return m, m.persistChannelFilters()
}

func (m Model) updateChannelFilterMenuKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Clear), key.Matches(msg, m.keys.ChannelFilter):
		return m.closeChannelFilterMenu(), nil

	case key.Matches(msg, m.keys.Up):
		if m.channelFilterMenuIndex > 0 {
			m.channelFilterMenuIndex--
		}
		return m, nil

	case key.Matches(msg, m.keys.Down):
		if m.channelFilterMenuIndex+1 < len(m.channelFilterMenuItems) {
			m.channelFilterMenuIndex++
		}
		return m, nil

	case key.Matches(msg, m.keys.Select), key.Matches(msg, m.keys.Pause), msg.Type == tea.KeySpace:
		return m.toggleChannelFilterMenuItem()
	}
	return m, nil
}

// renderChannelFilterMenuLines returns the menu rows to draw: all of them when
// they fit, else the title and hint, a window centered on the cursor like the
// list's, and the cursor's position. The window derives from the cursor alone,
// so a resize can never leave the cursor off screen. On very short terminals the
// title and hint give way first, and the position next, as in the help.
func (m Model) renderChannelFilterMenuLines(maxWidth int) []string {
	if !m.channelFilterMenu {
		return nil
	}
	head, rows, cursor := m.channelFilterMenuRows(maxWidth)
	available := m.height - 1
	if m.height <= 0 || len(head)+len(rows) <= available {
		return append(head, rows...)
	}

	budget := available
	if m.height > 2 {
		budget--
	}
	pinned := min(len(head), (budget-1)/2)
	window := budget - pinned
	start := max(min(cursor-window/2, len(rows)-window), 0)
	end := min(start+window, len(rows))

	out := make([]string, 0, available)
	out = append(out, head[:pinned]...)
	out = append(out, rows[start:end]...)
	if m.height > 2 {
		out = append(out, positionRow(m.channelFilterMenuIndex+1, len(m.channelFilterMenuEntries()), maxWidth))
	}
	return out
}

// channelFilterMenuEntries returns the items the open menu shows.
func (m Model) channelFilterMenuEntries() []channelFilterMenuItem {
	if len(m.channelFilterMenuItems) > 0 {
		return m.channelFilterMenuItems
	}
	return m.buildChannelFilterMenuItems()
}

// channelFilterMenuRows returns the fixed rows of the menu, the rows that
// scroll, and the index in rows of the one holding the cursor.
func (m Model) channelFilterMenuRows(maxWidth int) (head, rows []string, cursor int) {
	items := m.channelFilterMenuEntries()
	langCounts := m.languagesInCurrentList()
	countBy := make(map[string]int, len(langCounts))
	for _, lc := range langCounts {
		countBy[lc.Code] = lc.Count
	}

	head = []string{
		TruncateVisual("=== "+m.t("channel_filter.title")+" ===", maxWidth),
		TruncateVisual("  "+m.t("channel_filter.hint"), maxWidth),
	}
	audienceHeaderDone := false
	unchecked := string([]rune{'[', ' ', ']'})
	checked := string([]rune{'[', 'x', ']'})
	radioOff := string([]rune{'(', ' ', ')'})
	radioOn := string([]rune{'(', '•', ')'})
	for i, item := range items {
		marker := "  "
		selected := i == m.channelFilterMenuIndex
		if selected {
			marker = "▸ "
		}
		var line string
		switch item.Kind {
		case filterMenuAllLangs:
			box := unchecked
			if len(m.filterLanguages) == 0 {
				box = checked
			}
			line = marker + box + " " + m.t("channel_filter.all_languages")
		case filterMenuLang:
			box := unchecked
			if m.languageFilterSelected(item.Language) {
				box = checked
			}
			line = marker + box + " " + fmt.Sprintf(m.t("channel_filter.language_count"), strings.ToUpper(item.Language), countBy[item.Language])
		case filterMenuAudience:
			if !audienceHeaderDone {
				rows = append(rows, TruncateVisual("  "+m.t("channel_filter.audience_header"), maxWidth))
				audienceHeaderDone = true
			}
			radio := radioOff
			if m.filterMinViewers == item.Audience {
				radio = radioOn
			}
			label := m.t("channel_filter.audience_any")
			if item.Audience > 0 {
				label = fmt.Sprintf(m.t("channel_filter.audience_min"), item.Audience)
			}
			line = marker + radio + " " + label
		}
		if selected {
			cursor = len(rows)
		}
		rows = append(rows, TruncateVisual(line, maxWidth))
	}
	return head, rows, cursor
}

// preferenceFilterHidesChannels reports whether language/audience prefs hid
// every channel row while the underlying data still has channels to show.
// Categories are never filtered; an empty channel section with only category
// rows remaining still counts when channel sources exist.
func (m Model) preferenceFilterHidesChannels() bool {
	if !m.channelPrefsActive() {
		return false
	}
	hasChannelSource := len(m.channels) > 0 || len(m.favorites) > 0 || len(m.recents) > 0 || len(m.pendingRecents) > 0
	if !hasChannelSource {
		return false
	}
	for _, it := range m.VisibleItems() {
		if it.Channel != nil {
			return false
		}
	}
	return true
}
