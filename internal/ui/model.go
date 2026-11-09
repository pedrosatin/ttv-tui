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
			m.loading = true
		}
	}
}

// WithPageSize sets the number of items requested per page.
func WithPageSize(size int) Option {
	return func(m *Model) {
		if size > 0 {
			m.pageSize = size
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
		m.favorites = make([]twitch.Channel, len(favs))
		copy(m.favorites, favs)
	}
}

// WithRecents sets the initial recents.
func WithRecents(recs []twitch.Channel) Option {
	return func(m *Model) {
		m.recents = make([]twitch.Channel, len(recs))
		copy(m.recents, recs)
	}
}

// WithCorruptNotice initializes the Model with a one-time corrupt state file warning.
func WithCorruptNotice(notice string) Option {
	return func(m *Model) {
		m.corruptNotice = notice
	}
}

// Model is the Bubble Tea model for ttv-tui.
type Model struct {
	catalog     *i18n.Catalog
	source      twitch.Source
	tracker     *requestTracker
	keys        keyMap
	pageSize    int
	mode        Mode
	focus       Focus
	helpVisible bool
	helpScroll  int // first help row on screen; clamped by helpOffset
	generation  uint64
	hydrateGen  uint64
	loading     bool
	err         error
	categories  []twitch.Category
	channels    []twitch.Channel
	// searchCategories holds category hits for ModeSearch only. Kept separate
	// from categories so Esc back to the live top list does not briefly show
	// search results while the reload runs.
	searchCategories []twitch.Category
	nextCursor       string
	hasMore          bool
	filter           string
	filtering        bool
	searching        bool
	searchInput      string
	searchQuery      string
	// searchOrigin is the list mode active when the current search started
	// (ModeChannels or ModeCategories). Esc returns there; Ctrl+R ignores it.
	searchOrigin Mode
	// filterLanguages / filterMinViewers are channel preference filters (ticket 01).
	// Empty languages and min 0 mean no preference filter. Unknown-language
	// channels stay visible when languages are selected (marked in the row).
	filterLanguages        []string
	filterMinViewers       int
	filterPersist          ChannelFilterPersistFunc
	channelFilterMenu      bool
	channelFilterMenuIndex int
	channelFilterMenuItems []channelFilterMenuItem
	highlightedID          string
	highlightedLogin       string // secondary channel identity when upstream IDs churn between loads
	highlightIndex         int    // VisibleItems index; nearest-neighbor fallback after refresh
	focusResults           bool   // next channel/search load highlights its first result; see settleHighlightAfterLoad
	selectedCategory       *twitch.Category
	selectedChannel        *twitch.Channel
	tokenCheck             TokenCheckFunc
	authNotice             string
	corruptNotice          string
	statusNotice           string
	statusNoticeWarns      bool
	// ctrlCNotice is the transient "To quit use Ctrl+Q" status after Ctrl+C.
	// It outranks filter/search prompts so the hint stays visible in every mode.
	ctrlCNotice string
	// copyNotice is the transient copy status after yanking a chat message with
	// y (success names the clipboard path; failure is an explicit error).
	// copyNoticeGen invalidates stale clear ticks; copyNoticeUntil records when
	// the notice should disappear (tests advance nowFunc).
	copyNotice      string
	copyNoticeGen   uint64
	copyNoticeUntil time.Time
	nowFunc         func() time.Time
	store           Store
	favorites       []twitch.Channel
	recents         []twitch.Channel
	// pendingRecents is the store-ordered recent list after a channel is opened.
	// VisibleItems keeps using recents until the next list load (refresh, back
	// to categories, reopen) applies this slice, so selection does not jump rows.
	pendingRecents     []twitch.Channel
	playback           Playback
	playerGen          uint64
	playerSwitching    bool
	playerState        PlayerState
	playerLogin        string
	playerLastLine     string
	playerInfoLine     string
	playerFailure      PlayerFailure
	playerBreakElapsed time.Duration
	playerVolume       int
	// playerWindowNotified is set once the first video of the session reached
	// playback; playerWindowNotice keeps the one-time notice on screen until
	// the next key press.
	playerWindowNotified bool
	playerWindowNotice   bool
	// playerKeysWarnedGen is the last generation warned about its mpv window
	// having no keys.
	playerKeysWarnedGen        uint64
	playerContainmentWarnedGen uint64
	chat                       Chat
	chatPanel                  chatPanel
	composing                  bool
	composeDraft               string
	// showList / showChat are the session layout flags (ticket 02). Both start true.
	// Ctrl+W cycles named modes; Ctrl+H / Ctrl+L toggle each flag independently.
	// Layout is not persisted to disk.
	videoHost     VideoHost
	hostPublished HostPanel
	chatOverlay   bool
	panelsSwapped bool
	// chatOverlayOpacity is the percent asked for; 0 means the default.
	chatOverlayOpacity int
	// hostedListCols and hostedChatCols are the widths asked for; 0 means automatic.
	hostedListCols int
	hostedChatCols int
	hostedPersist  HostedColumnsPersist
	hostedSaveGen  int
	showList       bool
	showChat       bool
	width          int
	height         int
	quitting       bool
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
		catalog:      catalog,
		tracker:      &requestTracker{},
		keys:         defaultKeyMap(),
		pageSize:     defaultCategoryPageSize,
		mode:         ModeCategories,
		focus:        FocusList,
		generation:   1,
		hydrateGen:   1,
		playerVolume: 100,
		chatPanel:    newChatPanel(),
		favorites:    make([]twitch.Channel, 0),
		recents:      make([]twitch.Channel, 0),
		showList:     true,
		showChat:     true,
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
	return m.authNotice
}

// CorruptNotice returns the active one-time state corruption notice in the status bar, if any.
func (m Model) CorruptNotice() string {
	return m.corruptNotice
}

// StatusNotice returns the active one-time status notice in the status bar, if any.
func (m Model) StatusNotice() string {
	return m.statusNotice
}

// CtrlCNotice returns the transient "use Ctrl+Q to quit" hint after Ctrl+C, if any.
func (m Model) CtrlCNotice() string {
	return m.ctrlCNotice
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	var cmds []tea.Cmd
	if m.source != nil {
		cmds = append(cmds, m.fetchCategoriesCmd(m.generation, "", false))
		if hydrate := m.fetchHydrateCmd(m.hydrateGen); hydrate != nil {
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
	if m.mode == "" {
		return ModeCategories
	}
	return m.mode
}

// Focus returns the currently effective focused panel (FocusList or FocusChat).
// When chat is hidden by width or layout, effective focus degrades to the
// remaining panel while preserving the underlying focus for when it returns.
func (m Model) Focus() Focus {
	if m.focus == "" {
		return FocusList
	}
	listOn := m.listVisible()
	chatOn := m.chatColumnVisible()
	switch {
	case listOn && chatOn:
		return m.focus
	case chatOn:
		return FocusChat
	default:
		return FocusList
	}
}

// HelpVisible reports whether the keyboard shortcuts help view is currently open.
func (m Model) HelpVisible() bool {
	return m.helpVisible
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
	return m.loading
}

// Err returns the current error state, if any.
func (m Model) Err() error {
	return m.err
}

// Generation returns the current request generation counter.
func (m Model) Generation() uint64 {
	return m.generation
}

// CancelActiveRequest cancels any in-flight Source request and advances the generation
// so late responses from the canceled screen are ignored.
func (m Model) CancelActiveRequest() Model {
	m.tracker.cancelActive()
	m.generation++
	m.loading = false
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
		return m, m.saveHostedColumns(msg.gen)
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
	m.statusNotice = notice
	m.statusNoticeWarns = true
}

func (m *Model) clearStatusNotice() {
	m.statusNotice = ""
	m.statusNoticeWarns = false
}

// clearVolumeLimitNotice drops a limit notice the player has outdated. A change
// made in the mpv window reaches no key, so it cannot wait for one.
func (m *Model) clearVolumeLimitNotice() {
	if m.statusNotice == m.t("player.vol_max") || m.statusNotice == m.t("player.vol_min") {
		m.clearStatusNotice()
	}
}

func (m Model) t(key string) string {
	if m.catalog == nil {
		return key
	}
	return m.catalog.T(key)
}
