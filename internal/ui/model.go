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

const defaultCategoryPageSize = 50

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
type SearchLoadedMsg struct {
	Generation uint64
	Query      string
	Page       twitch.Page[twitch.Channel]
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

// Model is the Bubble Tea model for ttv-tui.
type Model struct {
	catalog            *i18n.Catalog
	source             twitch.Source
	tracker            *requestTracker
	keys               keyMap
	pageSize           int
	mode               Mode
	focus              Focus
	helpVisible        bool
	generation         uint64
	hydrateGen         uint64
	loading            bool
	err                error
	categories         []twitch.Category
	channels           []twitch.Channel
	nextCursor         string
	hasMore            bool
	filter             string
	filtering          bool
	searching          bool
	searchInput        string
	searchQuery        string
	highlightedID      string
	highlightedLogin   string // secondary channel identity when upstream IDs churn between loads
	highlightIndex     int    // VisibleItems index; nearest-neighbor fallback after refresh
	focusResults       bool   // next channel/search load highlights its first result; see settleHighlightAfterLoad
	selectedCategory   *twitch.Category
	selectedChannel    *twitch.Channel
	tokenCheck         TokenCheckFunc
	invalidTokenInit   bool
	authNotice         string
	corruptNotice      string
	statusNotice       string
	store              Store
	favorites          []twitch.Channel
	recents            []twitch.Channel
	playback           Playback
	playerGen          uint64
	playerSwitching    bool
	playerState        PlayerState
	playerLogin        string
	playerLastLine     string
	playerFailure      PlayerFailure
	playerBreakElapsed time.Duration
	playerVolume       int
	// playerWindowNotified is set once the first video of the session reached
	// playback; playerWindowNotice keeps the one-time notice on screen until
	// the next key press.
	playerWindowNotified bool
	playerWindowNotice   bool
	chat                 Chat
	chatPanel            chatPanel
	composing            bool
	composeDraft         string
	width                int
	height               int
	quitting             bool
}

// New creates a new UI Model backed by the given i18n catalog and options.
func New(catalog *i18n.Catalog, opts ...Option) Model {
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
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&m)
		}
	}
	if m.invalidTokenInit {
		m.authNotice = m.t("auth.token_invalid_notice")
	}
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
// When the terminal is narrower than 80 columns, the chat panel is hidden and
// effective focus degrades to FocusList while preserving the underlying focus
// state when the terminal widens again.
func (m Model) Focus() Focus {
	if m.focus == "" {
		return FocusList
	}
	if m.chatHiddenByWidth() {
		return FocusList
	}
	if !m.hasChatPanel() {
		return FocusList
	}
	return m.focus
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
	switch msg := msg.(type) {
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

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
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
		// Prefer exact login then live among clones (twitch.PreferSearchChannels).
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
		m.recordRecent(selected)
		var cmds []tea.Cmd
		if m.store != nil {
			cmds = append(cmds, m.saveStoreCmd())
		}
		if m.playback != nil {
			m.playerLogin = selected.Login
			m.playerState = PlayerStateStarting
			m.playerLastLine = ""
			m.playerFailure = PlayerFailureNone
			m.playerBreakElapsed = 0
			m.playerVolume = 100
			m.playerSwitching = true
			cmds = append(cmds, m.playback.Start(selected.Login))
		}
		if m.chat != nil || m.chatPanel.channel != "" {
			if chatCmd := m.startChatChannel(selected.Login); chatCmd != nil {
				cmds = append(cmds, chatCmd)
			}
		}
		return m, tea.Batch(cmds...)

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
		m.playerState = msg.State
		if msg.Login != "" {
			m.playerLogin = msg.Login
		}
		if msg.LastLine != "" {
			m.playerLastLine = msg.LastLine
		} else if msg.State != PlayerStateError {
			m.playerLastLine = ""
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
		if msg.State == PlayerStatePlaying && !m.playerWindowNotified {
			// mpv opens its own window; someone new to the app may not notice it.
			m.playerWindowNotified = true
			m.playerWindowNotice = true
		}
		if msg.Volume > 0 || msg.State == PlayerStatePlaying || msg.State == PlayerStatePaused || msg.State == PlayerStateBreak {
			m.playerVolume = clampPlayerVolume(msg.Volume)
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	}
	return m, nil
}

func (m Model) t(key string) string {
	if m.catalog == nil {
		return key
	}
	return m.catalog.T(key)
}
