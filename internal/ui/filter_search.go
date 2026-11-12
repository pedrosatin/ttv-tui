package ui

import (
	"context"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/store"
	"github.com/pedrosatin/ttv-tui/internal/streamurl"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

// Filter returns the active filter string.
func (m Model) Filter() string {
	return m.list.filter
}

// Filtering reports whether the local filter input is currently active.
func (m Model) Filtering() bool {
	return m.list.filtering
}

// Searching reports whether the global search prompt is currently active.
func (m Model) Searching() bool {
	return m.list.search.active
}

// SearchQuery returns the active or last submitted global search query.
func (m Model) SearchQuery() string {
	return m.list.search.query
}

// SetFilter updates the active filter while preserving selection by item ID.
// If the previously highlighted item ID remains in the filtered list, it stays
// highlighted regardless of its new positional index.
func (m Model) SetFilter(filter string) Model {
	m.list.filter = filter
	m.reconcileHighlight()
	return m
}

func (m Model) updateSearchingKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Clear):
		m.list.search.active = false
		m.list.search.input = ""
		return m, nil

	case key.Matches(msg, m.keys.Select), key.Matches(msg, m.keys.Search):
		q := strings.TrimSpace(m.list.search.input)
		if q == "" {
			m.list.search.active = false
			return m, nil
		}
		return m.startSearch(q)

	case msg.Type == tea.KeyBackspace || msg.Type == tea.KeyDelete:
		m.list.search.input = trimLastRune(m.list.search.input)
		return m, nil

	case msg.Type == tea.KeySpace:
		m.list.search.input += " "
		return m, nil

	case msg.Type == tea.KeyRunes:
		m.list.search.input += sanitizeChatText(string(msg.Runes))
		return m, nil
	}
	return m, nil
}

func (m Model) updateFilteringKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Clear):
		m.list.filtering = false
		m.list.filter = ""
		m.reconcileHighlight()
		return m, nil

	case key.Matches(msg, m.keys.Search):
		if q := strings.TrimSpace(m.list.filter); q != "" {
			return m.startSearch(q)
		}
		m.clearStaleStatusErrors()
		m.list.filtering = false
		m.list.search.active = true
		m.list.search.input = ""
		return m, nil

	case msg.Type == tea.KeyUp, msg.Type == tea.KeyDown:
		if m.list.err != nil {
			return m, nil
		}
		if msg.Type == tea.KeyUp {
			m.moveHighlight(-1)
			return m, nil
		}
		m.moveHighlight(1)
		if m.shouldLoadNextPage() {
			m.list.generation++
			m.list.loading = true
			if m.Mode() == ModeChannels && m.list.selectedCategory != nil {
				return m, m.fetchChannelsCmd(m.list.generation, *m.list.selectedCategory, m.list.nextCursor, true)
			}
			return m, m.fetchCategoriesCmd(m.list.generation, m.list.nextCursor, true)
		}
		return m, nil

	case key.Matches(msg, m.keys.Select):
		// A pasted stream URL opens immediately; otherwise with no local match,
		// Enter runs the same global search as Ctrl+F. With matches, Enter still opens
		// the highlighted item.
		if streamurl.IsStreamURL(strings.TrimSpace(m.list.filter)) || m.filterHasNoMatches() {
			return m.startSearch(m.list.filter)
		}
		m.list.filtering = false
		return m.selectHighlighted()

	case msg.Type == tea.KeyBackspace || msg.Type == tea.KeyDelete:
		m.list.filter = trimLastRune(m.list.filter)
		m.reconcileHighlight()
		return m, nil

	case msg.Type == tea.KeySpace:
		m.list.filter += " "
		m.reconcileHighlight()
		return m, nil

	case msg.Type == tea.KeyRunes:
		m.list.filter += sanitizeChatText(string(msg.Runes))
		m.reconcileHighlight()
		return m, nil
	}
	return m, nil
}

func (m Model) startSearch(query string) (tea.Model, tea.Cmd) {
	q := strings.TrimSpace(query)
	m.list.search.active = false
	m.list.filtering = false
	m.list.search.input = ""
	m.list.filter = ""
	if q == "" {
		return m, nil
	}
	if info, ok := streamurl.Parse(q); ok {
		var ch twitch.Channel
		if info.IsTwitch {
			ch = twitch.Channel{
				Platform:    store.PlatformTwitch,
				ID:          info.Login,
				Login:       info.Login,
				DisplayName: info.DisplayName,
			}
		} else {
			ch = twitch.Channel{
				Platform:    info.Platform,
				ID:          info.Label,
				Login:       info.Login,
				DisplayName: info.DisplayName,
				URL:         info.URL,
			}
		}
		m.clearHighlight()
		m.clearStaleStatusErrors()
		m.list.enters++
		return m.applyChannelSelected(ChannelSelectedMsg{
			Channel: ch,
			Enter:   m.list.enters,
		})
	}
	// Remember where Esc should land. A fresh search from channels or
	// categories records that mode; a new query while already in search keeps
	// the original origin so Esc still steps back one level.
	if m.list.mode != ModeSearch {
		m.list.search.origin = m.Mode()
	}
	m.list.search.query = q
	m.list.mode = ModeSearch
	m.list.channels = nil
	m.list.search.categories = nil
	m.clearHighlight()
	m.list.focusResults = true
	m.list.nextCursor = ""
	m.list.hasMore = false
	m.clearStaleStatusErrors()
	if m.source == nil {
		m.list.loading = false
		return m, nil
	}
	m.list.tracker.cancelActive()
	m.list.generation++
	m.list.loading = true
	return m, m.fetchSearchCmd(m.list.generation, q)
}

func (m Model) fetchSearchCmd(gen uint64, query string) tea.Cmd {
	src := m.source
	tracker := m.list.tracker
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
	if strings.TrimSpace(m.list.filter) == "" {
		return false
	}
	if len(m.list.categories) == 0 && len(m.list.channels) == 0 && len(m.list.search.categories) == 0 && len(m.list.favorites) == 0 && len(m.list.recents) == 0 {
		return false
	}
	return len(m.VisibleItems()) == 0
}
