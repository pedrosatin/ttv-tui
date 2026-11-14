package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// platformCatalog is what one platform offers the list: its live categories
// and their channels, and its search. Either may be missing.
type platformCatalog struct {
	platform string
	browser  catalog.Browser
	searcher catalog.Searcher
}

// WithPlatformCatalog configures the Model to list plat with b and search it
// with s. The list shows one platform at a time, the default one first; the
// platforms take turns in the order they were configured.
func WithPlatformCatalog(plat string, b catalog.Browser, s catalog.Searcher) Option {
	return func(m *Model) {
		m.setCatalog(plat, b, s)
	}
}

// catalogOf returns what was configured for plat, if anything.
func (m Model) catalogOf(plat string) platformCatalog {
	plat = platform.Normalize(plat)
	for _, c := range m.catalogs {
		if c.platform == plat {
			return c
		}
	}
	return platformCatalog{platform: plat}
}

// setCatalog records the catalog of plat in the place it already had, or last.
// A catalog with neither capability is removed. The slice is replaced, not
// changed: a Model copied by Bubble Tea must not share it.
func (m *Model) setCatalog(plat string, b catalog.Browser, s catalog.Searcher) {
	entry := platformCatalog{platform: platform.Normalize(plat), browser: b, searcher: s}
	keep := b != nil || s != nil
	next := make([]platformCatalog, 0, len(m.catalogs)+1)
	placed := false
	for _, c := range m.catalogs {
		if c.platform != entry.platform {
			next = append(next, c)
			continue
		}
		placed = true
		if keep {
			next = append(next, entry)
		}
	}
	if !placed && keep {
		next = append(next, entry)
	}
	m.catalogs = next
}

// openInitialCatalog points the list at the default platform, or at the first
// one configured when the default has no catalog.
func (m *Model) openInitialCatalog() {
	active := m.initialCatalog()
	m.list.platform = active.platform
	m.browser, m.searcher = active.browser, active.searcher
	m.list.loading = m.browser != nil
}

// initialCatalog escolhe Twitch quando disponível, ou o primeiro catálogo configurado.
func (m Model) initialCatalog() platformCatalog {
	active := platformCatalog{platform: platform.Default}
	if len(m.catalogs) > 0 {
		active = m.catalogs[0]
	}
	for _, c := range m.catalogs {
		if c.platform == platform.Default {
			active = c
		}
	}
	return active
}

// savedOnList diz se o salvo aparece na lista ativa. O de plataforma com catálogo
// aparece na lista dela, e o de plataforma sem catálogo, na lista inicial.
func (m Model) savedOnList(ch catalog.Channel) bool {
	plat := ch.CanonicalPlatform()
	c := m.catalogOf(plat)
	if c.browser != nil || c.searcher != nil {
		return plat == m.ListPlatform()
	}
	return m.ListPlatform() == m.initialCatalog().platform
}

// hasSavedOnList diz se algum favorito ou recente, pendentes inclusive, aparece na lista ativa.
func (m Model) hasSavedOnList() bool {
	for _, c := range m.list.favoriteCategories {
		if platform.Normalize(c.Platform) == m.ListPlatform() {
			return true
		}
	}
	for _, channels := range [][]catalog.Channel{m.list.favorites, m.list.recents, m.list.pendingRecents} {
		for _, ch := range channels {
			if m.savedOnList(ch) {
				return true
			}
		}
	}
	return false
}

// ListPlatform returns the canonical name of the platform the list shows.
func (m Model) ListPlatform() string {
	return platform.Normalize(m.list.platform)
}

// nextCatalog returns the catalog after the one on screen, wrapping around.
// With a single platform there is nothing to switch to.
func (m Model) nextCatalog() (platformCatalog, bool) {
	if len(m.catalogs) < 2 {
		return platformCatalog{}, false
	}
	active := m.ListPlatform()
	for i, c := range m.catalogs {
		if c.platform == active {
			return m.catalogs[(i+1)%len(m.catalogs)], true
		}
	}
	return m.catalogs[0], true
}

// listPlatformName is the name of the platform on screen, for a list that has
// more than one to show. With a single platform the name says nothing.
func (m Model) listPlatformName() string {
	if len(m.catalogs) < 2 {
		return ""
	}
	return platform.DisplayName(m.ListPlatform())
}

// listPlatformStatus is the status bar token that names the platform of the
// list. A list error needs the columns, and the unavailable catalog names it.
func (m Model) listPlatformStatus() string {
	name := m.listPlatformName()
	if name == "" || m.list.err != nil {
		return ""
	}
	return fmt.Sprintf(m.t("status.list_platform"), name)
}

// helpPlatformLine is the help row of the platform key, absent when there is
// nothing to switch to.
func (m Model) helpPlatformLine() string {
	if len(m.catalogs) < 2 {
		return ""
	}
	names := make([]string, len(m.catalogs))
	for i, c := range m.catalogs {
		names[i] = platform.DisplayName(c.platform)
	}
	return fmt.Sprintf(m.t("help.platform"), strings.Join(names, ", "))
}

// keySwitchPlatform is p on the list: the next platform's categories.
func (m Model) keySwitchPlatform() (tea.Model, tea.Cmd) {
	if m.Focus() == FocusChat {
		return m, nil
	}
	return m.switchPlatform()
}

// switchPlatform shows the live categories of the next platform. What plays
// and the chat stay as they are: only the list changes.
func (m Model) switchPlatform() (tea.Model, tea.Cmd) {
	next, ok := m.nextCatalog()
	if !ok {
		return m, nil
	}
	m.list.tracker.cancelActive()
	m.list.generation++
	m.applyPendingRecents()
	m.list.platform = next.platform
	m.list.recentsExpanded = false
	m.browser, m.searcher = next.browser, next.searcher
	m.list.mode = ModeCategories
	m.list.search = searchState{}
	m.list.filtering = false
	m.list.filter = ""
	m.list.prefs.menuOpen = false
	m.list.categories = nil
	m.list.channels = nil
	// A category belongs to the platform it came from.
	m.list.selectedCategory = nil
	m.list.nextCursor = ""
	m.list.hasMore = false
	m.list.loadFailed = false
	m.list.focusResults = false
	m.clearStaleStatusErrors()
	m.clearHighlight()
	m.reconcileHighlight()
	m.list.loading = m.browser != nil
	return m, m.fetchCategoriesCmd(m.list.generation, "", false)
}

// softErr reports a load the platform refused before it reached the site
// (catalog.Bloqueio). Nothing is wrong with what the list already has, so the
// rows stay on screen with a notice under them.
func (l listState) softErr() bool {
	return l.err != nil && catalog.KindOf(l.err) == catalog.Bloqueio
}

// hardErr reports a load error that takes the place of the rows.
func (l listState) hardErr() bool {
	return l.err != nil && !l.softErr()
}

// unavailablePlatformName names the platform whose catalog was refused.
func (m Model) unavailablePlatformName() string {
	return platform.DisplayName(errorPlatform(m.list.err, m.ListPlatform()))
}

// catalogUnavailableLines is the notice drawn under the rows while the catalog
// of the platform is refused: what happened and the keys that help.
func (m Model) catalogUnavailableLines(maxWidth int) []string {
	if !m.list.softErr() {
		return nil
	}
	action := m.t("catalog.unavailable_action")
	if len(m.catalogs) > 1 {
		action = m.t("catalog.unavailable_action_switch")
	}
	notice := fmt.Sprintf(m.t("catalog.unavailable"), m.unavailablePlatformName())
	if maxWidth <= 0 {
		return []string{styleWarning(notice), action}
	}
	var lines []string
	for _, part := range strings.Split(ansi.Wrap(notice, maxWidth, ""), "\n") {
		lines = append(lines, styleWarning(TruncateVisual(part, maxWidth)))
	}
	for _, part := range strings.Split(ansi.Wrap(action, maxWidth, ""), "\n") {
		lines = append(lines, TruncateVisual(part, maxWidth))
	}
	return lines
}

// unavailableStatusLead is the bar while the catalog is refused and nothing
// more pressing is going on.
func (m Model) unavailableStatusLead() (statusLead, bool) {
	if !m.list.softErr() {
		return statusLead{}, false
	}
	return statusLead{
		primary: fmt.Sprintf(m.t("catalog.unavailable_status"), m.unavailablePlatformName()),
		tone:    toneWarning,
	}, true
}
