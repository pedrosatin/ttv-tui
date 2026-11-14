package ui

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
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
		m.list.prefs.languages = normalizeUIFilterLanguages(languages)
		if minViewers < 0 {
			minViewers = 0
		}
		m.list.prefs.minViewers = minViewers
		m.list.prefs.persist = persist
	}
}

// FilterLanguages returns a copy of the active language filter (lowercase).
func (m Model) FilterLanguages() []string {
	if len(m.list.prefs.languages) == 0 {
		return nil
	}
	out := make([]string, len(m.list.prefs.languages))
	copy(out, m.list.prefs.languages)
	return out
}

// FilterMinViewers returns the active minimum audience filter (0 = none).
func (m Model) FilterMinViewers() int {
	return m.list.prefs.minViewers
}

// ChannelFilterMenuOpen reports whether the channel filter menu is visible.
func (m Model) ChannelFilterMenuOpen() bool {
	return m.list.prefs.menuOpen
}

func normalizeUIFilterLanguages(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		lang := catalog.NormalizeLanguage(raw)
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

// active reports whether any channel preference filter is set.
func (p channelPrefs) active() bool {
	return len(p.languages) > 0 || p.minViewers > 0
}

// channelLanguageUnknown reports whether ch has no usable language metadata.
func channelLanguageUnknown(ch catalog.Channel) bool {
	return ch.Language == nil || catalog.NormalizeLanguage(*ch.Language) == ""
}

// passes reports whether ch survives the language and audience
// preference filters.
//
// Choice for unknown language (documented for ticket 01): when one or more
// languages are selected, channels without a reported language stay visible
// and are marked in the row as unknown. Hiding them would look like a silent
// data loss when the API omits language. On Twitch the server filters first,
// so such a channel only shows up if the server returns it.
func (p channelPrefs) passes(ch catalog.Channel) bool {
	if p.minViewers > 0 {
		if ch.Viewers == nil || *ch.Viewers < p.minViewers {
			return false
		}
	}
	if len(p.languages) == 0 {
		return true
	}
	if channelLanguageUnknown(ch) {
		return true
	}
	lang := catalog.NormalizeLanguage(*ch.Language)
	for _, want := range p.languages {
		if want == lang {
			return true
		}
	}
	return false
}

// languageSelected reports whether lang (already normalized) is selected.
func (p channelPrefs) languageSelected(lang string) bool {
	for _, l := range p.languages {
		if l == lang {
			return true
		}
	}
	return false
}

// summary returns a short status fragment such as "PT · 500+".
func (p channelPrefs) summary() string {
	var parts []string
	for _, lang := range p.languages {
		parts = append(parts, strings.ToUpper(sanitizeScreenText(lang)))
	}
	if p.minViewers > 0 {
		parts = append(parts, fmt.Sprintf("%d", p.minViewers)+string('+'))
	}
	return strings.Join(parts, " · ")
}

type langCount struct {
	Code  string
	Count int
}

// languagesInCurrentList returns language codes present among the loaded listing
// and the favorites and recents of the active list, with counts, sorted by code.
// Unknown-language channels are omitted from the menu counts.
// Selected languages missing from the data are listed with zero; a channel counts once.
func (m Model) languagesInCurrentList() []langCount {
	counts := make(map[string]int)
	var seen []catalog.Channel
	add := func(ch catalog.Channel) {
		if channelLanguageUnknown(ch) {
			return
		}
		for _, previous := range seen {
			if channelMatches(previous, ch) {
				return
			}
		}
		seen = append(seen, ch)
		counts[catalog.NormalizeLanguage(*ch.Language)]++
	}
	for _, ch := range m.list.channels {
		add(ch)
	}
	for _, ch := range m.list.favorites {
		if m.savedOnList(ch) {
			add(ch)
		}
	}
	baseRecents := m.list.recents
	if m.list.pendingRecents != nil {
		baseRecents = m.list.pendingRecents
	}
	for _, ch := range baseRecents {
		if m.savedOnList(ch) {
			add(ch)
		}
	}
	languages := slices.Clone(m.list.prefs.languages)
	if m.Mode() == ModeChannels {
		languages = append(languages, m.list.prefs.seenLanguages...)
	}
	for _, lang := range languages {
		if _, ok := counts[lang]; !ok {
			counts[lang] = 0
		}
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
	m.setFocus(FocusList)
	m.layout.helpVisible = false
	m.list.filtering = false
	m.list.search.active = false
	m.rememberListLanguages()
	m.list.prefs.menuOpen = true
	m.list.prefs.menuItems = m.buildChannelFilterMenuItems()
	m.list.prefs.menuIndex = 0
	return m
}

func (m Model) closeChannelFilterMenu() Model {
	m.list.prefs.menuOpen = false
	return m
}

func (m Model) persistChannelFilters() tea.Cmd {
	if m.list.prefs.persist == nil {
		return nil
	}
	langs := m.FilterLanguages()
	minV := m.list.prefs.minViewers
	persist := m.list.prefs.persist
	return m.list.prefs.saves.cmd(func() tea.Msg {
		return configSavedMsg{Err: persist(langs, minV)}
	})
}

func (m Model) toggleChannelFilterMenuItem() (Model, tea.Cmd) {
	items := m.list.prefs.menuItems
	if len(items) == 0 {
		items = m.buildChannelFilterMenuItems()
		m.list.prefs.menuItems = items
	}
	if len(items) == 0 {
		return m, nil
	}
	idx := m.list.prefs.menuIndex
	if idx < 0 {
		idx = 0
	}
	if idx >= len(items) {
		idx = len(items) - 1
	}
	m.list.prefs.menuIndex = idx
	item := items[idx]
	oldLanguages := m.FilterLanguages()
	switch item.Kind {
	case filterMenuAllLangs:
		m.list.prefs.languages = nil
	case filterMenuLang:
		if m.list.prefs.languageSelected(item.Language) {
			next := make([]string, 0, len(m.list.prefs.languages))
			for _, l := range m.list.prefs.languages {
				if l != item.Language {
					next = append(next, l)
				}
			}
			m.list.prefs.languages = normalizeUIFilterLanguages(next)
		} else {
			m.list.prefs.languages = normalizeUIFilterLanguages(append(append([]string{}, m.list.prefs.languages...), item.Language))
		}
	case filterMenuAudience:
		m.list.prefs.minViewers = item.Audience
	}
	m.reconcileHighlight()
	persist := m.persistChannelFilters()
	if !slices.Equal(oldLanguages, m.FilterLanguages()) && m.Mode() == ModeChannels && m.browser != nil && m.list.selectedCategory != nil {
		m.list.tracker.cancelActive()
		m.list.generation++
		m.list.channels = nil
		m.list.nextCursor = ""
		m.list.hasMore = false
		m.list.err = nil
		m.list.loading = true
		m.reconcileHighlight()
		return m, tea.Batch(persist, m.fetchChannelsCmd(m.list.generation, *m.list.selectedCategory, "", false))
	}
	return m, persist
}

func (m Model) updateChannelFilterMenuKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	id := m.keys.actionFor(modeMenu, msg)
	switch id {
	case actBack, actChannelFilter:
		return m.closeChannelFilterMenu(), nil

	case actUp:
		if m.list.prefs.menuIndex > 0 {
			m.list.prefs.menuIndex--
		}
		return m, nil

	case actDown:
		if m.list.prefs.menuIndex+1 < len(m.list.prefs.menuItems) {
			m.list.prefs.menuIndex++
		}
		return m, nil

	case actHome:
		m.list.prefs.menuIndex = 0
		return m, nil

	case actEnd:
		m.list.prefs.menuIndex = max(len(m.list.prefs.menuItems)-1, 0)
		return m, nil

	case actPageUp, actPageDown:
		head, rows, cursor := m.channelFilterMenuRows(m.width)
		available := m.height - 1
		window := len(rows)
		if m.height > 0 && len(head)+len(rows) > available {
			budget := available
			if m.height > 2 {
				budget--
			}
			window = budget - min(len(head), (budget-1)/2)
		}
		step := max(window-1, 1)
		direction := 1
		if id == actPageUp {
			direction = -1
		}
		target := cursor + direction*step
		original := m.list.prefs.menuIndex
		for i, item := range m.list.prefs.menuItems {
			row := i
			if item.Kind == filterMenuAudience {
				row++
			}
			if direction > 0 && row <= target || direction < 0 && row >= target {
				if direction > 0 && i > m.list.prefs.menuIndex || direction < 0 && i < m.list.prefs.menuIndex {
					m.list.prefs.menuIndex = i
					if direction < 0 {
						break
					}
				}
			}
		}
		if m.list.prefs.menuIndex == original && len(m.list.prefs.menuItems) > 0 {
			m.list.prefs.menuIndex = min(max(original+direction, 0), len(m.list.prefs.menuItems)-1)
		}
		return m, nil

	case actSelect, actPause:
		return m.toggleChannelFilterMenuItem()
	}
	// O tipo KeySpace alterna com qualquer modificador, como antes do registro.
	if msg.Type == tea.KeySpace {
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
	if !m.list.prefs.menuOpen {
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
		out = append(out, positionRow(m.list.prefs.menuIndex+1, len(m.channelFilterMenuEntries()), maxWidth))
	}
	return out
}

// channelFilterMenuEntries returns the items the open menu shows.
func (m Model) channelFilterMenuEntries() []channelFilterMenuItem {
	if len(m.list.prefs.menuItems) > 0 {
		return m.list.prefs.menuItems
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
		selected := i == m.list.prefs.menuIndex
		if selected {
			marker = "▸ "
		}
		var line string
		switch item.Kind {
		case filterMenuAllLangs:
			box := unchecked
			if len(m.list.prefs.languages) == 0 {
				box = checked
			}
			line = marker + box + " " + m.t("channel_filter.all_languages")
		case filterMenuLang:
			box := unchecked
			if m.list.prefs.languageSelected(item.Language) {
				box = checked
			}
			line = marker + box + " " + fmt.Sprintf(m.t("channel_filter.language_count"), strings.ToUpper(sanitizeScreenText(item.Language)), countBy[item.Language])
		case filterMenuAudience:
			if !audienceHeaderDone {
				rows = append(rows, TruncateVisual("  "+m.t("channel_filter.audience_header"), maxWidth))
				audienceHeaderDone = true
			}
			radio := radioOff
			if m.list.prefs.minViewers == item.Audience {
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
	if !m.list.prefs.active() {
		return false
	}
	hasChannelSource := len(m.list.channels) > 0 || m.hasSavedOnList()
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

// rememberListLanguages mantém as opções enquanto a categoria e a plataforma não mudam.
func (m *Model) rememberListLanguages() {
	if m.Mode() != ModeChannels {
		return
	}
	langs := slices.Clone(m.list.prefs.seenLanguages)
	for _, ch := range m.list.channels {
		if !channelLanguageUnknown(ch) {
			lang := catalog.NormalizeLanguage(*ch.Language)
			if !slices.Contains(langs, lang) {
				langs = append(langs, lang)
			}
		}
	}
	m.list.prefs.seenLanguages = langs
}
