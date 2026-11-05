package ui

import (
	"context"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

// Filter returns the active filter string.
func (m Model) Filter() string {
	return m.filter
}

// Filtering reports whether the local filter input is currently active.
func (m Model) Filtering() bool {
	return m.filtering
}

// Searching reports whether the global search prompt is currently active.
func (m Model) Searching() bool {
	return m.searching
}

// SearchQuery returns the active or last submitted global search query.
func (m Model) SearchQuery() string {
	return m.searchQuery
}

// SetFilter updates the active filter while preserving selection by item ID.
// If the previously highlighted item ID remains in the filtered list, it stays
// highlighted regardless of its new positional index.
func (m Model) SetFilter(filter string) Model {
	m.filter = filter
	m.reconcileHighlight()
	return m
}

func (m Model) updateSearchingKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Clear):
		m.searching = false
		m.searchInput = ""
		return m, nil

	case key.Matches(msg, m.keys.Select), key.Matches(msg, m.keys.Search):
		q := strings.TrimSpace(m.searchInput)
		if q == "" {
			m.searching = false
			return m, nil
		}
		return m.startSearch(q)

	case msg.Type == tea.KeyBackspace || msg.Type == tea.KeyDelete:
		m.searchInput = trimLastRune(m.searchInput)
		return m, nil

	case msg.Type == tea.KeySpace:
		m.searchInput += " "
		return m, nil

	case msg.Type == tea.KeyRunes:
		m.searchInput += sanitizeChatText(string(msg.Runes))
		return m, nil
	}
	return m, nil
}

func (m Model) updateFilteringKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Clear):
		m.filtering = false
		m.filter = ""
		m.reconcileHighlight()
		return m, nil

	case key.Matches(msg, m.keys.Search):
		if q := strings.TrimSpace(m.filter); q != "" {
			return m.startSearch(q)
		}
		m.clearStaleStatusErrors()
		m.filtering = false
		m.searching = true
		m.searchInput = ""
		return m, nil

	case msg.Type == tea.KeyUp:
		m.moveHighlight(-1)
		return m, nil

	case msg.Type == tea.KeyDown:
		m.moveHighlight(1)
		if m.shouldLoadNextPage() {
			m.generation++
			m.loading = true
			if m.Mode() == ModeChannels && m.selectedCategory != nil {
				return m, m.fetchChannelsCmd(m.generation, *m.selectedCategory, m.nextCursor, true)
			}
			return m, m.fetchCategoriesCmd(m.generation, m.nextCursor, true)
		}
		return m, nil

	case key.Matches(msg, m.keys.Select):
		// No local match: Enter runs the same global search as Ctrl+F.
		// With matches, Enter still opens the highlighted item.
		if m.filterHasNoMatches() {
			return m.startSearch(m.filter)
		}
		m.filtering = false
		return m.selectHighlighted()

	case msg.Type == tea.KeyBackspace || msg.Type == tea.KeyDelete:
		m.filter = trimLastRune(m.filter)
		m.reconcileHighlight()
		return m, nil

	case msg.Type == tea.KeySpace:
		m.filter += " "
		m.reconcileHighlight()
		return m, nil

	case msg.Type == tea.KeyRunes:
		m.filter += sanitizeChatText(string(msg.Runes))
		m.reconcileHighlight()
		return m, nil
	}
	return m, nil
}

func (m Model) startSearch(query string) (tea.Model, tea.Cmd) {
	q := strings.TrimSpace(query)
	m.searching = false
	m.filtering = false
	m.searchInput = ""
	m.filter = ""
	if q == "" {
		return m, nil
	}
	// Remember where Esc should land. A fresh search from channels or
	// categories records that mode; a new query while already in search keeps
	// the original origin so Esc still steps back one level.
	if m.mode != ModeSearch {
		m.searchOrigin = m.Mode()
	}
	m.searchQuery = q
	m.mode = ModeSearch
	m.channels = nil
	m.searchCategories = nil
	m.clearHighlight()
	m.focusResults = true
	m.nextCursor = ""
	m.hasMore = false
	m.clearStaleStatusErrors()
	if m.source == nil {
		m.loading = false
		return m, nil
	}
	m.tracker.cancelActive()
	m.generation++
	m.loading = true
	return m, m.fetchSearchCmd(m.generation, q)
}

func (m Model) fetchSearchCmd(gen uint64, query string) tea.Cmd {
	src := m.source
	tracker := m.tracker
	return func() tea.Msg {
		ctx := context.Background()
		var cancel context.CancelFunc
		if tracker != nil {
			ctx, cancel = tracker.newContext()
			defer cancel()
		}

		var (
			page    twitch.Page[twitch.Channel]
			cats    []twitch.Category
			pageErr error
			catsErr error
			wg      sync.WaitGroup
		)
		wg.Add(2)
		go func() {
			defer wg.Done()
			page, pageErr = src.Search(ctx, query)
		}()
		go func() {
			defer wg.Done()
			cats, catsErr = src.SearchCategories(ctx, query)
		}()
		wg.Wait()

		err := pageErr
		if err == nil {
			err = catsErr
		}
		return SearchLoadedMsg{
			Generation: gen,
			Query:      query,
			Page:       page,
			Categories: cats,
			Err:        err,
		}
	}
}

// filterHasNoMatches reports whether a non-empty local filter hides every item
// of a list that has items. That is when global search is the way forward.
func (m Model) filterHasNoMatches() bool {
	if strings.TrimSpace(m.filter) == "" {
		return false
	}
	if len(m.categories) == 0 && len(m.channels) == 0 && len(m.searchCategories) == 0 && len(m.favorites) == 0 && len(m.recents) == 0 {
		return false
	}
	return len(m.VisibleItems()) == 0
}
