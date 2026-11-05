// Package player supervises streamlink→mpv playback: process groups, IPC sockets,
// observable states, and cleanup on every exit path.
package player

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// State is a playback lifecycle state. Values match the product vocabulary.
type State string

const (
	StateStarting State = "iniciando"
	StatePlaying  State = "tocando"
	StatePaused   State = "pausado"
	StateBreak    State = "intervalo"
	StateError    State = "erro"
	StateEnded    State = "encerrado"
)

// EndReason distinguishes how playback reached StateEnded.
type EndReason string

const (
	// EndReasonStopped is an intentional Stop() from the app.
	EndReasonStopped EndReason = "stopped"
	// EndReasonSocketEOF is EOF on the mpv IPC socket (reader closed).
	EndReasonSocketEOF EndReason = "socket_eof"
	// EndReasonStreamlinkExit is the supervised process exiting.
	EndReasonStreamlinkExit EndReason = "streamlink_exit"
	// EndReasonMPVDead is mpv death inferred while the parent process is still alive.
	EndReasonMPVDead EndReason = "mpv_dead"
)

// Event is an observable playback transition. Every event carries Generation so
// consumers can discard stale updates after a fast A→B→C channel switch.
type Event struct {
	Generation   uint64
	State        State
	Login        string
	LastLine     string
	BreakElapsed time.Duration
	Reason       EndReason
	// Volume is the last known mpv volume in 0..100 for this generation.
	Volume int
	// Failure classifies a StateError event; LastLine keeps the original line
	// as technical detail.
	Failure Failure
	// InputConfFailed is true on every event of a session whose mpv input.conf
	// could not be written: the mpv window ignores keys, the TUI still controls it.
	InputConfFailed bool
}

// CommandFunc builds the process to supervise for a given channel login and IPC socket path.
// Tests inject a helper that mimics streamlink+mpv without network or a real window.
type CommandFunc func(login, socketPath string) *exec.Cmd

// Options configures a Manager. Zero values select production defaults.
type Options struct {
	// Command builds the supervised process. Nil uses the default streamlink+mpv invocation.
	Command CommandFunc
	// CommandNoInputConf replaces Command for a session whose input.conf could
	// not be written. Nil with the default Command drops --input-conf; nil with
	// a custom Command keeps using Command.
	CommandNoInputConf CommandFunc
	// WriteInputConf creates the session's mpv input.conf at path. Nil uses the
	// production writer; a failure never stops playback.
	WriteInputConf func(path string) error
	// RuntimeDir is the parent for per-playback private directories.
	// Nil/empty uses $XDG_RUNTIME_DIR/ttv-tui, falling back to os.TempDir()/ttv-tui.
	RuntimeDir string
	// SocketWait is how long to wait for the mpv IPC socket file to appear after spawn.
	SocketWait time.Duration
	// StopWait is how long to wait after SIGTERM before escalating to SIGKILL.
	StopWait time.Duration
	// BreakMaxHold caps how long the interval state may last after streamlink's
	// hold-start line without the matching hold-end line. Past it the app stops
	// claiming an interval and returns to the state observed before. IPC silence
	// never starts an interval.
	BreakMaxHold time.Duration
	// EventsBuffer sizes the outbound event channel.
	EventsBuffer int
	// RequiredTools are executables checked with LookPath before each Start,
	// so a missing streamlink or mpv is reported without spawning anything.
	// Nil with the default Command checks streamlink and mpv; nil with a
	// custom Command checks nothing.
	RequiredTools []string
	// LookPath resolves RequiredTools. Nil uses exec.LookPath.
	LookPath func(file string) (string, error)
	// RegistryDir records each live player (owner pid, process group, socket) so
	// a later run can find a player orphaned by an abrupt exit. Empty disables it.
	RegistryDir string
}

const (
	defaultSocketWait   = 15 * time.Second
	defaultStopWait     = 3 * time.Second
	defaultBreakMaxHold = 5 * time.Minute
	defaultEventsBuffer = 32
)

// Manager owns at most one active Session at a time and assigns incremental generations.
type Manager struct {
	opts   Options
	events chan Event

	mu      sync.Mutex
	gen     uint64
	session *Session
	closed  bool
	// stopping counts sessions being torn down outside m.mu; stopped is
	// signalled (on m.mu) whenever one finishes.
	stopping int
	stopped  *sync.Cond
	// inputConfErr is the last input.conf write failure, for the exit log.
	inputConfErr error
}

// New creates a Manager. The Events channel stays open for the Manager lifetime;
// call Close after the final Stop when the application exits.
func New(opts Options) *Manager {
	if opts.SocketWait <= 0 {
		opts.SocketWait = defaultSocketWait
	}
	if opts.StopWait <= 0 {
		opts.StopWait = defaultStopWait
	}
	if opts.BreakMaxHold <= 0 {
		opts.BreakMaxHold = defaultBreakMaxHold
	}
	if opts.EventsBuffer <= 0 {
		opts.EventsBuffer = defaultEventsBuffer
	}
	if opts.Command == nil {
		opts.Command = DefaultCommand
		if opts.CommandNoInputConf == nil {
			opts.CommandNoInputConf = commandNoInputConf
		}
		if opts.RequiredTools == nil {
			opts.RequiredTools = []string{"streamlink", "mpv"}
		}
	}
	if opts.WriteInputConf == nil {
		opts.WriteInputConf = writeInputConf
	}
	if opts.LookPath == nil {
		opts.LookPath = exec.LookPath
	}
	if opts.RuntimeDir == "" {
		opts.RuntimeDir = defaultRuntimeDir()
	}
	m := &Manager{
		opts:   opts,
		events: make(chan Event, opts.EventsBuffer),
	}
	m.stopped = sync.NewCond(&m.mu)
	return m
}

// stopTracked stops sess; the caller already counted it in m.stopping.
func (m *Manager) stopTracked(sess *Session) {
	sess.Stop()
	m.mu.Lock()
	m.stopping--
	m.stopped.Broadcast()
	m.mu.Unlock()
}

// Events returns the channel of playback events. Receivers must discard events
// whose Generation is older than the generation they care about.
func (m *Manager) Events() <-chan Event {
	return m.events
}

// Generation returns the generation of the current (or last started) session.
func (m *Manager) Generation() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.gen
}

// Start stops any previous session and starts playback for login.
// It returns the new generation. The first event is StateStarting.
func (m *Manager) Start(login string) (uint64, error) {
	if login == "" {
		return 0, fmt.Errorf("player: empty channel login")
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return 0, fmt.Errorf("player: manager closed")
	}
	prev := m.session
	m.session = nil
	m.gen++
	gen := m.gen
	if prev != nil {
		m.stopping++
	}
	m.mu.Unlock()

	if prev != nil {
		m.stopTracked(prev)
	}

	if err := m.checkTools(); err != nil {
		m.emit(Event{
			Generation: gen,
			State:      StateError,
			Login:      login,
			LastLine:   err.Error(),
			Failure:    FailureOf(err),
		})
		return gen, err
	}

	sess, err := startSession(sessionConfig{
		Generation:         gen,
		Login:              login,
		RuntimeDir:         m.opts.RuntimeDir,
		Command:            m.opts.Command,
		CommandNoInputConf: m.opts.CommandNoInputConf,
		WriteInputConf:     m.opts.WriteInputConf,
		SocketWait:         m.opts.SocketWait,
		StopWait:           m.opts.StopWait,
		BreakMaxHold:       m.opts.BreakMaxHold,
		RegistryDir:        m.opts.RegistryDir,
		Emit:               m.emit,
	})
	if err != nil {
		m.emit(Event{
			Generation: gen,
			State:      StateError,
			Login:      login,
			LastLine:   err.Error(),
			Failure:    FailureOf(err),
		})
		return gen, err
	}

	m.mu.Lock()
	if sess.inputConfErr != nil {
		m.inputConfErr = sess.inputConfErr
	}
	// A newer Start may have won the race; drop this session if so.
	if m.gen != gen || m.closed {
		m.mu.Unlock()
		sess.Stop()
		return gen, fmt.Errorf("player: superseded before attach")
	}
	m.session = sess
	m.mu.Unlock()
	return gen, nil
}

// checkTools reports the first RequiredTools entry that LookPath cannot find.
func (m *Manager) checkTools() error {
	for _, tool := range m.opts.RequiredTools {
		if _, err := m.opts.LookPath(tool); err != nil {
			return &MissingToolError{Tool: tool, Err: err}
		}
	}
	return nil
}

// Stop ends the active session, if any, and returns only once every session
// being torn down (including one stopped concurrently, e.g. by the UI on Ctrl+Q
// or by a channel switch) has its process group gone. Exiting the app right
// after Stop therefore cannot leave a player behind.
func (m *Manager) Stop() {
	m.mu.Lock()
	sess := m.session
	m.session = nil
	if sess != nil {
		m.stopping++
	}
	m.mu.Unlock()
	if sess != nil {
		m.stopTracked(sess)
	}
	m.mu.Lock()
	for m.stopping > 0 {
		m.stopped.Wait()
	}
	m.mu.Unlock()
}

// TogglePause asks mpv to flip the pause property for the active session.
// The observable state changes only after the correlated IPC reply (or a
// property-change event from mpv, including pauses done in the mpv window).
func (m *Manager) TogglePause() error {
	m.mu.Lock()
	sess := m.session
	m.mu.Unlock()
	if sess == nil {
		return fmt.Errorf("player: no active session")
	}
	return sess.togglePause()
}

// AdjustVolume changes volume by delta (typically ±5), clamped to 0..100.
func (m *Manager) AdjustVolume(delta int) error {
	m.mu.Lock()
	sess := m.session
	m.mu.Unlock()
	if sess == nil {
		return fmt.Errorf("player: no active session")
	}
	return sess.adjustVolume(delta)
}

// SetVolume sets absolute volume clamped to 0..100.
func (m *Manager) SetVolume(v int) error {
	m.mu.Lock()
	sess := m.session
	m.mu.Unlock()
	if sess == nil {
		return fmt.Errorf("player: no active session")
	}
	return sess.setVolume(clampVolume(v))
}

// Volume returns the last known volume for the active session, or 0.
func (m *Manager) Volume() int {
	m.mu.Lock()
	sess := m.session
	m.mu.Unlock()
	if sess == nil {
		return 0
	}
	return sess.Volume()
}

// InputConfErr returns the last input.conf write failure, or nil. Callers read
// it after the TUI exits: a log line written while it owns the terminal would
// corrupt the screen.
func (m *Manager) InputConfErr() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.inputConfErr
}

// Close stops playback and closes the Events channel. After Close, Start fails.
func (m *Manager) Close() {
	m.Stop()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.closed = true
	close(m.events)
}

func (m *Manager) emit(ev Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	select {
	case m.events <- ev:
	default:
		// Prefer dropping the oldest buffered event over blocking supervision.
		select {
		case <-m.events:
		default:
		}
		select {
		case m.events <- ev:
		default:
		}
	}
}

// DefaultCommand builds: streamlink https://www.twitch.tv/<login> best --player mpv
// with mpv IPC/window/hwdec flags. --no-config ignores user configs but keeps the
// bindings compiled into mpv, so --no-input-default-bindings drops those and the
// session's input.conf provides the only keys. --volume-max=100 holds the window
// to the 0..100 range the TUI shows; mpv's default ceiling is 130.
func DefaultCommand(login, socketPath string) *exec.Cmd {
	return playerCommand(login, socketPath, inputConfPath(socketPath))
}

// commandNoInputConf is DefaultCommand for a session without an input.conf. The
// built-in bindings stay off: the window ignores keys and the TUI keeps working,
// which beats going back to bindings the user cannot see or undo.
func commandNoInputConf(login, socketPath string) *exec.Cmd {
	return playerCommand(login, socketPath, "")
}

// playerCommand omits --input-conf when inputConf is empty.
func playerCommand(login, socketPath, inputConf string) *exec.Cmd {
	playerArgs := fmt.Sprintf(
		"--no-config --no-input-default-bindings --volume-max=100 --input-ipc-server=%s --force-window=immediate --hwdec=auto-safe",
		socketPath,
	)
	if inputConf != "" {
		playerArgs += " --input-conf=" + inputConf
	}
	cmd := exec.Command(
		"streamlink",
		"--no-config",
		"--loglevel", "info",
		fmt.Sprintf("https://www.twitch.tv/%s", login),
		"best",
		"--player", "mpv",
		"--player-args", playerArgs,
	)
	return cmd
}

func defaultRuntimeDir() string {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "ttv-tui")
}
