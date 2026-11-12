package ui

import (
	"context"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/store"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

// defaultCategoryPageSize is the initial categories page size. Twitch's
// games(first:) accepts at most 100; pagination after the first page is
// blocked by integrity, so the first request uses the API maximum.
const defaultCategoryPageSize = 100

// Mode identifies the active navigation screen in the left list panel.
type Mode string

const (
	ModeCategories Mode = "categories"
	ModeChannels   Mode = "channels"
	ModeSearch     Mode = "search"
)

// Focus identifies which panel currently holds keyboard focus.
type Focus string

const (
	FocusList Focus = "list"
	FocusChat Focus = "chat"
)

// CategoriesLoadedMsg delivers the result of an asynchronous Categories page request.
type CategoriesLoadedMsg struct {
	Generation uint64
	Page       twitch.Page[twitch.Category]
	Append     bool
	Err        error
}

// ChannelsLoadedMsg delivers the result of an asynchronous Channels page request.
type ChannelsLoadedMsg struct {
	Generation uint64
	Category   twitch.Category
	Page       twitch.Page[twitch.Channel]
	Append     bool
	Err        error
}

// SearchLoadedMsg delivers the result of an asynchronous Search request.
// Categories come from SearchCategories and render above channel hits.
type SearchLoadedMsg struct {
	Generation uint64
	Query      string
	Page       twitch.Page[twitch.Channel]
	Categories []twitch.Category
	Err        error
}

// SavedAvailabilityMsg delivers a batch availability lookup for favorites and
// recents. Failures leave saved channels unverified and never set Model.err.
type SavedAvailabilityMsg struct {
	Generation uint64
	Channels   []twitch.Channel
	Err        error
}

// CategorySelectedMsg is emitted when the user presses Enter on a highlighted category
// when no Source is attached.
type CategorySelectedMsg struct {
	Category twitch.Category
}

// ChannelSelectedMsg is emitted when the user presses Enter on a highlighted channel.
type ChannelSelectedMsg struct {
	Channel twitch.Channel
	// Enter is the count of Enters on a channel row up to this one. A message
	// from an earlier Enter that arrives after a later one is dropped.
	Enter uint64
}

type requestTracker struct {
	mu     sync.Mutex
	cancel context.CancelFunc
}

func (rt *requestTracker) newContext() (context.Context, context.CancelFunc) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.cancel != nil {
		rt.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	rt.cancel = cancel
	return ctx, cancel
}

func (rt *requestTracker) cancelActive() {
	if rt == nil {
		return
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.cancel != nil {
		rt.cancel()
		rt.cancel = nil
	}
}

// TokenCheckFunc validates the configured token asynchronously on startup.
// Store defines the persistence contract required by the UI for saving state.
type Store interface {
	Save(state store.State) error
}

type TokenCheckFunc func(ctx context.Context) error

// AuthValidatedMsg delivers the result of the startup token validation check.
type AuthValidatedMsg struct {
	Err error
}

// Option configures a UI Model at construction time.
type Option func(*Model)

// WithSource configures the Model to load data from src.
func WithSource(src twitch.Source) Option {
	return func(m *Model) {
		m.source = src
		if src != nil {
			m.list.loading = true
		}
	}
}

// WithPageSize sets the number of items requested per page.
func WithPageSize(size int) Option {
	return func(m *Model) {
		if size > 0 {
			m.list.pageSize = size
		}
	}
}

// WithTokenCheck configures an asynchronous startup token check. If it returns
// an error, the status bar shows a one-time renewal notice without interrupting navigation.
func WithTokenCheck(fn TokenCheckFunc) Option {
	return func(m *Model) {
		m.tokenCheck = fn
	}
}

// WithStore configures a persistent store for favorites and recents.
func WithStore(s Store) Option {
	return func(m *Model) {
		m.store = s
	}
}

// WithFavorites sets the initial favorites.
func WithFavorites(favs []twitch.Channel) Option {
	return func(m *Model) {
		m.list.favorites = make([]twitch.Channel, len(favs))
		copy(m.list.favorites, favs)
	}
}

// WithRecents sets the initial recents.
func WithRecents(recs []twitch.Channel) Option {
	return func(m *Model) {
		m.list.recents = make([]twitch.Channel, len(recs))
		copy(m.list.recents, recs)
	}
}

// WithCorruptNotice initializes the Model with a one-time corrupt state file warning.
func WithCorruptNotice(notice string) Option {
	return func(m *Model) {
		m.notices.corrupt = notice
	}
}

// Model is the Bubble Tea model for ttv-tui. Infrastructure sits directly on
// it; the rest of the state is grouped by subject. The groups are values, not
// pointers: Bubble Tea copies Model by value and a copy must not share state.
type Model struct {
	catalog    *i18n.Catalog
	source     twitch.Source
	keys       keyMap
	tokenCheck TokenCheckFunc
	store      Store
	storeSaves *saveSlot
	playback   Playback
	list       listState
	player     playbackState
	chat       chatState
	layout     layoutState
	hosted     hostedState
	notices    noticeState
	width      int
	height     int
	quitting   bool
}

// listState is the left panel: what is loaded, what is highlighted and
// selected, and the text filter, search and channel preference filters over it.
type listState struct {
	tracker    *requestTracker
	pageSize   int
	mode       Mode
	generation uint64
	hydrateGen uint64
	loading    bool
	err        error
	loadFailed bool // the last load failed; an empty list is missing data, not an empty result
	categories []twitch.Category
	channels   []twitch.Channel
	nextCursor string
	hasMore    bool
	filter     string
	filtering  bool
	search     searchState
	prefs      channelPrefs

	highlightedID    string
	highlightedLogin string // secondary channel identity when upstream IDs churn between loads
	highlightIndex   int    // VisibleItems index; nearest-neighbor fallback after refresh
	focusResults     bool   // next channel/search load highlights its first result; see settleHighlightAfterLoad
	selectedCategory *twitch.Category
	selectedChannel  *twitch.Channel
	// enters counts Enter on a channel row; see ChannelSelectedMsg.Enter.
	enters uint64

	favorites []twitch.Channel
	recents   []twitch.Channel
	// pendingRecents is the store-ordered recent list after a channel is opened.
	// VisibleItems keeps using recents until the next list load (refresh, back
	// to categories, reopen) applies this slice, so selection does not jump rows.
	pendingRecents []twitch.Channel
}

// searchState is the Ctrl+F search over the list.
type searchState struct {
	active bool
	input  string
	query  string
	// origin is the list mode active when the current search started
	// (ModeChannels or ModeCategories). Esc returns there; Ctrl+R ignores it.
	origin Mode
	// categories holds category hits for ModeSearch only. Kept separate from
	// the list categories so Esc back to the live top list does not briefly
	// show search results while the reload runs.
	categories []twitch.Category
}

// channelPrefs holds the channel preference filters (ticket 01) and their
// menu. Empty languages and min 0 mean no preference filter. Unknown-language
// channels stay visible when languages are selected (marked in the row).
type channelPrefs struct {
	languages  []string
	minViewers int
	persist    ChannelFilterPersistFunc
	saves      *saveSlot
	menuOpen   bool
	menuIndex  int
	menuItems  []channelFilterMenuItem
}

// playbackState is what the UI knows about the Player.
type playbackState struct {
	gen uint64
	// selection numbers the Starts asked for (channel chosen, r). awaiting is
	// the one whose reply the UI waits for during a switch, 0 when none.
	selection    uint64
	awaiting     uint64
	state        PlayerState
	login        string
	lastLine     string
	infoLine     string
	failure      PlayerFailure
	breakElapsed time.Duration
	volume       int
	// windowNotified is set once the first video of the session reached
	// playback; windowNotice keeps the one-time notice on screen until the
	// next key press.
	windowNotified bool
	windowNotice   bool
	// keysWarnedGen is the last generation warned about its mpv window having
	// no keys.
	keysWarnedGen        uint64
	containmentWarnedGen uint64
	// stopAfterStart is a Ctrl+X pressed during the switch. The Start reply
	// it outran ends as stopped, and its playback is stopped again.
	stopAfterStart bool
}

// chatState is the chat session, its panel and the message being composed.
type chatState struct {
	session      Chat
	panel        chatPanel
	composing    bool
	composeDraft string
	// copyNotice is the transient copy status after yanking a chat message with
	// y (success names the clipboard path; failure is an explicit error).
	// copyNoticeGen invalidates stale clear ticks; copyNoticeUntil records when
	// the notice should disappear (tests advance nowFunc).
	copyNotice      string
	copyNoticeGen   uint64
	copyNoticeUntil time.Time
	nowFunc         func() time.Time
}

// layoutState is the focus, the help modal and the session layout flags.
type layoutState struct {
	focus       Focus
	helpVisible bool
	helpScroll  int // first help row on screen; clamped by helpOffset
	// showList / showChat are the session layout flags (ticket 02). Both start true.
	// Ctrl+W cycles named modes; Ctrl+H / Ctrl+L toggle each flag independently.
	// Layout is not persisted to disk.
	showList bool
	showChat bool
}

// hostedState is the single window: the video host and the panels over it.
type hostedState struct {
	host          VideoHost
	published     HostPanel
	chatOverlay   bool
	panelsSwapped bool
	// chatOverlayOpacity is the percent asked for; 0 means the default.
	chatOverlayOpacity int
	// listCols and chatCols are the widths asked for; 0 means automatic.
	listCols int
	chatCols int
	persist  HostedColumnsPersist
	saveGen  int
	// queuedGen is the newest saveGen handed to saves.
	queuedGen int
	saves     *saveSlot
}

// noticeState holds the status bar notices.
type noticeState struct {
	auth        string
	corrupt     string
	status      string
	statusWarns bool
	gen         uint64
	// ctrlC is the transient "To quit use Ctrl+Q" status after Ctrl+C. It
	// outranks filter/search prompts so the hint stays visible in every mode.
	ctrlC string
}

// New creates a new UI Model backed by the given i18n catalog and options.
func New(catalog *i18n.Catalog, opts ...Option) Model {
	// Only auto-detect once per process; tests may override via
	// forceOrientationColorsForTest without New resetting them.
	colorMu.RLock()
	inited := colorInited
	colorMu.RUnlock()
	if !inited {
		configureOrientationColors()
	}
	if catalog == nil {
		catalog, _ = i18n.New(i18n.LangEN)
	}
	m := Model{
		catalog: catalog,
		keys:    defaultKeyMap(),
		list: listState{
			tracker:    &requestTracker{},
			pageSize:   defaultCategoryPageSize,
			mode:       ModeCategories,
			generation: 1,
			hydrateGen: 1,
			favorites:  make([]twitch.Channel, 0),
			recents:    make([]twitch.Channel, 0),
			prefs:      channelPrefs{saves: &saveSlot{}},
		},
		player:     playbackState{volume: 100},
		chat:       chatState{panel: newChatPanel()},
		layout:     layoutState{focus: FocusList, showList: true, showChat: true},
		hosted:     hostedState{saves: &saveSlot{}},
		storeSaves: &saveSlot{},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&m)
		}
	}
	return m
}

// AuthNotice returns the active one-time authentication notice in the status bar, if any.
func (m Model) AuthNotice() string {
	return m.notices.auth
}

// CorruptNotice returns the active one-time state corruption notice in the status bar, if any.
func (m Model) CorruptNotice() string {
	return m.notices.corrupt
}

// StatusNotice returns the active one-time status notice in the status bar, if any.
func (m Model) StatusNotice() string {
	return m.notices.status
}

// CtrlCNotice returns the transient "use Ctrl+Q to quit" hint after Ctrl+C, if any.
func (m Model) CtrlCNotice() string {
	return m.notices.ctrlC
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	var cmds []tea.Cmd
	if m.source != nil {
		cmds = append(cmds, m.fetchCategoriesCmd(m.list.generation, "", false))
		if hydrate := m.fetchHydrateCmd(m.list.hydrateGen); hydrate != nil {
			cmds = append(cmds, hydrate)
		}
	}
	if m.tokenCheck != nil {
		cmds = append(cmds, m.checkTokenCmd())
	}
	switch len(cmds) {
	case 0:
		return nil
	case 1:
		return cmds[0]
	default:
		return tea.Batch(cmds...)
	}
}

// Mode returns the active list mode (categories, channels, or search).
func (m Model) Mode() Mode {
	if m.list.mode == "" {
		return ModeCategories
	}
	return m.list.mode
}

// Focus returns the currently effective focused panel (FocusList or FocusChat).
// When chat is hidden by width or layout, effective focus degrades to the
// remaining panel while preserving the underlying focus for when it returns.
func (m Model) Focus() Focus {
	if m.layout.focus == "" {
		return FocusList
	}
	listOn := m.listVisible()
	chatOn := m.chatColumnVisible()
	switch {
	case listOn && chatOn:
		return m.layout.focus
	case chatOn:
		return FocusChat
	default:
		return FocusList
	}
}

// HelpVisible reports whether the keyboard shortcuts help view is currently open.
func (m Model) HelpVisible() bool {
	return m.layout.helpVisible
}

// Width returns the last known terminal width in columns.
func (m Model) Width() int {
	return m.width
}

// Height returns the last known terminal height in rows.
func (m Model) Height() int {
	return m.height
}

// Quitting reports whether the user has triggered application exit.
func (m Model) Quitting() bool {
	return m.quitting
}

// Loading reports whether an asynchronous Source request is in flight.
func (m Model) Loading() bool {
	return m.list.loading
}

// Err returns the current error state, if any.
func (m Model) Err() error {
	return m.list.err
}

// Generation returns the current request generation counter.
func (m Model) Generation() uint64 {
	return m.list.generation
}

// CancelActiveRequest cancels any in-flight Source request and advances the generation
// so late responses from the canceled screen are ignored.
func (m Model) CancelActiveRequest() Model {
	m.list.tracker.cancelActive()
	m.list.generation++
	m.list.loading = false
	return m
}

func (m Model) checkTokenCmd() tea.Cmd {
	fn := m.tokenCheck
	if fn == nil {
		return nil
	}
	return func() tea.Msg {
		return AuthValidatedMsg{Err: fn(context.Background())}
	}
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	if model, ok := next.(Model); ok {
		return model.syncHostPanel(), cmd
	}
	return next, cmd
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case hostedNoticeMsg:
		return m.applyHostedNotice(msg)
	case hostedSaveMsg:
		cmd := m.hosted.saveColumns(msg.gen)
		if cmd != nil {
			m.hosted.queuedGen = msg.gen
		}
		return m, cmd
	case tea.BatchMsg:
		return m.applyBatch(msg)

	case AuthValidatedMsg:
		return m.applyAuthValidated(msg)

	case copyResultMsg:
		return m.applyCopyResult(msg)

	case copyNoticeClearMsg:
		return m.clearCopyNoticeIfExpired(msg.gen), nil

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case CategoriesLoadedMsg:
		return m.applyCategoriesLoaded(msg)

	case ChannelsLoadedMsg:
		return m.applyChannelsLoaded(msg)

	case SearchLoadedMsg:
		return m.applySearchLoaded(msg)

	case SavedAvailabilityMsg:
		return m.applySavedAvailability(msg)

	case ChannelSelectedMsg:
		return m.applyChannelSelected(msg)

	case storeSavedMsg:
		return m.applyStoreSaved(msg)

	case configSavedMsg:
		return m.applyConfigSaved(msg)

	case ChatEventMsg:
		return m.handleChatEvent(msg)

	case PlayerStatusMsg:
		return m.applyPlayerStatus(msg)

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	}
	return m, nil
}

// warnStatus leads the bar with a warning until the next key.
func (m *Model) warnStatus(notice string) {
	m.notices.gen++
	m.notices.status = notice
	m.notices.statusWarns = true
}

func (m *Model) clearStatusNotice() {
	m.notices.gen++
	m.notices.status = ""
	m.notices.statusWarns = false
}

// clearVolumeLimitNotice drops a limit notice the player has outdated. A change
// made in the mpv window reaches no key, so it cannot wait for one.
func (m *Model) clearVolumeLimitNotice() {
	if m.notices.status == m.t("player.vol_max") || m.notices.status == m.t("player.vol_min") {
		m.clearStatusNotice()
	}
}

func (m Model) t(key string) string {
	if m.catalog == nil {
		return key
	}
	return m.catalog.T(key)
}
