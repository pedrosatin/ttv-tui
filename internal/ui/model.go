package ui

import (
	"context"
	"strings"
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

// WithInvalidTokenNotice initializes the Model with the one-time invalid token status bar notice.
func WithInvalidTokenNotice() Option {
	return func(m *Model) {
		m.invalidTokenInit = true
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

// WithStatusNotice initializes the Model with a one-time status-bar notice.
func WithStatusNotice(notice string) Option {
	return func(m *Model) {
		m.statusNotice = notice
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
	invalidTokenInit       bool
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
	pendingRecents  []twitch.Channel
	playback        Playback
	playerGen       uint64
	playerSwitching bool
	// playerEmbedded is the output of the current playback: the panel or, when
	// false, the mpv window. The layout picks it at Start and never after.
	playerEmbedded     bool
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
	// showList / showChat are the session layout flags (ticket 02). Both start
	// true (split) unless the video layout can draw (see openInVideoLayout).
	// Ctrl+W cycles named modes; Ctrl+H / Ctrl+L toggle each flag independently.
	// Layout is not persisted to disk.
	videoRequested bool
	videoReason    string
	// videoStateRows is how many rows the persistent video state line took from
	// the terminal height at the last resize. See videoStateLines.
	videoStateRows int
	videoSurface   VideoSurface
	videoHost      VideoHost
	hostPublished  HostPanel
	chatOverlay    bool
	// hostedListCols and hostedChatCols are the widths asked for; 0 means automatic.
	hostedListCols int
	hostedChatCols int
	hostedPersist  HostedColumnsPersist
	hostedSaveGen  int
	videoPublished VideoPanel
	showVideo      bool
	// videoListOpen is the list opened over the panel. It implies showVideo.
	videoListOpen bool
	// videoChatOff hides the chat strip of the video mode, so the panel takes
	// every row. It outlives leaving the mode, for the rest of the session.
	videoChatOff bool
	// videoChatRows is the height of the chat strip; zero means
	// defaultVideoChatRows.
	videoChatRows int
	// videoChatTop puts the chat strip above the panel instead of under it.
	videoChatTop bool
	showList     bool
	showChat     bool
	width        int
	height       int
	quitting     bool
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
	if m.invalidTokenInit {
		m.authNotice = m.t("auth.token_invalid_notice")
	}
	m.openInVideoLayout()
	return m
}

// NewWithSource creates a new UI Model wired to a twitch.Source.
func NewWithSource(catalog *i18n.Catalog, source twitch.Source) Model {
	return New(catalog, WithSource(source))
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
		return model.syncVideoPanel(m, cmd)
	}
	return next, cmd
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case hostedSaveMsg:
		return m, m.saveHostedColumns(msg.gen)
	case tea.BatchMsg:
		var cmds []tea.Cmd
		var cur tea.Model = m
		for _, cmd := range msg {
			if cmd == nil {
				continue
			}
			var nextCmd tea.Cmd
			cur, nextCmd = cur.Update(cmd())
			if nextCmd != nil {
				cmds = append(cmds, nextCmd)
			}
		}
		return cur, tea.Batch(cmds...)

	case AuthValidatedMsg:
		if msg.Err != nil {
			m.authNotice = m.t("auth.token_invalid_notice")
		}
		return m, nil

	case copyResultMsg:
		return m.applyCopyResult(msg)

	case copyNoticeClearMsg:
		return m.clearCopyNoticeIfExpired(msg.gen), nil

	case tea.WindowSizeMsg:
		m.applyWindowSize(msg.Width, msg.Height)
		return m, nil

	case CategoriesLoadedMsg:
		if msg.Generation != m.generation {
			return m, nil
		}
		m.loading = false
		if msg.Err != nil {
			m.err = msg.Err
			return m, nil
		}
		m.err = nil
		if msg.Append {
			existing := make(map[string]struct{}, len(m.categories))
			for _, c := range m.categories {
				existing[c.ID] = struct{}{}
			}
			for _, c := range msg.Page.Items {
				if _, dup := existing[c.ID]; !dup {
					m.categories = append(m.categories, c)
					existing[c.ID] = struct{}{}
				}
			}
		} else {
			m.categories = make([]twitch.Category, len(msg.Page.Items))
			copy(m.categories, msg.Page.Items)
		}
		m.nextCursor = msg.Page.Cursor
		m.hasMore = msg.Page.HasMore
		m.reconcileHighlight()
		return m, nil

	case ChannelsLoadedMsg:
		if msg.Generation != m.generation {
			return m, nil
		}
		m.loading = false
		if msg.Err != nil {
			m.err = msg.Err
			return m, nil
		}
		m.err = nil
		if msg.Append {
			existing := make(map[string]struct{}, len(m.channels))
			for _, c := range m.channels {
				existing[c.ID] = struct{}{}
			}
			for _, c := range msg.Page.Items {
				if _, dup := existing[c.ID]; !dup {
					m.channels = append(m.channels, c)
					existing[c.ID] = struct{}{}
				}
			}
		} else {
			m.channels = make([]twitch.Channel, len(msg.Page.Items))
			copy(m.channels, msg.Page.Items)
		}
		m.refreshSavedAvailability(msg.Page.Items)
		m.nextCursor = msg.Page.Cursor
		m.hasMore = msg.Page.HasMore
		m.settleHighlightAfterLoad()
		return m, nil

	case SearchLoadedMsg:
		if msg.Generation != m.generation {
			return m, nil
		}
		m.loading = false
		if msg.Err != nil {
			m.err = msg.Err
			return m, nil
		}
		m.err = nil
		// Prefer exact name then viewers (twitch.PreferSearchCategories).
		cats := twitch.PreferSearchCategories(msg.Query, msg.Categories)
		m.searchCategories = make([]twitch.Category, len(cats))
		copy(m.searchCategories, cats)
		// Prefer exact login, exact display name, then live among clones
		// (twitch.PreferSearchChannels).
		// Lives here so every Source gets the same Enter/highlight target.
		items := twitch.PreferSearchChannels(msg.Query, msg.Page.Items)
		m.channels = make([]twitch.Channel, len(items))
		copy(m.channels, items)
		m.refreshSavedAvailability(items)
		m.nextCursor = msg.Page.Cursor
		m.hasMore = msg.Page.HasMore
		m.settleHighlightAfterLoad()
		return m, nil

	case SavedAvailabilityMsg:
		if msg.Generation != m.hydrateGen {
			return m, nil
		}
		if msg.Err != nil {
			// Keep [unverified]; never surface a hard error for this soft check.
			return m, nil
		}
		m.refreshSavedAvailability(msg.Channels)
		return m, nil

	case ChannelSelectedMsg:
		selected := msg.Channel
		m.selectedChannel = &selected
		m.clearStaleStatusErrors()
		m.queueRecent(selected)
		var cmds []tea.Cmd
		if m.store != nil {
			cmds = append(cmds, m.saveStoreCmd())
		}
		m.closeVideoList()
		if m.playback != nil {
			// The player keeps the volume of a channel it restarts.
			if selected.Login != m.playerLogin {
				m.playerVolume = 100
			}
			m.playerLogin = selected.Login
			m.playerState = PlayerStateStarting
			m.playerLastLine = ""
			m.playerFailure = PlayerFailureNone
			m.playerBreakElapsed = 0
			m.playerSwitching = true
			m.playerEmbedded = m.embeddedLayout()
			cmds = append(cmds, m.playback.Start(selected.Login, m.playerEmbedded))
		}
		if m.chat != nil || m.chatPanel.channel != "" {
			if chatCmd := m.startChatChannel(selected.Login); chatCmd != nil {
				cmds = append(cmds, chatCmd)
			}
		}
		return m, tea.Batch(cmds...)

	case storeSavedMsg:
		if msg.Err != nil {
			m.warnStatus(m.t("store.save_failed"))
		} else if msg.Notice != "" {
			m.clearStatusNotice()
			m.statusNotice = m.t(msg.Notice)
		}
		return m, nil

	case ChatEventMsg:
		return m.handleChatEvent(msg)

	case PlayerStatusMsg:
		if m.playerSwitching {
			if !msg.FromStart {
				return m, nil
			}
			m.playerSwitching = false
			m.playerGen = msg.Generation
		} else if msg.Generation != 0 && m.playerGen != 0 && msg.Generation < m.playerGen {
			// Stale generation from a previous channel — discard.
			return m, nil
		} else if msg.Generation > m.playerGen {
			m.playerGen = msg.Generation
		}
		stateBefore, volumeBefore := m.playerState, m.playerVolume
		if msg.State != m.playerState {
			// A pause in the mpv window never reaches the TUI as a key, so the
			// notice cannot wait for one: the bar needs the room for the state.
			m.playerWindowNotice = false
		}
		m.playerState = msg.State
		if msg.Login != "" {
			m.playerLogin = msg.Login
		}
		if msg.State == PlayerStateError {
			m.playerLastLine = msg.LastLine
			m.playerInfoLine = ""
		} else {
			m.playerLastLine = ""
			m.playerInfoLine = ""
			if strings.Contains(strings.ToLower(msg.LastLine), "[info]") {
				m.playerInfoLine = sanitizeScreenText(strings.TrimSpace(msg.LastLine))
			}
		}
		if msg.State == PlayerStateError {
			m.playerFailure = msg.Failure
			// A classified offline failure is fresh evidence for this channel:
			// the listing may still show stale viewers from before play.
			if msg.Failure == PlayerFailureOffline {
				m.markChannelOffline(m.playerLogin)
			}
		} else {
			m.playerFailure = PlayerFailureNone
		}
		m.playerBreakElapsed = msg.BreakElapsed
		if msg.ContainmentDegraded && msg.Generation != m.playerContainmentWarnedGen {
			m.playerContainmentWarnedGen = msg.Generation
			m.warnStatus(m.t("player.warn.containment"))
		}
		if msg.InputConfFailed && msg.Generation != m.playerKeysWarnedGen {
			// Every event of that playback carries the flag; warn once.
			m.playerKeysWarnedGen = msg.Generation
			m.warnStatus(m.t("player.warn.input_conf"))
		}
		if msg.State == PlayerStatePlaying && !m.playerWindowNotified && !m.playerEmbedded && m.videoHost == nil {
			// mpv opens its own window; someone new to the app may not notice it.
			m.playerWindowNotified = true
			m.playerWindowNotice = true
		}
		if msg.Volume > 0 || msg.State == PlayerStatePlaying || msg.State == PlayerStatePaused || msg.State == PlayerStateBreak {
			m.playerVolume = clampPlayerVolume(msg.Volume)
		}
		if m.playerState != stateBefore || m.playerVolume != volumeBefore {
			m.clearVolumeLimitNotice()
		}
		return m, nil

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
