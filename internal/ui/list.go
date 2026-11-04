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
	out := make([]twitch.Category, len(m.categories))
	copy(out, m.categories)
	return out
}

// VisibleCategories returns the categories matching the current filter.
func (m Model) VisibleCategories() []twitch.Category {
	q := strings.ToLower(strings.TrimSpace(m.filter))
	if q == "" {
		return m.Categories()
	}
	out := make([]twitch.Category, 0, len(m.categories))
	for _, c := range m.categories {
		if strings.Contains(strings.ToLower(c.Name), q) {
			out = append(out, c)
		}
	}
	return out
}

// Channels returns a copy of all loaded channels for the active category or search.
func (m Model) Channels() []twitch.Channel {
	out := make([]twitch.Channel, len(m.channels))
	copy(out, m.channels)
	return out
}

// VisibleChannels returns the channels matching the current filter.
func (m Model) VisibleChannels() []twitch.Channel {
	q := strings.ToLower(strings.TrimSpace(m.filter))
	if q == "" {
		return m.Channels()
	}
	out := make([]twitch.Channel, 0, len(m.channels))
	for _, c := range m.channels {
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
	out := make([]twitch.Channel, len(m.favorites))
	copy(out, m.favorites)
	return out
}

// Recents returns a copy of all recent channels.
func (m Model) Recents() []twitch.Channel {
	out := make([]twitch.Channel, len(m.recents))
	copy(out, m.recents)
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
		for _, c := range m.channels {
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
		q := strings.ToLower(strings.TrimSpace(m.filter))
		if q == "" {
			return true
		}
		return strings.Contains(strings.ToLower(name), q) ||
			strings.Contains(strings.ToLower(login), q) ||
			(title != "" && strings.Contains(strings.ToLower(title), q))
	}

	// 1. Favorites
	for _, fav := range m.favorites {
		ch, avail := savedItem(fav)
		title := ""
		if ch.Title != nil {
			title = *ch.Title
		}
		if !matchesFilter(ch.DisplayName, ch.Login, title) {
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
	for _, rec := range m.recents {
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
		chCopy := ch
		items = append(items, ListItem{
			Type:         ItemTypeRecent,
			Channel:      &chCopy,
			Availability: avail,
		})
		markSeen(ch)
	}

	// 3. Conteúdo da tela
	switch m.Mode() {
	case ModeCategories:
		for _, cat := range m.categories {
			if !matchesFilter(cat.Name, "", "") {
				continue
			}
			catCopy := cat
			items = append(items, ListItem{
				Type:     ItemTypeCategory,
				Category: &catCopy,
			})
		}
	case ModeChannels, ModeSearch:
		for _, ch := range m.channels {
			if isSeen(ch) {
				continue
			}
			title := ""
			if ch.Title != nil {
				title = *ch.Title
			}
			if !matchesFilter(ch.DisplayName, ch.Login, title) {
				continue
			}
			chCopy := ch
			avail := m.listingAvailability(ch)
			if m.isPlayingChannel(ch) {
				avail = twitch.AvailabilityLive
			}
			items = append(items, ListItem{
				Type:         ItemTypeChannel,
				Channel:      &chCopy,
				Availability: avail,
			})
			markSeen(ch)
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
	if m.playerLogin == "" || ch.Login == "" {
		return false
	}
	switch m.playerState {
	case PlayerStatePlaying, PlayerStatePaused, PlayerStateBreak:
		return strings.EqualFold(m.playerLogin, ch.Login)
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
	m.channels = apply(m.channels)
	m.favorites = apply(m.favorites)
	m.recents = apply(m.recents)
}

// refreshSavedAvailability copies the stream state of freshly loaded channels
// into the matching favorites and recents, so the evidence survives leaving the
// listing. Identity is preserved; only availability, viewers and title change.
// Channels absent from the listing keep what they had: absence is not evidence.
func (m *Model) refreshSavedAvailability(loaded []twitch.Channel) {
	m.favorites = m.withLoadedAvailability(m.favorites, loaded)
	m.recents = m.withLoadedAvailability(m.recents, loaded)
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
	return m.highlightedID
}

// HighlightedItem returns the currently highlighted ListItem, if any.
func (m Model) HighlightedItem() (ListItem, bool) {
	if m.highlightedID == "" {
		return ListItem{}, false
	}
	for _, it := range m.VisibleItems() {
		if it.ID() == m.highlightedID {
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
	if m.selectedCategory == nil {
		return nil
	}
	c := *m.selectedCategory
	return &c
}

// SelectedChannel returns the last channel confirmed with Enter, or nil if none.
func (m Model) SelectedChannel() *twitch.Channel {
	if m.selectedChannel == nil {
		return nil
	}
	ch := *m.selectedChannel
	return &ch
}

func (m Model) returnToCategories() (tea.Model, tea.Cmd) {
	m.tracker.cancelActive()
	m.generation++
	m.hydrateGen++
	m.mode = ModeCategories
	m.filtering = false
	m.searching = false
	m.filter = ""
	m.searchInput = ""
	m.err = nil
	m.nextCursor = ""
	m.hasMore = false
	m.focusResults = false
	if m.selectedCategory != nil && m.selectedCategory.ID != "" {
		m.highlightedID = m.selectedCategory.ID
		m.highlightedLogin = ""
	}
	m.reconcileHighlight()
	if m.source == nil {
		m.loading = false
		return m, nil
	}
	m.loading = true
	return m, m.withHydrate(m.fetchCategoriesCmd(m.generation, "", false))
}

func (m Model) retryCurrentMode() (tea.Model, tea.Cmd) {
	if m.source == nil {
		return m, nil
	}
	m.tracker.cancelActive()
	m.generation++
	m.hydrateGen++
	m.loading = true
	m.err = nil
	m.nextCursor = ""
	m.hasMore = false

	switch m.Mode() {
	case ModeChannels:
		if m.selectedCategory != nil {
			return m, m.withHydrate(m.fetchChannelsCmd(m.generation, *m.selectedCategory, "", false))
		}
		m.mode = ModeCategories
		return m, m.withHydrate(m.fetchCategoriesCmd(m.generation, "", false))
	case ModeSearch:
		if strings.TrimSpace(m.searchQuery) != "" {
			return m, m.withHydrate(m.fetchSearchCmd(m.generation, m.searchQuery))
		}
		m.mode = ModeCategories
		return m, m.withHydrate(m.fetchCategoriesCmd(m.generation, "", false))
	default:
		return m, m.withHydrate(m.fetchCategoriesCmd(m.generation, "", false))
	}
}

func (m *Model) recordRecent(ch twitch.Channel) {
	filtered := make([]twitch.Channel, 0, len(m.recents))
	for _, r := range m.recents {
		if !channelMatches(r, ch) {
			filtered = append(filtered, r)
		}
	}
	m.recents = append([]twitch.Channel{ch}, filtered...)
	if len(m.recents) > defaultMaxRecents {
		m.recents = m.recents[:defaultMaxRecents]
	}
}

// ToggleFavorite toggles ch in favorites. If present, removes it; if absent, adds it.
// Returns true if added, false if removed.
func (m *Model) ToggleFavorite(ch twitch.Channel) bool {
	for i, fav := range m.favorites {
		if channelMatches(fav, ch) {
			m.favorites = append(m.favorites[:i], m.favorites[i+1:]...)
			return false
		}
	}
	m.favorites = append(m.favorites, ch)
	return true
}

func (m Model) toStoreState() store.State {
	return store.State{
		Favorites: store.FromTwitchChannels(m.favorites),
		Recents:   store.FromTwitchChannels(m.recents),
	}
}

func (m Model) saveStoreCmd() tea.Cmd {
	if m.store == nil {
		return nil
	}
	st := m.toStoreState()
	s := m.store
	return func() tea.Msg {
		_ = s.Save(st)
		return nil
	}
}

func (m Model) toggleFavoriteHighlighted() (tea.Model, tea.Cmd) {
	it, ok := m.HighlightedItem()
	if !ok || it.Channel == nil {
		return m, nil
	}
	ch := *it.Channel
	added := m.ToggleFavorite(ch)
	if added {
		m.statusNotice = m.t("favorites.added")
	} else {
		m.statusNotice = m.t("favorites.removed")
	}
	var cmds []tea.Cmd
	if m.store != nil {
		cmds = append(cmds, m.saveStoreCmd())
	}
	m.reconcileHighlight()
	return m, tea.Batch(cmds...)
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
		m.selectedCategory = &selected
		if m.source == nil {
			return m, func() tea.Msg {
				return CategorySelectedMsg{Category: selected}
			}
		}
		m.tracker.cancelActive()
		m.generation++
		m.mode = ModeChannels
		m.channels = nil
		m.filter = ""
		m.filtering = false
		m.searching = false
		m.clearHighlight()
		m.focusResults = true
		m.nextCursor = ""
		m.hasMore = false
		m.err = nil
		m.loading = true
		return m, m.fetchChannelsCmd(m.generation, selected, "", false)

	default: // ItemTypeChannel, ItemTypeFavorite, ItemTypeRecent
		if it.Channel == nil {
			return m, nil
		}
		selected := *it.Channel
		m.selectedChannel = &selected
		m.recordRecent(selected)
		return m, func() tea.Msg {
			return ChannelSelectedMsg{Channel: selected}
		}
	}
}

func (m *Model) moveHighlight(delta int) {
	// A manual move is the user's choice; a pending load must not override it.
	m.focusResults = false
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
	m.highlightedID = ""
	m.highlightedLogin = ""
	m.highlightIndex = 0
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
	m.highlightedID = it.ID()
	m.highlightIndex = idx
	m.highlightedLogin = ""
	if it.Channel != nil {
		m.highlightedLogin = it.Channel.Login
	}
}

// indexOfHighlighted finds the highlighted row by stable ID, then by channel
// login. Returns -1 when neither identity is present in items.
func (m Model) indexOfHighlighted(items []ListItem) int {
	if m.highlightedID != "" {
		for i, it := range items {
			if it.ID() == m.highlightedID {
				return i
			}
		}
	}
	if m.highlightedLogin != "" {
		for i, it := range items {
			if it.Channel != nil && strings.EqualFold(it.Channel.Login, m.highlightedLogin) {
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
	m.setHighlightToItem(items, m.highlightIndex)
}

// settleHighlightAfterLoad places the highlight after a channel or search page
// arrives. Right after a category or search is opened, the highlight goes to
// the first result of that screen, so Enter opens what the user just asked for
// and not a favorite or recent rendered above it. Search results are preferred
// by twitch.PreferSearchChannels before this runs (exact login first), so the
// first row is the intended Enter target. Otherwise (refresh, next page) the
// highlighted item keeps its place by identity, or the nearest neighbor when
// that identity left the list.
func (m *Model) settleHighlightAfterLoad() {
	if !m.focusResults {
		m.reconcileHighlight()
		return
	}
	m.focusResults = false
	if id, ok := m.firstResultID(); ok {
		items := m.VisibleItems()
		for i, it := range items {
			if it.ID() == id {
				m.setHighlightToItem(items, i)
				return
			}
		}
		m.highlightedID = id
		m.highlightedLogin = ""
		m.highlightIndex = 0
		return
	}
	m.reconcileHighlight()
}

// firstResultID returns the ID of the first visible row of the loaded listing.
// For search, PreferSearchChannels already put the intended Enter target at
// channels[0] (exact login when present); highlight that row wherever it
// renders — including when it was folded into Favorites/Recents and omitted
// from the search section. For category channels, prefer the listing's own
// section; when every loaded channel is already shown as a favorite or
// recent, fall back to the first loaded channel wherever it renders.
func (m Model) firstResultID() (string, bool) {
	items := m.VisibleItems()
	if m.Mode() == ModeSearch && len(m.channels) > 0 {
		target := m.channels[0]
		for _, it := range items {
			if it.Channel != nil && channelMatches(*it.Channel, target) {
				return it.ID(), true
			}
		}
	}
	for _, it := range items {
		if it.Type == ItemTypeChannel {
			return it.ID(), true
		}
	}
	for _, ch := range m.channels {
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
	if m.source == nil || m.loading || !m.hasMore || m.nextCursor == "" {
		return false
	}
	if m.Mode() == ModeSearch {
		return false
	}
	ids := m.visibleItemIDs()
	if len(ids) == 0 {
		return false
	}
	return ids[len(ids)-1] == m.highlightedID
}

func (m Model) fetchCategoriesCmd(gen uint64, cursor string, appendPage bool) tea.Cmd {
	src := m.source
	tracker := m.tracker
	limit := m.pageSize
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
	tracker := m.tracker
	limit := m.pageSize
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
	hydrate := m.fetchHydrateCmd(m.hydrateGen)
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
	seen := make(map[string]struct{}, len(m.favorites)+len(m.recents))
	out := make([]string, 0, len(m.favorites)+len(m.recents))
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
	for _, ch := range m.favorites {
		add(ch)
	}
	for _, ch := range m.recents {
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
