package ui

import (
	"context"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
	"github.com/pedrosatin/ttv-tui/internal/streamurl"
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
	if input, ok := editText(m.list.search.input, msg); ok {
		m.list.search.input = input
		return m, nil
	}
	switch m.keys.actionFor(modeSearch, msg) {
	case actBack:
		m.list.search.active = false
		m.list.search.input = ""
		return m, nil

	case actSelect, actSearch:
		q := strings.TrimSpace(m.list.search.input)
		if q == "" {
			m.list.search.active = false
			return m, nil
		}
		return m.startSearch(q)
	}
	return m, nil
}

func (m Model) updateFilteringKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if filter, ok := editText(m.list.filter, msg); ok {
		m.list.filter = filter
		m.reconcileHighlight()
		return m, nil
	}
	if step := fieldArrow(msg); step != 0 {
		if m.list.hardErr() {
			return m, nil
		}
		m.moveHighlight(step)
		if step < 0 {
			return m, nil
		}
		return m.loadNextPageIfNeeded()
	}
	switch m.keys.actionFor(modeFilter, msg) {
	case actBack:
		m.list.filtering = false
		m.list.filter = ""
		m.reconcileHighlight()
		return m, nil

	case actSearch:
		if q := strings.TrimSpace(m.list.filter); q != "" {
			return m.startSearch(q)
		}
		m.clearStaleStatusErrors()
		m.list.filtering = false
		m.list.search.active = true
		m.list.search.input = ""
		return m, nil

	case actPageUp:
		return m.moveListPage(-1)

	case actPageDown:
		return m.moveListPage(1)

	case actSelect:
		// A pasted stream URL opens immediately; otherwise with no local match,
		// Enter runs the same global search as Ctrl+F. Enter só abre o item
		// destacado quando ele está na tela; sem ele, o texto vai à busca.
		if streamurl.IsStreamURL(strings.TrimSpace(m.list.filter)) || m.filterHasNoMatches() || !m.highlightOnScreen() {
			return m.startSearch(m.list.filter)
		}
		m.list.filtering = false
		return m.selectHighlighted()
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
		var ch catalog.Channel
		if info.IsTwitch {
			ch = catalog.Channel{
				Platform:    platform.Twitch,
				ID:          info.Login,
				Login:       info.Login,
				DisplayName: info.DisplayName,
			}
		} else {
			ch = catalog.Channel{
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
		if m.Mode() == ModeChannels {
			m.list.search.originCategory = m.list.selectedCategory
		} else {
			m.list.search.originCategory = nil
		}
	}
	m.list.search.categoryOpened = false
	m.list.search.savedChannels = nil
	m.list.search.savedNextCursor = ""
	m.list.search.savedHasMore = false
	m.list.search.savedHighlightedID = ""
	m.list.search.query = q
	m.list.mode = ModeSearch
	m.list.channels = nil
	m.list.search.categories = nil
	m.clearHighlight()
	m.list.focusResults = true
	m.list.nextCursor = ""
	m.list.hasMore = false
	m.clearStaleStatusErrors()
	if m.searcher == nil && m.browser == nil {
		m.list.loading = false
		return m, nil
	}
	m.list.tracker.cancelActive()
	m.list.generation++
	m.list.loading = true
	return m, m.fetchSearchCmd(m.list.generation, q)
}

func (m Model) fetchSearchCmd(gen uint64, query string) tea.Cmd {
	searcher := m.searcher
	browser := m.browser
	tracker := m.list.tracker
	return func() tea.Msg {
		ctx := context.Background()
		var cancel context.CancelFunc
		if tracker != nil {
			ctx, cancel = tracker.newContext()
			defer cancel()
		}

		var (
			page    catalog.Page[catalog.Channel]
			cats    []catalog.Category
			pageErr error
			catsErr error
			wg      sync.WaitGroup
		)
		if searcher != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				page, pageErr = searcher.Search(ctx, query)
			}()
		}
		if browser != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				cats, catsErr = browser.SearchCategories(ctx, query)
			}()
		}
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

// highlightOnScreen diz se o item destacado aparece na tela: a lista ocupa
// colunas e o destaque é um dos itens dela. O recorte da lista (rowWindow) se
// centra no destaque, então um item da lista destacado sempre está à vista.
func (m Model) highlightOnScreen() bool {
	if !m.listVisible() {
		return false
	}
	_, ok := m.HighlightedItem()
	return ok
}

// filterHasNoMatches reports whether a non-empty local filter hides every item
// of a list that has items. That is when global search is the way forward.
func (m Model) filterHasNoMatches() bool {
	if strings.TrimSpace(m.list.filter) == "" {
		return false
	}
	if !m.hasData() {
		return false
	}
	return len(m.VisibleItems()) == 0
}
