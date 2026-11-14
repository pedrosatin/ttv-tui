// Package player supervises streamlink→mpv playback: process groups, IPC sockets,
// observable states, and cleanup on every exit path.
package player

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/config"
	"github.com/pedrosatin/ttv-tui/internal/pathlookup"
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

// baselineVolume is where a channel starts: mpv can open above the 0..100 range the
// app shows.
const baselineVolume = 100

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
	Target       Target
	Login        string
	LastLine     string
	BreakElapsed time.Duration
	Reason       EndReason
	// Volume is the last known mpv volume in 0..100 for this generation.
	Volume int
	// Muted is mpv's mute as last known for this generation.
	Muted bool
	// Failure classifies a StateError event; LastLine keeps the original line
	// as technical detail.
	Failure Failure
	// InputConfFailed is true on every event of a session whose mpv input.conf
	// could not be written: the mpv window ignores keys, the TUI still controls it.
	InputConfFailed bool
	// ContainmentDegraded means Windows refused Job Object containment.
	ContainmentDegraded bool
}

// Target describes the stream to play: the ready stream URL, optional streamlink
// CLI arguments, optional streamlink plugins to sideload, and an optional
// human-readable label/identity for the session (used in logs, process
// registration, and orphan recovery).
type Target struct {
	URL       string
	ExtraArgs []string
	// Plugins go to a private directory of the session and reach streamlink
	// through --plugin-dir. Only the default command passes them.
	Plugins []Plugin
	Label   string
}

// Identity returns the session identity: Label when non-empty, otherwise URL.
func (t Target) Identity() string {
	if t.Label != "" {
		return t.Label
	}
	return t.URL
}

// TargetFromURL builds a Target with the given URL and optional extra streamlink arguments.
func TargetFromURL(rawURL string, extra ...string) Target {
	return Target{
		URL:       rawURL,
		ExtraArgs: slices.Clone(extra),
	}
}

// checkStreamURL accepts only an absolute http(s) URL with a host and no
// whitespace or control characters. The URL is a positional argument of
// streamlink, so anything else (a leading "-" above all) could reach it as an
// option.
func checkStreamURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("player: empty stream URL")
	}
	if strings.IndexFunc(rawURL, func(r rune) bool { return r <= ' ' || r == 0x7f }) >= 0 {
		return fmt.Errorf("player: stream URL has whitespace or control characters")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("player: invalid stream URL: %w", err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("player: stream URL must be an http(s) URL with a host")
	}
	return nil
}

// CommandFunc builds the process to supervise for a given Target and IPC
// endpoint (a socket path on Unix, a pipe name on Windows). The endpoint is not
// a place to find the session's other files. Tests inject a helper that mimics
// streamlink+mpv without network or a real window.
type CommandFunc func(target Target, endpoint string) *exec.Cmd

// sessionCommand is a CommandFunc that also sees the session's directory. The
// default command needs it for --input-conf, which an endpoint cannot give.
type sessionCommand func(target Target, layout sessionLayout) *exec.Cmd

// sessionCommandOf adapts a CommandFunc, which only needs the endpoint.
func sessionCommandOf(f CommandFunc) sessionCommand {
	if f == nil {
		return nil
	}
	return func(target Target, layout sessionLayout) *exec.Cmd {
		return f(target, layout.Endpoint)
	}
}

// streamlinkInvocation is the production command. bin and mpv are absolute
// paths resolved before the session starts, and the process runs in the
// session's private directory: on Windows, CreateProcess and the DLL loader
// look in the working directory first, so nothing of the player chain may be
// looked up by name or run from where the user started the app.
type streamlinkInvocation struct {
	bin string
	mpv string
	env []string
}

// command builds: streamlink [plugin-dir] [extra...] <url> best --player <mpv>
// with mpv IPC/window/hwdec flags, input.conf in the session directory and IPC
// on the session endpoint. --no-config ignores user configs but keeps the
// bindings compiled into mpv, so --no-input-default-bindings drops those and the
// session's input.conf provides the only keys. --volume-max=100 holds the window
// to the 0..100 range the TUI shows; mpv's default ceiling is 130. --no-osc drops
// the on-screen controller: it answers clicks outside our bindings and speaks English.
func (s streamlinkInvocation) command(target Target, layout sessionLayout) *exec.Cmd {
	return s.build(target, layout, inputConfPath(layout.Dir))
}

// commandNoInputConf is command for a session without an input.conf. The
// built-in bindings stay off: the window ignores keys and the TUI keeps working,
// which beats going back to bindings the user cannot see or undo.
func (s streamlinkInvocation) commandNoInputConf(target Target, layout sessionLayout) *exec.Cmd {
	return s.build(target, layout, "")
}

// build omits --input-conf when inputConf is empty. The endpoint goes to
// --input-ipc-server; it is not a path inside the directory (it can be a pipe name).
func (s streamlinkInvocation) build(target Target, layout sessionLayout, inputConf string) *exec.Cmd {
	args := append([]string{"--no-config", "--loglevel", "info"}, sessionExtraArgs(target, layout)...)
	args = append(args,
		target.URL,
		"best",
		"--player", s.mpv,
		"--player-args", buildPlayerArgs(mpvArgs(layout.Endpoint, inputConf)),
	)
	cmd := exec.Command(s.bin, args...)
	cmd.Dir = layout.Dir
	if len(s.env) > 0 {
		cmd.Env = append(os.Environ(), s.env...)
	}
	return cmd
}

// sessionExtraArgs is what goes between streamlink's own flags and the URL: the
// session's plugin directory, when it has one, then the target's arguments.
func sessionExtraArgs(target Target, layout sessionLayout) []string {
	return append(pluginDirArgs(layout.PluginDir), target.ExtraArgs...)
}

// Options configures a Manager. Zero values select production defaults.
type Options struct {
	// Command builds the supervised process. Nil uses the default streamlink+mpv invocation.
	// It receives the IPC endpoint only; the default invocation also gets the
	// session directory.
	Command CommandFunc
	// CommandNoInputConf replaces Command for a session whose input.conf could
	// not be written. Nil with the default Command drops --input-conf; nil with
	// a custom Command keeps using Command.
	CommandNoInputConf CommandFunc
	// WriteInputConf creates the session's mpv input.conf at path. Nil uses the
	// production writer; a failure never stops playback.
	WriteInputConf func(path string) error
	// RuntimeDir is the parent for per-playback private directories.
	// Empty uses config.RuntimeDir: $XDG_RUNTIME_DIR/ttv-tui, else the OS convention
	// (see there), ending in os.TempDir()/ttv-tui.
	RuntimeDir string
	// SocketWait is how long to wait, after spawn, for the mpv IPC endpoint to
	// accept a connection.
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
	// LookPath resolves RequiredTools and, for the default Command, the
	// streamlink and mpv it runs; those must come back absolute. Nil searches
	// only the absolute directories of PATH.
	LookPath func(file string) (string, error)
	// RegistryDir records each live player (owner pid, process group, socket) so
	// a later run can find a player orphaned by an abrupt exit. Empty disables it.
	RegistryDir string
	// StreamlinkPath is an explicit path to the streamlink binary.
	// Empty means "streamlink" on PATH.
	StreamlinkPath string
	// StreamlinkEnv adds environment variables to streamlink execution.
	StreamlinkEnv []string
}

const (
	// defaultSocketWait covers everything streamlink does before mpv exists,
	// over the user's network: 15.6s measured behind a VPN at 380ms RTT, which
	// the previous 15s turned into a timeout on every attempt. The cost of the
	// margin falls only on a start that hangs without a word: it takes this long
	// to be reported. Nothing else waits for it: a player that dies ends the wait
	// at once, a fatal line fails the session at once, and an endpoint that
	// exists and refuses fails after ipcRefusedGrace.
	defaultSocketWait   = 30 * time.Second
	defaultStopWait     = 3 * time.Second
	defaultBreakMaxHold = 5 * time.Minute
	defaultEventsBuffer = 32
)

// Manager owns at most one active Session at a time and assigns incremental generations.
type Manager struct {
	opts   Options
	events chan Event
	// defaultCommand is true when Options.Command was nil: the session then
	// builds the default invocation from its directory and endpoint.
	defaultCommand bool

	mu      sync.Mutex
	gen     uint64
	session *Session
	// survivor blocks spawning until a group that outlived Stop disappears.
	survivor *Session
	// closing refuses new Starts from the moment Close begins; closed means the
	// Events channel is closed.
	closing bool
	closed  bool
	// stopping counts sessions being torn down outside m.mu, and starting the
	// Starts past their last check, spawning or attaching. stopped is signalled
	// (on m.mu) whenever either drops.
	stopping int
	starting int
	stopped  *sync.Cond
	// stopGen is the generation the last Stop left; a Start older than it was
	// overtaken by that Stop.
	stopGen uint64
	// order is the highest order a StartInOrder has begun with.
	order uint64
	// inputConfErr is the last input.conf write failure, for the exit log.
	inputConfErr error
	// notes are the problems sessions recorded, for the exit log.
	notes []string
	// keptIdentity and keptVolume are the channel/stream identity of the last session to end and the
	// volume it ended on. They outlive the session: a retry may follow a start that
	// failed and left none.
	keptIdentity string
	keptVolume   int
	// keptMuted is the mute the last session ended with. Unlike the volume it
	// follows the viewer to any channel, as on the Twitch site.
	keptMuted bool

	// afterPrevStop, when set by a test, runs in Start once the previous
	// session is down and before the new one is spawned.
	afterPrevStop func()
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
	defaultCommand := opts.Command == nil
	if defaultCommand && opts.RequiredTools == nil {
		opts.RequiredTools = []string{"streamlink", "mpv"}
	}
	if opts.WriteInputConf == nil {
		opts.WriteInputConf = writeInputConf
	}
	if opts.LookPath == nil {
		opts.LookPath = pathlookup.Abs
	}
	if opts.RuntimeDir == "" {
		opts.RuntimeDir = defaultRuntimeDir()
	}
	m := &Manager{
		opts:           opts,
		events:         make(chan Event, opts.EventsBuffer),
		defaultCommand: defaultCommand,
	}
	m.stopped = sync.NewCond(&m.mu)
	return m
}

// stopTracked stops sess; the caller already counted it in m.stopping.
func (m *Manager) stopTracked(sess *Session) error {
	err := m.stopSession(sess)
	m.mu.Lock()
	m.stopping--
	m.stopped.Broadcast()
	m.mu.Unlock()
	return err
}

// stopSession retains a surviving group even when the session cannot attach.
func (m *Manager) stopSession(sess *Session) error {
	sess.Stop()
	sess.mu.Lock()
	survived := sess.state == StateError && sess.lastLine == sess.survivorLine()
	sess.mu.Unlock()
	if !survived {
		return nil
	}
	m.mu.Lock()
	m.survivor = sess
	m.mu.Unlock()
	return fmt.Errorf("%s", sess.survivorLine())
}

// survivorError checks existence without signalling the group. The caller holds m.mu.
func (m *Manager) survivorError() error {
	if m.survivor == nil {
		return nil
	}
	if !m.survivor.group.alive() {
		m.survivor = nil
		return nil
	}
	return fmt.Errorf("%s", m.survivor.survivorLine())
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

// Start stops any previous session and starts playback for target in the mpv
// window. It returns the new generation. The first event is StateStarting.
//
// The new player is spawned only once no other session is being torn down and
// no other Start is spawning, so two players never run at once. A Stop, Close
// or newer Start that comes first cancels it.
func (m *Manager) Start(target Target) (uint64, error) {
	return m.start(target, 0)
}

// StartInOrder is Start for a caller whose requests may reach the Manager out
// of order. order grows with each request; a Start that comes after one with a
// higher order is superseded at once and spawns nothing.
func (m *Manager) StartInOrder(target Target, order uint64) (uint64, error) {
	return m.start(target, order)
}

// start is Start, ordered when order is not 0.
func (m *Manager) start(target Target, order uint64) (uint64, error) {
	if err := checkStreamURL(target.URL); err != nil {
		return 0, err
	}

	m.mu.Lock()
	if m.closing {
		m.mu.Unlock()
		return 0, fmt.Errorf("player: manager closed")
	}
	if order != 0 {
		if order < m.order {
			m.mu.Unlock()
			return 0, fmt.Errorf("player: superseded before attach")
		}
		m.order = order
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
		_ = m.stopTracked(prev)
		m.keepVolume(prev)
	}
	if m.afterPrevStop != nil {
		m.afterPrevStop()
	}

	m.mu.Lock()
	for m.stopping > 0 || m.starting > 0 {
		m.stopped.Wait()
	}
	if err := m.overtaken(gen); err != nil {
		m.mu.Unlock()
		return gen, err
	}
	if err := m.survivorError(); err != nil {
		m.mu.Unlock()
		return gen, err
	}
	m.starting++
	m.mu.Unlock()
	defer m.doneStarting()

	m.mu.Lock()
	streamlinkPath := m.opts.StreamlinkPath
	streamlinkEnv := append([]string(nil), m.opts.StreamlinkEnv...)
	m.mu.Unlock()

	command := sessionCommandOf(m.opts.Command)
	noInputConf := sessionCommandOf(m.opts.CommandNoInputConf)
	found, err := m.checkTools(streamlinkPath)
	if err == nil && m.defaultCommand {
		var inv streamlinkInvocation
		inv, err = m.invocation(found, streamlinkPath, streamlinkEnv)
		command, noInputConf = inv.command, inv.commandNoInputConf
	}
	if err != nil {
		m.emit(Event{
			Generation: gen,
			State:      StateError,
			Target:     target,
			Login:      target.Identity(),
			LastLine:   err.Error(),
			Failure:    FailureOf(err),
		})
		return gen, err
	}
	sess, err := startSession(sessionConfig{
		Generation:         gen,
		Target:             target,
		RuntimeDir:         m.opts.RuntimeDir,
		Command:            command,
		CommandNoInputConf: noInputConf,
		Volume:             m.startVolume(target),
		Muted:              m.startMuted(),
		WriteInputConf:     m.opts.WriteInputConf,
		SocketWait:         m.opts.SocketWait,
		StopWait:           m.opts.StopWait,
		BreakMaxHold:       m.opts.BreakMaxHold,
		RegistryDir:        m.opts.RegistryDir,
		Emit:               m.emit,
		Note:               m.note,
	})
	if err != nil {
		m.emit(Event{
			Generation: gen,
			State:      StateError,
			Target:     target,
			Login:      target.Identity(),
			LastLine:   err.Error(),
			Failure:    FailureOf(err),
		})
		return gen, err
	}

	m.mu.Lock()
	if sess.inputConfErr != nil {
		m.inputConfErr = sess.inputConfErr
	}
	// A Stop, Close or newer Start may have come while spawning; drop this
	// session if so. Stop and Close wait for it through m.starting.
	if err := m.overtaken(gen); err != nil {
		m.mu.Unlock()
		_ = m.stopSession(sess)
		return gen, err
	}
	m.session = sess
	m.mu.Unlock()
	return gen, nil
}

// overtaken reports why the Start of gen must not attach a session, if it must
// not. The caller holds m.mu.
func (m *Manager) overtaken(gen uint64) error {
	switch {
	case m.closing:
		return fmt.Errorf("player: manager closed")
	case m.stopGen >= gen:
		return fmt.Errorf("player: stopped before attach")
	case m.gen != gen:
		return fmt.Errorf("player: superseded before attach")
	}
	return nil
}

func (m *Manager) doneStarting() {
	m.mu.Lock()
	m.starting--
	m.stopped.Broadcast()
	m.mu.Unlock()
}

// keepVolume records where sess left the volume, once it can no longer change.
func (m *Manager) keepVolume(sess *Session) {
	volume, muted := sess.Volume(), sess.Muted()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.keptIdentity, m.keptVolume, m.keptMuted = sess.cfg.Target.Identity(), volume, muted
}

// startMuted is the mute a new session opens with: where the last one left it.
func (m *Manager) startMuted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.keptMuted
}

// startVolume is the volume a new session of target opens at: where the same
// channel/identity left it, so a retry changes nothing the user set, or the baseline for
// any other channel.
func (m *Manager) startVolume(target Target) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if target.Identity() == m.keptIdentity {
		return m.keptVolume
	}
	return baselineVolume
}

// checkTools reports the first RequiredTools entry that LookPath cannot find,
// or else where each one was found. A configured streamlinkPath replaces the
// lookup of streamlink.
func (m *Manager) checkTools(streamlinkPath string) (map[string]string, error) {
	found := make(map[string]string, len(m.opts.RequiredTools))
	for _, tool := range m.opts.RequiredTools {
		if tool == "streamlink" && streamlinkPath != "" {
			if fi, err := os.Stat(streamlinkPath); err != nil || fi.IsDir() {
				return nil, &MissingToolError{Tool: streamlinkPath, Err: err}
			}
			found[tool] = streamlinkPath
			continue
		}
		path, err := m.opts.LookPath(tool)
		if err != nil {
			return nil, &MissingToolError{Tool: tool, Err: err}
		}
		found[tool] = path
	}
	return found, nil
}

// invocation resolves the executables of the default command to absolute
// paths, reusing what checkTools found. A configured streamlinkPath that is
// relative keeps meaning what it always did, relative to where the app
// started: the session runs elsewhere. A lookup that comes back relative is
// refused, since it would name the directory the user started the app from.
func (m *Manager) invocation(found map[string]string, streamlinkPath string, env []string) (streamlinkInvocation, error) {
	resolve := func(tool string) (string, error) {
		path, ok := found[tool]
		if !ok {
			var err error
			if path, err = m.opts.LookPath(tool); err != nil {
				return "", &MissingToolError{Tool: tool, Err: err}
			}
		}
		if !filepath.IsAbs(path) {
			return "", &MissingToolError{Tool: tool, Err: fmt.Errorf("found at relative path %q", path)}
		}
		return path, nil
	}
	inv := streamlinkInvocation{env: env}
	if streamlinkPath != "" {
		abs, err := filepath.Abs(streamlinkPath)
		if err != nil {
			return inv, &MissingToolError{Tool: streamlinkPath, Err: err}
		}
		inv.bin = abs
	} else {
		bin, err := resolve("streamlink")
		if err != nil {
			return inv, err
		}
		inv.bin = bin
	}
	mpv, err := resolve("mpv")
	if err != nil {
		return inv, err
	}
	inv.mpv = mpv
	return inv, nil
}

// SetStreamlink updates the executable path and extra environment used for streamlink.
func (m *Manager) SetStreamlink(path string, env ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.opts.StreamlinkPath = path
	m.opts.StreamlinkEnv = append([]string(nil), env...)
}

// UsesStreamlink reports whether sessions run the default streamlink command.
// With a custom Command (tests, other players) no streamlink executable is
// needed, so callers must not look for one or download it.
func (m *Manager) UsesStreamlink() bool {
	return m.defaultCommand
}

// StreamlinkPath returns the configured streamlink path.
func (m *Manager) StreamlinkPath() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.opts.StreamlinkPath
}

// Stop ends the active session, if any, and cancels a Start in progress. It
// returns only once every session being torn down (including one stopped
// concurrently, e.g. by the UI on Ctrl+Q or by a channel switch) has its process
// group gone and no Start is spawning. Exiting the app right after Stop
// therefore cannot leave a player behind.
func (m *Manager) Stop() {
	m.mu.Lock()
	_ = m.stopLocked()
}

// StopGeneration is Stop for a caller that read generation gen. It does nothing
// once a newer Start or Stop has taken over: a Stop asked for before a play
// must not take that play down. It returns the group termination error, if any.
func (m *Manager) StopGeneration(gen uint64) error {
	m.mu.Lock()
	if gen < m.gen {
		m.mu.Unlock()
		return nil
	}
	return m.stopLocked()
}

// stopLocked is Stop for a caller that holds m.mu. It releases m.mu.
func (m *Manager) stopLocked() error {
	m.gen++
	m.stopGen = m.gen
	sess := m.session
	m.session = nil
	if sess != nil {
		m.stopping++
	}
	m.mu.Unlock()
	var err error
	if sess != nil {
		err = m.stopTracked(sess)
		m.keepVolume(sess)
	}
	m.mu.Lock()
	for m.stopping > 0 || m.starting > 0 {
		m.stopped.Wait()
	}
	if err == nil {
		err = m.survivorError()
	}
	m.mu.Unlock()
	return err
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

// ToggleMute flips the mute of the active session.
func (m *Manager) ToggleMute() error {
	m.mu.Lock()
	sess := m.session
	m.mu.Unlock()
	if sess == nil {
		return fmt.Errorf("player: no active session")
	}
	return sess.toggleMute()
}

// Muted returns the mute of the active session, or false without one.
func (m *Manager) Muted() bool {
	m.mu.Lock()
	sess := m.session
	m.mu.Unlock()
	if sess == nil {
		return false
	}
	return sess.Muted()
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

// Notes returns the problems sessions recorded, oldest first. Callers read them
// after the TUI exits, like InputConfErr.
func (m *Manager) Notes() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.notes...)
}

// note keeps msg for Notes, once.
func (m *Manager) note(msg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !slices.Contains(m.notes, msg) {
		m.notes = append(m.notes, msg)
	}
}

// Close stops playback and closes the Events channel. After Close, Start fails.
func (m *Manager) Close() {
	m.mu.Lock()
	m.closing = true
	m.mu.Unlock()
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

func mpvArgs(endpoint, inputConf string) []string {
	args := []string{
		"--no-config",
		"--no-input-default-bindings",
		"--no-osc",
		"--osd-level=1",
		"--osd-bar=no",
		"--volume-max=100",
		"--input-ipc-server=" + endpoint,
		"--force-window=immediate",
		"--hwdec=auto-safe",
	}
	if inputConf != "" {
		args = append(args, "--input-conf="+inputConf)
	}
	return args
}

// buildPlayerArgs joins an mpv argv into the single string --player-args takes.
// streamlink runs shlex.split on that string, so each argument is quoted to come
// back out identical. Callers pass an argv, never a hand-interpolated string.
func buildPlayerArgs(argv []string) string {
	quoted := make([]string, len(argv))
	for i, arg := range argv {
		quoted[i] = quotePlayerArg(arg)
	}
	return strings.Join(quoted, " ")
}

// quotePlayerArg quotes one argument for Python's shlex.split (POSIX mode).
// Arguments made only of characters shlex.quote treats as safe pass through
// unchanged, so the common case stays readable. Anything else goes inside single
// quotes, where backslashes are literal (Windows paths, \\.\pipe\name) and an
// embedded ' is written as '\” (close quote, escaped quote, reopen).
func quotePlayerArg(arg string) string {
	if arg == "" {
		return "''"
	}
	safe := true
	for i := 0; i < len(arg); i++ {
		if !isShlexSafe(arg[i]) {
			safe = false
			break
		}
	}
	if safe {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}

// isShlexSafe matches the set shlex.quote leaves unquoted: [A-Za-z0-9_@%+=:,./-].
func isShlexSafe(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	return strings.IndexByte("@%+=:,./-_", c) >= 0
}

func defaultRuntimeDir() string {
	return config.RuntimeDir()
}
