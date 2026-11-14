package ui

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
	"github.com/pedrosatin/ttv-tui/internal/store"
)

// hydrationTimeout limita a espera pela disponibilidade dos canais salvos
// (favoritos e recentes). Fica no mesmo valor do prazo de uma requisição da
// Twitch: vencido o prazo, a busca é cancelada e os itens seguem como não
// verificados, com a lista usável. É variável só para o teste encurtá-lo.
var hydrationTimeout = 10 * time.Second

const defaultMaxRecents = 50

// homeRecentsLimit é quantos recentes a tela inicial mostra antes da linha
// "mais…".
const homeRecentsLimit = 6

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
	// ItemTypeMoreRecents é a linha "mais…" da tela inicial, depois dos
	// primeiros recentes. Enter nela mostra o resto.
	ItemTypeMoreRecents ListItemType = "MaisRecentes"
)

// moreRecentsID é o ID estável da linha "mais…".
const moreRecentsID = "recentes:mais"

// ListItem represents a strongly-typed item in the navigation list.
// Availability is the honest stream state for channel items: unknown unless
// something (the playing player, a loaded listing, a search) proved it.
type ListItem struct {
	Type         ListItemType
	Category     *catalog.Category
	Channel      *catalog.Channel
	Availability catalog.Availability
	// Favorite marca com ★ um canal da listagem ou da busca que está nos
	// favoritos: ele fica no lugar dele, sem ir para a seção Favoritos.
	Favorite bool
	// Hidden é quantos recentes a linha "mais…" esconde.
	Hidden int
}

// Offline reports whether there is explicit evidence that the channel is not broadcasting.
func (it ListItem) Offline() bool {
	return it.Availability == catalog.AvailabilityOffline
}

// ID returns the stable identifier of the list item.
func (it ListItem) ID() string {
	if it.Type == ItemTypeMoreRecents {
		return moreRecentsID
	}
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
func (m Model) Categories() []catalog.Category {
	out := make([]catalog.Category, len(m.list.categories))
	copy(out, m.list.categories)
	return out
}

// VisibleCategories returns the categories matching the current filter.
func (m Model) VisibleCategories() []catalog.Category {
	q := strings.ToLower(strings.TrimSpace(m.list.filter))
	if q == "" {
		return m.Categories()
	}
	out := make([]catalog.Category, 0, len(m.list.categories))
	for _, c := range m.list.categories {
		if strings.Contains(strings.ToLower(c.Name), q) {
			out = append(out, c)
		}
	}
	return out
}

// Channels returns a copy of all loaded channels for the active category or search.
func (m Model) Channels() []catalog.Channel {
	out := make([]catalog.Channel, len(m.list.channels))
	copy(out, m.list.channels)
	return out
}

// VisibleChannels returns the channels matching the text filter and the
// language/audience preference filters.
func (m Model) VisibleChannels() []catalog.Channel {
	q := strings.ToLower(strings.TrimSpace(m.list.filter))
	out := make([]catalog.Channel, 0, len(m.list.channels))
	for _, c := range m.list.channels {
		if !m.list.prefs.passes(c) {
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
func (m Model) Favorites() []catalog.Channel {
	out := make([]catalog.Channel, len(m.list.favorites))
	copy(out, m.list.favorites)
	return out
}

// Recents returns a copy of all recent channels.
func (m Model) Recents() []catalog.Channel {
	out := make([]catalog.Channel, len(m.list.recents))
	copy(out, m.list.recents)
	return out
}

func channelMatches(a, b catalog.Channel) bool {
	return a.Matches(b)
}

// VisibleItems returns the deduplicated list of items in strict order.
//
// A tela inicial mostra Favoritos, os primeiros recentes (com a linha "mais…"
// quando sobram) e as categorias; um canal dos favoritos não se repete nos
// recentes. A busca e os canais de uma categoria mostram só os resultados: um
// favorito entre eles fica no lugar dele, marcado com ★, e os favoritos e
// recentes que não são resultado ficam na tela inicial.
func (m Model) VisibleItems() []ListItem {
	p := itemPass{
		query:      strings.ToLower(strings.TrimSpace(m.list.filter)),
		prefs:      m.list.prefs,
		seenLogins: make(map[string]struct{}),
		seenIDs:    make(map[string]struct{}),
	}
	if m.Mode() != ModeCategories {
		p.starred = m.starredListing()
		return m.appendContent(nil, p)
	}
	items := m.appendFavorites(nil, p)
	items = m.appendRecents(items, p)
	return m.appendContent(items, p)
}

// itemPass is the state of one VisibleItems call: the filters a row has to
// pass and the channels already listed, which a later origin skips.
type itemPass struct {
	query      string // text filter, trimmed and lowercased
	prefs      channelPrefs
	seenLogins map[string]struct{}
	seenIDs    map[string]struct{}
	// starred são os índices de m.list.channels que são favoritos e ganham ★.
	starred map[int]bool
}

func (p itemPass) markSeen(ch catalog.Channel) {
	if ch.Login != "" {
		p.seenLogins[strings.ToLower(ch.Login)] = struct{}{}
	}
	if ch.ID != "" {
		p.seenIDs[ch.ID] = struct{}{}
	}
}

func (p itemPass) isSeen(ch catalog.Channel) bool {
	if ch.Login != "" {
		if _, ok := p.seenLogins[strings.ToLower(ch.Login)]; ok {
			return true
		}
	}
	if ch.ID != "" {
		if _, ok := p.seenIDs[ch.ID]; ok {
			return true
		}
	}
	return false
}

// matchesFilter reports whether one of the fields contains the text filter.
func (p itemPass) matchesFilter(name, login, title string) bool {
	if p.query == "" {
		return true
	}
	return strings.Contains(strings.ToLower(name), p.query) ||
		strings.Contains(strings.ToLower(login), p.query) ||
		(title != "" && strings.Contains(strings.ToLower(title), p.query))
}

// passes reports whether ch survives the text filter and the language and
// audience preference filters.
func (p itemPass) passes(ch catalog.Channel) bool {
	title := ""
	if ch.Title != nil {
		title = *ch.Title
	}
	return p.matchesFilter(ch.DisplayName, ch.Login, title) && p.prefs.passes(ch)
}

// channelItem builds a channel row that owns its copy of ch.
func channelItem(itemType ListItemType, ch catalog.Channel, avail catalog.Availability) ListItem {
	return ListItem{Type: itemType, Channel: &ch, Availability: avail}
}

// findLoaded returns the loaded copy of ch when the current listing has
// evidence about its availability.
func (m Model) findLoaded(ch catalog.Channel) (catalog.Channel, bool) {
	for _, c := range m.list.channels {
		if channelMatches(c, ch) && m.listingAvailability(c) != catalog.AvailabilityUnknown {
			return c, true
		}
	}
	return ch, false
}

// resolveSaved returns the channel and the availability to show for a favorite
// or a recent: the loaded copy when the listing has one, the stored one
// otherwise.
func (m Model) resolveSaved(saved catalog.Channel) (catalog.Channel, catalog.Availability) {
	ch := saved
	avail := saved.Availability
	if loaded, ok := m.findLoaded(saved); ok {
		ch = loaded
		avail = m.listingAvailability(loaded)
	}
	if m.player.isPlaying(ch) {
		avail = catalog.AvailabilityLive
	}
	return ch, avail
}

// appendFavorites appends the favorites that pass the filters. A filtered
// favorite is not marked as seen, so it does not hide a later row.
func (m Model) appendFavorites(items []ListItem, p itemPass) []ListItem {
	for _, fav := range m.list.favorites {
		if fav.ID == "" && fav.Login == "" {
			continue
		}
		ch, avail := m.resolveSaved(fav)
		if p.isSeen(ch) || !p.passes(ch) {
			continue
		}
		items = append(items, channelItem(ItemTypeFavorite, ch, avail))
		p.markSeen(ch)
	}
	return items
}

// appendRecents appends the recents that are not favorites. The seen check
// runs on the stored channel and on the resolved one; the seen mark comes
// from the resolved one. Sem filtro de texto e sem o usuário ter pedido o
// resto, vão homeRecentsLimit deles e a linha "mais…" com quantos sobram.
func (m Model) appendRecents(items []ListItem, p itemPass) []ListItem {
	limited := p.query == "" && !m.list.recentsExpanded
	shown, hidden := 0, 0
	for _, rec := range m.list.recents {
		if rec.ID == "" && rec.Login == "" {
			continue
		}
		if p.isSeen(rec) {
			continue
		}
		ch, avail := m.resolveSaved(rec)
		if p.isSeen(ch) || !p.passes(ch) {
			continue
		}
		if limited && shown == homeRecentsLimit {
			hidden++
			continue
		}
		items = append(items, channelItem(ItemTypeRecent, ch, avail))
		p.markSeen(ch)
		shown++
	}
	if hidden > 0 {
		items = append(items, ListItem{Type: ItemTypeMoreRecents, Hidden: hidden})
	}
	return items
}

// appendContent appends what the current screen lists below the saved
// channels.
func (m Model) appendContent(items []ListItem, p itemPass) []ListItem {
	switch m.Mode() {
	case ModeCategories:
		items = appendCategories(items, m.list.categories, p)
	case ModeSearch:
		// An exact channel hit first (v1.9 ticket 11), then categories, then the
		// other channels (ticket 09).
		items = m.appendListed(items, p, ItemTypeExactChannel)
		items = appendCategories(items, m.list.search.categories, p)
		items = m.appendListed(items, p, ItemTypeChannel)
	case ModeChannels:
		items = m.appendListed(items, p, ItemTypeChannel)
	}
	return items
}

// appendCategories appends the categories that match the text filter.
// Categories are not deduplicated.
func appendCategories(items []ListItem, cats []catalog.Category, p itemPass) []ListItem {
	for _, cat := range cats {
		if !p.matchesFilter(cat.Name, "", "") {
			continue
		}
		items = append(items, ListItem{Type: ItemTypeCategory, Category: &cat})
	}
	return items
}

// appendListed appends the channels of the loaded listing that are not in the
// list yet and pass the filters. With ItemTypeExactChannel it takes only the
// channels the search query names exactly.
func (m Model) appendListed(items []ListItem, p itemPass, itemType ListItemType) []ListItem {
	for i, ch := range m.list.channels {
		if itemType == ItemTypeExactChannel && !catalog.ExactChannelMatch(m.list.search.query, ch) {
			continue
		}
		if p.isSeen(ch) || !p.passes(ch) {
			continue
		}
		avail := m.listingAvailability(ch)
		if m.player.isPlaying(ch) {
			avail = catalog.AvailabilityLive
		}
		it := channelItem(itemType, ch, avail)
		it.Favorite = p.starred[i]
		items = append(items, it)
		p.markSeen(ch)
	}
	return items
}

// starredListing devolve os índices dos canais da listagem que são favoritos.
// Cada favorito marca um canal só: o de mesmo ID e, sem ele, o primeiro que
// casa pelo login. Um login retomado por outro canal não leva a ★ do favorito.
func (m Model) starredListing() map[int]bool {
	if len(m.list.favorites) == 0 || len(m.list.channels) == 0 {
		return nil
	}
	out := make(map[int]bool)
	for _, fav := range m.list.favorites {
		idx := -1
		if fav.ID != "" {
			idx = slices.IndexFunc(m.list.channels, func(ch catalog.Channel) bool {
				return ch.ID == fav.ID && channelMatches(fav, ch)
			})
		}
		if idx < 0 {
			idx = slices.IndexFunc(m.list.channels, func(ch catalog.Channel) bool { return channelMatches(fav, ch) })
		}
		if idx >= 0 {
			out[idx] = true
		}
	}
	return out
}

// listingAvailability resolves the availability of a channel from the loaded
// listing: explicit evidence from the Source wins; a viewer count only exists
// for an active stream; a category listing only contains live streams.
// Anything else is unknown — a missing viewer count is never offline.
func (m Model) listingAvailability(ch catalog.Channel) catalog.Availability {
	if ch.Availability != catalog.AvailabilityUnknown {
		return ch.Availability
	}
	if ch.Viewers != nil {
		return catalog.AvailabilityLive
	}
	if m.Mode() == ModeChannels {
		return catalog.AvailabilityLive
	}
	return catalog.AvailabilityUnknown
}

// isPlaying reports whether ch is the channel the player is showing now.
// A channel being played is live regardless of what the store says.
func (p playbackState) isPlaying(ch catalog.Channel) bool {
	if p.login == "" || ch.Login == "" {
		return false
	}
	switch p.state {
	case PlayerStatePlaying, PlayerStatePaused, PlayerStateBreak:
		if platform.IsNonTwitch(ch.Platform) {
			return strings.EqualFold(p.login, ch.PlayerLabel())
		}
		return strings.EqualFold(p.login, ch.Login)
	default:
		return false
	}
}

// markChannelOffline records explicit offline evidence for the channel identified by label
// in the channels listing, favorites and recents. Only the matching channel is
// touched: viewers of other channels stay intact. The slices are copied so
// earlier Model values that share a backing array are not mutated.
func (m *Model) markChannelOffline(label string) {
	if strings.TrimSpace(label) == "" {
		return
	}
	apply := func(chs []catalog.Channel) []catalog.Channel {
		out := make([]catalog.Channel, len(chs))
		copy(out, chs)
		for i := range out {
			if !strings.EqualFold(out[i].PlayerLabel(), label) {
				continue
			}
			out[i].Availability = catalog.AvailabilityOffline
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
func (m *Model) refreshSavedAvailability(loaded []catalog.Channel) {
	m.list.favorites = m.withLoadedAvailability(m.list.favorites, loaded)
	m.list.recents = m.withLoadedAvailability(m.list.recents, loaded)
	if m.list.pendingRecents != nil {
		m.list.pendingRecents = m.withLoadedAvailability(m.list.pendingRecents, loaded)
	}
}

// withLoadedAvailability returns a copy of saved with the evidence from loaded
// applied. It never mutates saved, whose backing array may be shared with
// earlier Model values.
func (m Model) withLoadedAvailability(saved, loaded []catalog.Channel) []catalog.Channel {
	out := make([]catalog.Channel, len(saved))
	copy(out, saved)
	for i := range out {
		for _, c := range loaded {
			if !channelMatches(out[i], c) {
				continue
			}
			avail := m.listingAvailability(c)
			if avail == catalog.AvailabilityUnknown {
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
			if avail == catalog.AvailabilityOffline {
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
// A load error replaces the rows on screen, so no row is highlighted under it;
// an unavailable catalog leaves them there.
func (m Model) HighlightedItem() (ListItem, bool) {
	if m.list.highlightedID == "" || m.list.hardErr() {
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
func (m Model) HighlightedCategory() (catalog.Category, bool) {
	it, ok := m.HighlightedItem()
	if !ok || it.Type != ItemTypeCategory || it.Category == nil {
		return catalog.Category{}, false
	}
	return *it.Category, true
}

// HighlightedChannel returns the currently highlighted channel by ID.
func (m Model) HighlightedChannel() (catalog.Channel, bool) {
	it, ok := m.HighlightedItem()
	if !ok || it.Channel == nil {
		return catalog.Channel{}, false
	}
	return *it.Channel, true
}

// SelectedCategory returns the last category confirmed with Enter, or nil if none.
func (m Model) SelectedCategory() *catalog.Category {
	if m.list.selectedCategory == nil {
		return nil
	}
	c := *m.list.selectedCategory
	return &c
}

// SelectedChannel returns the last channel confirmed with Enter, or nil if none.
func (m Model) SelectedChannel() *catalog.Channel {
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
	m.list.channels = nil
	m.clearStaleStatusErrors()
	m.list.nextCursor = ""
	m.list.hasMore = false
	m.list.focusResults = false
	if m.list.selectedCategory != nil && m.list.selectedCategory.ID != "" {
		m.list.highlightedID = m.list.selectedCategory.ID
		m.list.highlightedLogin = ""
	}
	m.reconcileHighlight()
	if m.browser == nil {
		m.list.loading = false
		// No Browser to reload, but the hydrate generation moved: ask again so
		// saved channels do not stay unverified.
		return m, m.withHydrate(nil)
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
	if m.browser == nil {
		m.list.loading = false
		// No Browser to reload, but the hydrate generation moved: ask again so
		// saved channels do not stay unverified.
		return m, m.withHydrate(nil)
	}
	m.list.loading = true
	return m, m.withHydrate(m.fetchChannelsCmd(m.list.generation, cat, "", false))
}

func (m Model) retryCurrentMode() (tea.Model, tea.Cmd) {
	if m.browser == nil && m.searcher == nil && len(m.resolvers) == 0 {
		return m, nil
	}
	m.list.tracker.cancelActive()
	m.list.generation++
	m.list.hydrateGen++
	m.applyPendingRecents()
	m.reconcileHighlight()
	m.list.loading = true
	m.list.err = nil
	m.list.nextCursor = ""
	m.list.hasMore = false

	switch m.Mode() {
	case ModeChannels:
		if m.list.selectedCategory != nil {
			if m.browser == nil {
				m.list.loading = false
				return m, m.withHydrate(nil)
			}
			return m, m.withHydrate(m.fetchChannelsCmd(m.list.generation, *m.list.selectedCategory, "", false))
		}
		m.list.mode = ModeCategories
		m.list.channels = nil
		if m.browser == nil {
			m.list.loading = false
			return m, m.withHydrate(nil)
		}
		return m, m.withHydrate(m.fetchCategoriesCmd(m.list.generation, "", false))
	case ModeSearch:
		if strings.TrimSpace(m.list.search.query) != "" {
			if m.searcher == nil && m.browser == nil {
				m.list.loading = false
				return m, m.withHydrate(nil)
			}
			return m, m.withHydrate(m.fetchSearchCmd(m.list.generation, m.list.search.query))
		}
		m.list.mode = ModeCategories
		m.list.channels = nil
		if m.browser == nil {
			m.list.loading = false
			return m, m.withHydrate(nil)
		}
		return m, m.withHydrate(m.fetchCategoriesCmd(m.list.generation, "", false))
	default:
		if m.browser == nil {
			m.list.loading = false
			return m, m.withHydrate(nil)
		}
		return m, m.withHydrate(m.fetchCategoriesCmd(m.list.generation, "", false))
	}
}

// queueRecent records ch as the newest recent for persistence without changing
// the visible list. VisibleItems keeps using m.recents until applyPendingRecents
// runs on the next list load.
func (m *Model) queueRecent(ch catalog.Channel) {
	base := m.list.recents
	if m.list.pendingRecents != nil {
		base = m.list.pendingRecents
	}
	filtered := make([]catalog.Channel, 0, len(base))
	for _, r := range base {
		if !channelMatches(r, ch) {
			filtered = append(filtered, r)
		}
	}
	m.list.pendingRecents = append([]catalog.Channel{ch}, filtered...)
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
func (m *Model) ToggleFavorite(ch catalog.Channel) bool {
	for i, fav := range m.list.favorites {
		if channelMatches(fav, ch) {
			m.list.favorites = slices.Concat(m.list.favorites[:i], m.list.favorites[i+1:])
			return false
		}
	}
	m.list.favorites = append(m.list.favorites, ch)
	return true
}

func (l listState) toStoreState() store.State {
	recs := l.recents
	if l.pendingRecents != nil {
		recs = l.pendingRecents
	}
	return store.State{
		Favorites: store.FromChannels(l.favorites),
		Recents:   store.FromChannels(recs),
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
	st := m.list.toStoreState()
	s := m.store
	confirmation := ""
	if len(notice) > 0 {
		confirmation = notice[0]
	}
	return m.storeSaves.cmd(func() tea.Msg {
		return storeSavedMsg{Err: s.Save(st), Notice: confirmation}
	})
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
	case ItemTypeMoreRecents:
		// O destaque fica na mesma posição, agora no primeiro recente que
		// estava escondido.
		m.list.recentsExpanded = true
		m.reconcileHighlight()
		return m, nil

	case ItemTypeCategory:
		if it.Category == nil {
			return m, nil
		}
		selected := *it.Category
		m.list.selectedCategory = &selected
		if m.browser == nil {
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
		m.list.enters++
		enter := m.list.enters
		// Recents are queued for the store in ChannelSelectedMsg; the visible
		// order stays frozen so Enter does not land on a shifted row.
		return m, func() tea.Msg {
			return ChannelSelectedMsg{Channel: selected, Enter: enter}
		}
	}
}

// stampChannelCategory fills CategoryID/CategoryName from the open category
// when the channel itself does not already carry them (category listing rows).
func stampChannelCategory(ch *catalog.Channel, cat *catalog.Category) {
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
	items := m.VisibleItems()
	if len(items) == 0 {
		// Sem linha nenhuma, a seta não escolhe nada: a carga em andamento
		// ainda destaca o primeiro resultado.
		m.clearHighlight()
		return
	}
	// A manual move is the user's choice; a pending load must not override it.
	m.list.focusResults = false
	idx := m.list.indexOfHighlighted(items)
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
func (l listState) indexOfHighlighted(items []ListItem) int {
	if l.highlightedID != "" {
		for i, it := range items {
			if it.ID() == l.highlightedID {
				return i
			}
		}
	}
	if l.highlightedLogin != "" {
		for i, it := range items {
			if it.Channel != nil && strings.EqualFold(it.Channel.Login, l.highlightedLogin) {
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
	if idx := m.list.indexOfHighlighted(items); idx >= 0 {
		m.setHighlightToItem(items, idx)
		return
	}
	m.setHighlightToItem(items, m.list.highlightIndex)
}

// settleHighlightAfterLoad places the highlight after a channel or search page
// arrives. Right after a category or search is opened, the highlight goes to
// the first result of that screen, so Enter opens what the user just asked for.
// Search results are preferred
// by catalog.PreferSearchChannels before this runs (exact login, then exact
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
// (PreferSearchCategories), then channels[0]. Um favorito entre os resultados
// fica no lugar dele, com ★, e recebe o destaque como os outros. An exact hit
// hidden by the preference filters has no row, so the categories take over.
func (m Model) firstResultID() (string, bool) {
	items := m.VisibleItems()
	if m.Mode() == ModeSearch {
		for _, ch := range m.list.channels {
			if !catalog.ExactChannelMatch(m.list.search.query, ch) {
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
	if m.browser == nil || m.list.loading || !m.list.hasMore || m.list.nextCursor == "" {
		return false
	}
	if m.Mode() == ModeSearch {
		return false
	}
	if m.list.softErr() {
		// The site refused the last page. Scrolling must not ask it again on
		// every key (a held key would hammer the wall that blocked the app);
		// "r" is the way to try again, as the notice says.
		return false
	}
	ids := m.visibleItemIDs()
	if len(ids) == 0 {
		return false
	}
	return ids[len(ids)-1] == m.list.highlightedID
}

func (m Model) fetchCategoriesCmd(gen uint64, cursor string, appendPage bool) tea.Cmd {
	browser := m.browser
	if browser == nil {
		return nil
	}
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
		page, err := browser.Categories(ctx, catalog.PageRequest{
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

func (m Model) fetchChannelsCmd(gen uint64, cat catalog.Category, cursor string, appendPage bool) tea.Cmd {
	browser := m.browser
	if browser == nil {
		return nil
	}
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
		page, err := browser.Channels(ctx, catID, catalog.PageRequest{
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

// savedIDs returns the unique stable identifiers of the favorites and recents,
// grouped by canonical platform. An identifier only means something on its own
// platform: "xqc" is one channel on Twitch and another on Kick.
func (l listState) savedIDs() map[string][]string {
	type key struct{ plat, id string }
	seen := make(map[key]struct{}, len(l.favorites)+len(l.recents)+len(l.pendingRecents))
	out := make(map[string][]string)
	add := func(ch catalog.Channel) {
		id := strings.TrimSpace(ch.StableID())
		if id == "" {
			return
		}
		plat := ch.CanonicalPlatform()
		k := key{plat: plat, id: id}
		if plat != platform.YouTube {
			// Identifiers fold case everywhere but on YouTube.
			k.id = strings.ToLower(id)
		}
		if _, ok := seen[k]; ok {
			return
		}
		seen[k] = struct{}{}
		out[plat] = append(out[plat], id)
	}
	for _, ch := range l.favorites {
		add(ch)
	}
	for _, ch := range l.recents {
		add(ch)
	}
	for _, ch := range l.pendingRecents {
		add(ch)
	}
	return out
}

// fetchHydrateCmd looks up Availability for favorites and recents, each
// platform's channels with that platform's resolver. Saved channels of a
// platform with no resolver stay unverified. With nothing to ask it returns nil
// so no request is issued.
func (m Model) fetchHydrateCmd(gen uint64) tea.Cmd {
	return resolveCmd(gen, m.resolvers, m.list.savedIDs())
}

// fetchChannelStateCmd looks up the Availability of ch alone, for a channel
// opened with no evidence of its state (a pasted URL).
func (m Model) fetchChannelStateCmd(ch catalog.Channel) tea.Cmd {
	id := strings.TrimSpace(ch.StableID())
	if id == "" {
		return nil
	}
	lookup := resolveCmd(0, m.resolvers, map[string][]string{ch.CanonicalPlatform(): {id}})
	if lookup == nil {
		return nil
	}
	return func() tea.Msg {
		msg, ok := lookup().(SavedAvailabilityMsg)
		if !ok {
			return nil
		}
		msg.ChannelLookup = true
		return msg
	}
}

// resolveCmd asks each platform in ids its resolver, all at once, and delivers
// what the ones that answered know. A platform that fails does not take the
// others with it; Err is set only when none answered.
func resolveCmd(gen uint64, resolvers map[string]catalog.Resolver, ids map[string][]string) tea.Cmd {
	type lookup struct {
		plat     string
		resolver catalog.Resolver
		ids      []string
	}
	var lookups []lookup
	for plat, platIDs := range ids {
		if r := resolvers[plat]; r != nil && len(platIDs) > 0 {
			lookups = append(lookups, lookup{plat: plat, resolver: r, ids: platIDs})
		}
	}
	if len(lookups) == 0 {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), hydrationTimeout)
		defer cancel()

		results := make([][]catalog.Channel, len(lookups))
		errs := make([]error, len(lookups))
		var wg sync.WaitGroup
		for i, l := range lookups {
			wg.Add(1)
			go func() {
				defer wg.Done()
				channels, err := l.resolver.ChannelsByIDs(ctx, l.ids)
				if err != nil {
					errs[i] = err
					return
				}
				// A resolver answers for its platform; say so where it did not.
				for j := range channels {
					if channels[j].Platform == "" {
						channels[j].Platform = l.plat
					}
				}
				results[i] = channels
			}()
		}
		wg.Wait()
		msg := SavedAvailabilityMsg{Generation: gen}
		failed := 0
		for i := range lookups {
			if errs[i] != nil {
				failed++
				if msg.Err == nil {
					msg.Err = errs[i]
				}
				continue
			}
			msg.Channels = append(msg.Channels, results[i]...)
		}
		if failed < len(lookups) {
			msg.Err = nil
		}
		return msg
	}
}
