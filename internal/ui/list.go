package ui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/store"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

const defaultMaxRecents = 50

// ListItemType identifies the typed category of a list item.
type ListItemType string

const (
	ItemTypeCategory ListItemType = "Categoria"
	ItemTypeChannel  ListItemType = "Canal"
	ItemTypeFavorite ListItemType = "CanalFavorito"
	ItemTypeRecent   ListItemType = "CanalRecente"
	// ItemTypeExactChannel is a search hit whose login or display name equals
	// the query. It renders in its own section, ahead of the categories.
	ItemTypeExactChannel ListItemType = "CanalExato"
)

// ListItem represents a strongly-typed item in the navigation list.
// Availability is the honest stream state for channel items: unknown unless
// something (the playing player, a loaded listing, a search) proved it.
type ListItem struct {
	Type         ListItemType
	Category     *twitch.Category
	Channel      *twitch.Channel
	Availability twitch.Availability
}

// Offline reports whether there is explicit evidence that the channel is not broadcasting.
func (it ListItem) Offline() bool {
	return it.Availability == twitch.AvailabilityOffline
}

// ID returns the stable identifier of the list item.
func (it ListItem) ID() string {
	if it.Category != nil {
		return it.Category.ID
	}
	if it.Channel != nil {
		if it.Channel.ID != "" {
			return it.Channel.ID
		}
		return it.Channel.Login
	}
	return ""
}

// Login returns the Twitch channel login, or empty string for categories.
func (it ListItem) Login() string {
	if it.Channel != nil {
		return it.Channel.Login
	}
	return ""
}

// DisplayName returns the user-facing name for the item.
func (it ListItem) DisplayName() string {
	if it.Category != nil {
		return it.Category.Name
	}
	if it.Channel != nil {
		if it.Channel.DisplayName != "" {
			return it.Channel.DisplayName
		}
		return it.Channel.Login
	}
	return ""
}

// Categories returns a copy of all loaded categories.
func (m Model) Categories() []twitch.Category {
	out := make([]twitch.Category, len(m.list.categories))
	copy(out, m.list.categories)
	return out
}

// VisibleCategories returns the categories matching the current filter.
func (m Model) VisibleCategories() []twitch.Category {
	q := strings.ToLower(strings.TrimSpace(m.list.filter))
	if q == "" {
		return m.Categories()
	}
	out := make([]twitch.Category, 0, len(m.list.categories))
	for _, c := range m.list.categories {
		if strings.Contains(strings.ToLower(c.Name), q) {
			out = append(out, c)
		}
	}
	return out
}

// Channels returns a copy of all loaded channels for the active category or search.
func (m Model) Channels() []twitch.Channel {
	out := make([]twitch.Channel, len(m.list.channels))
	copy(out, m.list.channels)
	return out
}

// VisibleChannels returns the channels matching the text filter and the
// language/audience preference filters.
func (m Model) VisibleChannels() []twitch.Channel {
	q := strings.ToLower(strings.TrimSpace(m.list.filter))
	out := make([]twitch.Channel, 0, len(m.list.channels))
	for _, c := range m.list.channels {
		if !m.channelPassesPrefs(c) {
			continue
		}
		if q == "" {
			out = append(out, c)
			continue
		}
		title := ""
		if c.Title != nil {
			title = strings.ToLower(*c.Title)
		}
		if strings.Contains(strings.ToLower(c.DisplayName), q) ||
			strings.Contains(strings.ToLower(c.Login), q) ||
			(title != "" && strings.Contains(title, q)) {
			out = append(out, c)
		}
	}
	return out
}

// Favorites returns a copy of all favorite channels.
func (m Model) Favorites() []twitch.Channel {
	out := make([]twitch.Channel, len(m.list.favorites))
	copy(out, m.list.favorites)
	return out
}

// Recents returns a copy of all recent channels.
func (m Model) Recents() []twitch.Channel {
	out := make([]twitch.Channel, len(m.list.recents))
	copy(out, m.list.recents)
	return out
}

func channelMatches(a, b twitch.Channel) bool {
	if a.ID != "" && b.ID != "" && a.ID == b.ID {
		return true
	}
	return strings.EqualFold(a.Login, b.Login)
}

// VisibleItems returns the deduplicated list of items in strict order:
// Favorites -> Recents -> Content (Categories or Channels).
// A channel present in Favorites is omitted from Recents and Content.
// A channel present in Recents is omitted from Content.
func (m Model) VisibleItems() []ListItem {
	var items []ListItem
	seenLogins := make(map[string]struct{})
	seenIDs := make(map[string]struct{})

	markSeen := func(ch twitch.Channel) {
		if ch.Login != "" {
			seenLogins[strings.ToLower(ch.Login)] = struct{}{}
		}
		if ch.ID != "" {
			seenIDs[ch.ID] = struct{}{}
		}
	}

	isSeen := func(ch twitch.Channel) bool {
		if ch.Login != "" {
			if _, ok := seenLogins[strings.ToLower(ch.Login)]; ok {
				return true
			}
		}
		if ch.ID != "" {
			if _, ok := seenIDs[ch.ID]; ok {
				return true
			}
		}
		return false
	}

	// findLoaded returns the loaded copy of ch when the current listing has
	// evidence about its availability.
	findLoaded := func(ch twitch.Channel) (twitch.Channel, bool) {
		for _, c := range m.list.channels {
			if channelMatches(c, ch) && m.listingAvailability(c) != twitch.AvailabilityUnknown {
				return c, true
			}
		}
		return ch, false
	}

	savedItem := func(saved twitch.Channel) (twitch.Channel, twitch.Availability) {
		ch := saved
		avail := saved.Availability
		if loaded, ok := findLoaded(saved); ok {
			ch = loaded
			avail = m.listingAvailability(loaded)
		}
		if m.isPlayingChannel(ch) {
			avail = twitch.AvailabilityLive
		}
		return ch, avail
	}

	matchesFilter := func(name, login, title string) bool {
		q := strings.ToLower(strings.TrimSpace(m.list.filter))
		if q == "" {
			return true
		}
		return strings.Contains(strings.ToLower(name), q) ||
			strings.Contains(strings.ToLower(login), q) ||
			(title != "" && strings.Contains(strings.ToLower(title), q))
	}

	// 1. Favorites
	for _, fav := range m.list.favorites {
		ch, avail := savedItem(fav)
		title := ""
		if ch.Title != nil {
			title = *ch.Title
		}
		if !matchesFilter(ch.DisplayName, ch.Login, title) {
			continue
		}
		if !m.channelPassesPrefs(ch) {
			continue
		}
		chCopy := ch
		items = append(items, ListItem{
			Type:         ItemTypeFavorite,
			Channel:      &chCopy,
			Availability: avail,
		})
		markSeen(ch)
	}

	// 2. Recents (excluding any already seen in Favorites)
	for _, rec := range m.list.recents {
		if isSeen(rec) {
			continue
		}
		ch, avail := savedItem(rec)
		title := ""
		if ch.Title != nil {
			title = *ch.Title
		}
		if !matchesFilter(ch.DisplayName, ch.Login, title) {
			continue
		}
		if !m.channelPassesPrefs(ch) {
			continue
		}
		chCopy := ch
		items = append(items, ListItem{
			Type:         ItemTypeRecent,
			Channel:      &chCopy,
			Availability: avail,
		})
		markSeen(ch)
	}

	addChannel := func(ch twitch.Channel, itemType ListItemType) {
		if isSeen(ch) {
			return
		}
		title := ""
		if ch.Title != nil {
			title = *ch.Title
		}
		if !matchesFilter(ch.DisplayName, ch.Login, title) {
			return
		}
		if !m.channelPassesPrefs(ch) {
			return
		}
		chCopy := ch
		avail := m.listingAvailability(ch)
		if m.isPlayingChannel(ch) {
			avail = twitch.AvailabilityLive
		}
		items = append(items, ListItem{
			Type:         itemType,
			Channel:      &chCopy,
			Availability: avail,
		})
		markSeen(ch)
	}

	// 3. Conteúdo da tela
	switch m.Mode() {
	case ModeCategories:
		for _, cat := range m.list.categories {
			if !matchesFilter(cat.Name, "", "") {
				continue
			}
			catCopy := cat
			items = append(items, ListItem{
				Type:     ItemTypeCategory,
				Category: &catCopy,
			})
		}
	case ModeSearch:
		// An exact channel hit first (v1.9 ticket 11), then categories, then the
		// other channels (ticket 09).
		for _, ch := range m.list.channels {
			if twitch.ExactChannelMatch(m.list.search.query, ch) {
				addChannel(ch, ItemTypeExactChannel)
			}
		}
		for _, cat := range m.list.search.categories {
			if !matchesFilter(cat.Name, "", "") {
				continue
			}
			catCopy := cat
			items = append(items, ListItem{
				Type:     ItemTypeCategory,
				Category: &catCopy,
			})
		}
		fallthrough
	case ModeChannels:
		for _, ch := range m.list.channels {
			addChannel(ch, ItemTypeChannel)
		}
	}

	return items
}

// listingAvailability resolves the availability of a channel from the loaded
// listing: explicit evidence from the Source wins; a viewer count only exists
// for an active stream; a category listing only contains live streams.
// Anything else is unknown — a missing viewer count is never offline.
func (m Model) listingAvailability(ch twitch.Channel) twitch.Availability {
	if ch.Availability != twitch.AvailabilityUnknown {
		return ch.Availability
	}
	if ch.Viewers != nil {
		return twitch.AvailabilityLive
	}
	if m.Mode() == ModeChannels {
		return twitch.AvailabilityLive
	}
	return twitch.AvailabilityUnknown
}

// isPlayingChannel reports whether ch is the channel the player is showing now.
// A channel being played is live regardless of what the store says.
func (m Model) isPlayingChannel(ch twitch.Channel) bool {
	if m.player.login == "" || ch.Login == "" {
		return false
	}
	switch m.player.state {
	case PlayerStatePlaying, PlayerStatePaused, PlayerStateBreak:
		return strings.EqualFold(m.player.login, ch.Login)
	default:
		return false
	}
}

// markChannelOffline records explicit offline evidence for login in the
// channels listing, favorites and recents. Only the matching channel is
// touched: viewers of other channels stay intact. The slices are copied so
// earlier Model values that share a backing array are not mutated.
func (m *Model) markChannelOffline(login string) {
	if strings.TrimSpace(login) == "" {
		return
	}
	apply := func(chs []twitch.Channel) []twitch.Channel {
		out := make([]twitch.Channel, len(chs))
		copy(out, chs)
		for i := range out {
			if !strings.EqualFold(out[i].Login, login) {
				continue
			}
			out[i].Availability = twitch.AvailabilityOffline
			out[i].Viewers = nil
		}
		return out
	}
	m.list.channels = apply(m.list.channels)
	m.list.favorites = apply(m.list.favorites)
	m.list.recents = apply(m.list.recents)
	if m.list.pendingRecents != nil {
		m.list.pendingRecents = apply(m.list.pendingRecents)
	}
}

// refreshSavedAvailability copies the stream state of freshly loaded channels
// into the matching favorites and recents, so the evidence survives leaving the
// listing. Identity is preserved; availability, viewers, title and category
// change. Channels absent from the listing keep what they had: absence is not
// evidence.
func (m *Model) refreshSavedAvailability(loaded []twitch.Channel) {
	m.list.favorites = m.withLoadedAvailability(m.list.favorites, loaded)
	m.list.recents = m.withLoadedAvailability(m.list.recents, loaded)
	if m.list.pendingRecents != nil {
		m.list.pendingRecents = m.withLoadedAvailability(m.list.pendingRecents, loaded)
	}
}

// withLoadedAvailability returns a copy of saved with the evidence from loaded
// applied. It never mutates saved, whose backing array may be shared with
// earlier Model values.
func (m Model) withLoadedAvailability(saved, loaded []twitch.Channel) []twitch.Channel {
	out := make([]twitch.Channel, len(saved))
	copy(out, saved)
	for i := range out {
		for _, c := range loaded {
			if !channelMatches(out[i], c) {
				continue
			}
			avail := m.listingAvailability(c)
			if avail == twitch.AvailabilityUnknown {
				break
			}
			out[i].Availability = avail
			out[i].Viewers = c.Viewers
			out[i].Title = c.Title
			if c.CategoryID != nil {
				out[i].CategoryID = c.CategoryID
			}
			if c.CategoryName != nil {
				out[i].CategoryName = c.CategoryName
			}
			if c.Language != nil {
				out[i].Language = c.Language
			}
			if avail == twitch.AvailabilityOffline {
				out[i].Viewers = nil
			}
			break
		}
	}
	return out
}

// HighlightedID returns the stable ID of the currently highlighted item.
func (m Model) HighlightedID() string {
	return m.list.highlightedID
}

// HighlightedItem returns the currently highlighted ListItem, if any.
func (m Model) HighlightedItem() (ListItem, bool) {
	if m.list.highlightedID == "" {
		return ListItem{}, false
	}
	for _, it := range m.VisibleItems() {
		if it.ID() == m.list.highlightedID {
			return it, true
		}
	}
	return ListItem{}, false
}

// HighlightedCategory returns the currently highlighted category by ID.
func (m Model) HighlightedCategory() (twitch.Category, bool) {
	it, ok := m.HighlightedItem()
	if !ok || it.Type != ItemTypeCategory || it.Category == nil {
		return twitch.Category{}, false
	}
	return *it.Category, true
}

// HighlightedChannel returns the currently highlighted channel by ID.
func (m Model) HighlightedChannel() (twitch.Channel, bool) {
	it, ok := m.HighlightedItem()
	if !ok || it.Channel == nil {
		return twitch.Channel{}, false
	}
	return *it.Channel, true
}

// SelectedCategory returns the last category confirmed with Enter, or nil if none.
func (m Model) SelectedCategory() *twitch.Category {
	if m.list.selectedCategory == nil {
		return nil
	}
	c := *m.list.selectedCategory
	return &c
}

// SelectedChannel returns the last channel confirmed with Enter, or nil if none.
func (m Model) SelectedChannel() *twitch.Channel {
	if m.list.selectedChannel == nil {
		return nil
	}
	ch := *m.list.selectedChannel
	return &ch
}

func (m Model) returnToCategories() (tea.Model, tea.Cmd) {
	m.list.tracker.cancelActive()
	m.list.generation++
	m.list.hydrateGen++
	m.applyPendingRecents()
	m.list.mode = ModeCategories
	m.list.search.origin = ""
	m.list.filtering = false
	m.list.search.active = false
	m.list.prefs.menuOpen = false
	m.list.filter = ""
	m.list.search.input = ""
	m.list.search.categories = nil
	m.clearStaleStatusErrors()
	m.list.nextCursor = ""
	m.list.hasMore = false
	m.list.focusResults = false
	if m.list.selectedCategory != nil && m.list.selectedCategory.ID != "" {
		m.list.highlightedID = m.list.selectedCategory.ID
		m.list.highlightedLogin = ""
	}
	m.reconcileHighlight()
	if m.source == nil {
		m.list.loading = false
		return m, nil
	}
	m.list.loading = true
	return m, m.withHydrate(m.fetchCategoriesCmd(m.list.generation, "", false))
}

// returnFromSearch steps back from search results to the list that started the
// search. A search opened inside a category reloads that category's channels;
// a search opened from the categories list returns to categories.
func (m Model) returnFromSearch() (tea.Model, tea.Cmd) {
	if m.list.search.origin == ModeChannels && m.list.selectedCategory != nil {
		return m.returnToChannels()
	}
	return m.returnToCategories()
}

func (m Model) returnToChannels() (tea.Model, tea.Cmd) {
	if m.list.selectedCategory == nil {
		return m.returnToCategories()
	}
	cat := *m.list.selectedCategory
	m.list.tracker.cancelActive()
	m.list.generation++
	m.list.hydrateGen++
	m.applyPendingRecents()
	m.list.mode = ModeChannels
	m.list.search.origin = ""
	m.list.filtering = false
	m.list.search.active = false
	m.list.prefs.menuOpen = false
	m.list.filter = ""
	m.list.search.input = ""
	m.list.search.query = ""
	m.list.search.categories = nil
	m.clearStaleStatusErrors()
	m.list.nextCursor = ""
	m.list.hasMore = false
	m.list.channels = nil
	m.clearHighlight()
	m.list.focusResults = true
	if m.source == nil {
		m.list.loading = false
		return m, nil
	}
	m.list.loading = true
	return m, m.withHydrate(m.fetchChannelsCmd(m.list.generation, cat, "", false))
}

func (m Model) retryCurrentMode() (tea.Model, tea.Cmd) {
	if m.source == nil {
		return m, nil
	}
	m.list.tracker.cancelActive()
	m.list.generation++
	m.list.hydrateGen++
	m.applyPendingRecents()
	m.list.loading = true
	m.list.err = nil
	m.list.nextCursor = ""
	m.list.hasMore = false

	switch m.Mode() {
	case ModeChannels:
		if m.list.selectedCategory != nil {
			return m, m.withHydrate(m.fetchChannelsCmd(m.list.generation, *m.list.selectedCategory, "", false))
		}
		m.list.mode = ModeCategories
		return m, m.withHydrate(m.fetchCategoriesCmd(m.list.generation, "", false))
	case ModeSearch:
		if strings.TrimSpace(m.list.search.query) != "" {
			return m, m.withHydrate(m.fetchSearchCmd(m.list.generation, m.list.search.query))
		}
		m.list.mode = ModeCategories
		return m, m.withHydrate(m.fetchCategoriesCmd(m.list.generation, "", false))
	default:
		return m, m.withHydrate(m.fetchCategoriesCmd(m.list.generation, "", false))
	}
}

// queueRecent records ch as the newest recent for persistence without changing
// the visible list. VisibleItems keeps using m.recents until applyPendingRecents
// runs on the next list load.
func (m *Model) queueRecent(ch twitch.Channel) {
	base := m.list.recents
	if m.list.pendingRecents != nil {
		base = m.list.pendingRecents
	}
	filtered := make([]twitch.Channel, 0, len(base))
	for _, r := range base {
		if !channelMatches(r, ch) {
			filtered = append(filtered, r)
		}
	}
	m.list.pendingRecents = append([]twitch.Channel{ch}, filtered...)
	if len(m.list.pendingRecents) > defaultMaxRecents {
		m.list.pendingRecents = m.list.pendingRecents[:defaultMaxRecents]
	}
}

// applyPendingRecents promotes the store-ordered recents into the visible list.
func (m *Model) applyPendingRecents() {
	if m.list.pendingRecents == nil {
		return
	}
	m.list.recents = m.list.pendingRecents
	m.list.pendingRecents = nil
}

// ToggleFavorite toggles ch in favorites. If present, removes it; if absent, adds it.
// Returns true if added, false if removed.
func (m *Model) ToggleFavorite(ch twitch.Channel) bool {
	for i, fav := range m.list.favorites {
		if channelMatches(fav, ch) {
			m.list.favorites = append(m.list.favorites[:i], m.list.favorites[i+1:]...)
			return false
		}
	}
	m.list.favorites = append(m.list.favorites, ch)
	return true
}

func (m Model) toStoreState() store.State {
	recs := m.list.recents
	if m.list.pendingRecents != nil {
		recs = m.list.pendingRecents
	}
	return store.State{
		Favorites: store.FromTwitchChannels(m.list.favorites),
		Recents:   store.FromTwitchChannels(recs),
	}
}

type storeSavedMsg struct {
	Err    error
	Notice string
}

func (m Model) saveStoreCmd(notice ...string) tea.Cmd {
	if m.store == nil {
		return nil
	}
	st := m.toStoreState()
	s := m.store
	confirmation := ""
	if len(notice) > 0 {
		confirmation = notice[0]
	}
	return func() tea.Msg {
		return storeSavedMsg{Err: s.Save(st), Notice: confirmation}
	}
}

func (m Model) toggleFavoriteHighlighted() (tea.Model, tea.Cmd) {
	it, ok := m.HighlightedItem()
	if !ok || it.Channel == nil {
		return m, nil
	}
	ch := *it.Channel
	added := m.ToggleFavorite(ch)
	notice := "favorites.removed"
	if added {
		notice = "favorites.added"
	}
	m.clearStatusNotice()
	cmd := m.saveStoreCmd(notice)
	m.reconcileHighlight()
	return m, cmd
}

func (m Model) selectHighlighted() (tea.Model, tea.Cmd) {
	it, ok := m.HighlightedItem()
	if !ok {
		return m, nil
	}

	switch it.Type {
	case ItemTypeCategory:
		if it.Category == nil {
			return m, nil
		}
		selected := *it.Category
		m.list.selectedCategory = &selected
		if m.source == nil {
			return m, func() tea.Msg {
				return CategorySelectedMsg{Category: selected}
			}
		}
		m.list.tracker.cancelActive()
		m.list.generation++
		m.list.mode = ModeChannels
		m.list.channels = nil
		m.list.search.categories = nil
		m.list.search.query = ""
		m.list.search.origin = ""
		m.list.filter = ""
		m.list.filtering = false
		m.list.search.active = false
		m.clearHighlight()
		m.list.focusResults = true
		m.list.nextCursor = ""
		m.list.hasMore = false
		m.clearStaleStatusErrors()
		m.list.loading = true
		return m, m.fetchChannelsCmd(m.list.generation, selected, "", false)

	default: // ItemTypeChannel, ItemTypeExactChannel, ItemTypeFavorite, ItemTypeRecent
		if it.Channel == nil {
			return m, nil
		}
		selected := *it.Channel
		// Stamp the open category onto a category-listing row so the status bar
		// shows the channel's game. Favorites, recents and search hits keep
		// whatever CategoryName the Source already set; unknown stays empty and
		// the bar omits it (never the last list visited).
		if it.Type == ItemTypeChannel && m.Mode() == ModeChannels {
			stampChannelCategory(&selected, m.list.selectedCategory)
		}
		m.list.selectedChannel = &selected
		// Opening a channel drops the local filter so favorites and the rest of
		// the list are visible again; the user did not ask to stay filtered.
		m.list.filter = ""
		m.list.filtering = false
		// Recents are queued for the store in ChannelSelectedMsg; the visible
		// order stays frozen so Enter does not land on a shifted row.
		return m, func() tea.Msg {
			return ChannelSelectedMsg{Channel: selected}
		}
	}
}

// stampChannelCategory fills CategoryID/CategoryName from the open category
// when the channel itself does not already carry them (category listing rows).
func stampChannelCategory(ch *twitch.Channel, cat *twitch.Category) {
	if ch == nil || cat == nil {
		return
	}
	if (ch.CategoryName == nil || strings.TrimSpace(*ch.CategoryName) == "") && strings.TrimSpace(cat.Name) != "" {
		name := cat.Name
		ch.CategoryName = &name
	}
	if (ch.CategoryID == nil || strings.TrimSpace(*ch.CategoryID) == "") && strings.TrimSpace(cat.ID) != "" {
		id := cat.ID
		ch.CategoryID = &id
	}
}

func (m *Model) moveHighlight(delta int) {
	// A manual move is the user's choice; a pending load must not override it.
	m.list.focusResults = false
	items := m.VisibleItems()
	if len(items) == 0 {
		m.clearHighlight()
		return
	}
	idx := m.indexOfHighlighted(items)
	if idx < 0 {
		m.setHighlightToItem(items, 0)
		return
	}
	next := idx + delta
	if next < 0 {
		next = 0
	}
	if next >= len(items) {
		next = len(items) - 1
	}
	m.setHighlightToItem(items, next)
}

// clearHighlight drops the highlight identity and its cached position.
func (m *Model) clearHighlight() {
	m.list.highlightedID = ""
	m.list.highlightedLogin = ""
	m.list.highlightIndex = 0
}

// setHighlightToItem points the highlight at items[idx] and refreshes the
// cached index and channel login used after a later list reload.
func (m *Model) setHighlightToItem(items []ListItem, idx int) {
	if len(items) == 0 {
		m.clearHighlight()
		return
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(items) {
		idx = len(items) - 1
	}
	it := items[idx]
	m.list.highlightedID = it.ID()
	m.list.highlightIndex = idx
	m.list.highlightedLogin = ""
	if it.Channel != nil {
		m.list.highlightedLogin = it.Channel.Login
	}
}

// indexOfHighlighted finds the highlighted row by stable ID, then by channel
// login. Returns -1 when neither identity is present in items.
func (m Model) indexOfHighlighted(items []ListItem) int {
	if m.list.highlightedID != "" {
		for i, it := range items {
			if it.ID() == m.list.highlightedID {
				return i
			}
		}
	}
	if m.list.highlightedLogin != "" {
		for i, it := range items {
			if it.Channel != nil && strings.EqualFold(it.Channel.Login, m.list.highlightedLogin) {
				return i
			}
		}
	}
	return -1
}

// reconcileHighlight keeps the highlight on the same identity after the
// visible list changes. When that identity is gone (refresh dropped a channel,
// filter hid a row), the highlight lands on the nearest remaining row at the
// previous index — never forced back to the top.
func (m *Model) reconcileHighlight() {
	items := m.VisibleItems()
	if len(items) == 0 {
		if m.totalItemCount() == 0 {
			m.clearHighlight()
		}
		return
	}
	if idx := m.indexOfHighlighted(items); idx >= 0 {
		m.setHighlightToItem(items, idx)
		return
	}
	m.setHighlightToItem(items, m.list.highlightIndex)
}

// settleHighlightAfterLoad places the highlight after a channel or search page
// arrives. Right after a category or search is opened, the highlight goes to
// the first result of that screen, so Enter opens what the user just asked for
// and not a favorite or recent rendered above it. Search results are preferred
// by twitch.PreferSearchChannels before this runs (exact login, then exact
// display name), and firstResultID puts that hit ahead of the categories. Otherwise (refresh, next page) the
// highlighted item keeps its place by identity, or the nearest neighbor when
// that identity left the list.
func (m *Model) settleHighlightAfterLoad() {
	if !m.list.focusResults {
		m.reconcileHighlight()
		return
	}
	m.list.focusResults = false
	if id, ok := m.firstResultID(); ok {
		items := m.VisibleItems()
		for i, it := range items {
			if it.ID() == id {
				m.setHighlightToItem(items, i)
				return
			}
		}
		m.list.highlightedID = id
		m.list.highlightedLogin = ""
		m.list.highlightIndex = 0
		return
	}
	m.reconcileHighlight()
}

// firstResultID returns the ID of the first visible row of the loaded listing.
// For search, a channel named exactly by the query wins, and PreferSearchChannels
// already put it at channels[0]. Without one, categories come first
// (PreferSearchCategories), then channels[0]. Highlight the target wherever it
// renders — including when it was folded into Favorites/Recents and omitted from
// the search section. An exact hit hidden by the preference filters has no row,
// so the categories take over. For category channels, prefer the listing's own section;
// when every loaded channel is already shown as a favorite or recent, fall
// back to the first loaded channel wherever it renders.
func (m Model) firstResultID() (string, bool) {
	items := m.VisibleItems()
	if m.Mode() == ModeSearch {
		for _, ch := range m.list.channels {
			if !twitch.ExactChannelMatch(m.list.search.query, ch) {
				continue
			}
			for _, it := range items {
				if it.Channel != nil && channelMatches(*it.Channel, ch) {
					return it.ID(), true
				}
			}
		}
	}
	if m.Mode() == ModeSearch && len(m.list.search.categories) > 0 {
		targetID := m.list.search.categories[0].ID
		for _, it := range items {
			if it.Type == ItemTypeCategory && it.Category != nil && it.Category.ID == targetID {
				return it.ID(), true
			}
		}
		if targetID != "" {
			return targetID, true
		}
	}
	if m.Mode() == ModeSearch && len(m.list.channels) > 0 {
		target := m.list.channels[0]
		for _, it := range items {
			if it.Channel != nil && channelMatches(*it.Channel, target) {
				return it.ID(), true
			}
		}
	}
	for _, it := range items {
		if it.Type == ItemTypeCategory && m.Mode() == ModeSearch {
			return it.ID(), true
		}
		if it.Type == ItemTypeChannel {
			return it.ID(), true
		}
	}
	for _, ch := range m.list.channels {
		for _, it := range items {
			if it.Channel != nil && channelMatches(*it.Channel, ch) {
				return it.ID(), true
			}
		}
	}
	return "", false
}

func (m Model) visibleItemIDs() []string {
	items := m.VisibleItems()
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.ID()
	}
	return ids
}

func (m Model) totalItemCount() int {
	return len(m.VisibleItems())
}

func (m Model) shouldLoadNextPage() bool {
	if m.source == nil || m.list.loading || !m.list.hasMore || m.list.nextCursor == "" {
		return false
	}
	if m.Mode() == ModeSearch {
		return false
	}
	ids := m.visibleItemIDs()
	if len(ids) == 0 {
		return false
	}
	return ids[len(ids)-1] == m.list.highlightedID
}

func (m Model) fetchCategoriesCmd(gen uint64, cursor string, appendPage bool) tea.Cmd {
	src := m.source
	tracker := m.list.tracker
	limit := m.list.pageSize
	if limit <= 0 {
		limit = defaultCategoryPageSize
	}
	return func() tea.Msg {
		ctx := context.Background()
		var cancel context.CancelFunc
		if tracker != nil {
			ctx, cancel = tracker.newContext()
			defer cancel()
		}
		page, err := src.Categories(ctx, twitch.PageRequest{
			Cursor: cursor,
			Limit:  limit,
		})
		return CategoriesLoadedMsg{
			Generation: gen,
			Page:       page,
			Append:     appendPage,
			Err:        err,
		}
	}
}

func (m Model) fetchChannelsCmd(gen uint64, cat twitch.Category, cursor string, appendPage bool) tea.Cmd {
	src := m.source
	tracker := m.list.tracker
	limit := m.list.pageSize
	if limit <= 0 {
		limit = defaultCategoryPageSize
	}
	catID := cat.ID
	if catID == "" {
		catID = cat.Name
	}
	return func() tea.Msg {
		ctx := context.Background()
		var cancel context.CancelFunc
		if tracker != nil {
			ctx, cancel = tracker.newContext()
			defer cancel()
		}
		page, err := src.Channels(ctx, catID, twitch.PageRequest{
			Cursor: cursor,
			Limit:  limit,
		})
		return ChannelsLoadedMsg{
			Generation: gen,
			Category:   cat,
			Page:       page,
			Append:     appendPage,
			Err:        err,
		}
	}
}

// withHydrate batches cmd with a favorites/recents availability lookup when
// there is anything to hydrate. Hydration uses its own generation and does not
// share the request tracker, so it never cancels catalog fetches.
func (m Model) withHydrate(cmd tea.Cmd) tea.Cmd {
	hydrate := m.fetchHydrateCmd(m.list.hydrateGen)
	switch {
	case cmd == nil:
		return hydrate
	case hydrate == nil:
		return cmd
	default:
		return tea.Batch(cmd, hydrate)
	}
}

// savedLogins returns unique non-empty logins from favorites and recents.
func (m Model) savedLogins() []string {
	n := len(m.list.favorites) + len(m.list.recents)
	if m.list.pendingRecents != nil {
		n += len(m.list.pendingRecents)
	}
	seen := make(map[string]struct{}, n)
	out := make([]string, 0, n)
	add := func(ch twitch.Channel) {
		login := strings.TrimSpace(ch.Login)
		if login == "" {
			return
		}
		key := strings.ToLower(login)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, login)
	}
	for _, ch := range m.list.favorites {
		add(ch)
	}
	for _, ch := range m.list.recents {
		add(ch)
	}
	for _, ch := range m.list.pendingRecents {
		add(ch)
	}
	return out
}

// fetchHydrateCmd looks up Availability for favorites and recents in one batch.
// An empty saved set returns nil so no request is issued.
func (m Model) fetchHydrateCmd(gen uint64) tea.Cmd {
	src := m.source
	if src == nil {
		return nil
	}
	logins := m.savedLogins()
	if len(logins) == 0 {
		return nil
	}
	return func() tea.Msg {
		channels, err := src.ChannelsByLogins(context.Background(), logins)
		return SavedAvailabilityMsg{
			Generation: gen,
			Channels:   channels,
			Err:        err,
		}
	}
}
