// Package player supervises streamlink→mpv playback: process groups, IPC sockets,
// observable states, and cleanup on every exit path.
package player

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/config"
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
	// ContainmentDegraded means Windows refused Job Object containment.
	ContainmentDegraded bool
}

// CommandFunc builds the process to supervise for a given channel login and IPC
// endpoint (a socket path on Unix, a pipe name on Windows). The endpoint is not
// a place to find the session's other files. Tests inject a helper that mimics
// streamlink+mpv without network or a real window.
type CommandFunc func(login, endpoint string) *exec.Cmd

// sessionCommand is a CommandFunc that also sees the session's directory. The
// default command needs it for --input-conf, which an endpoint cannot give.
type sessionCommand func(login string, layout sessionLayout) *exec.Cmd

// sessionCommandOf adapts a CommandFunc, which only needs the endpoint.
func sessionCommandOf(f CommandFunc) sessionCommand {
	if f == nil {
		return nil
	}
	return func(login string, layout sessionLayout) *exec.Cmd {
		return f(login, layout.Endpoint)
	}
}

// defaultSessionCommand is the production command: input.conf in the session
// directory, IPC on the session endpoint.
func defaultSessionCommand(login string, layout sessionLayout) *exec.Cmd {
	return DefaultCommand(login, layout.Endpoint, inputConfPath(layout.Dir))
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
	// LookPath resolves RequiredTools. Nil uses exec.LookPath.
	LookPath func(file string) (string, error)
	// RegistryDir records each live player (owner pid, process group, socket) so
	// a later run can find a player orphaned by an abrupt exit. Empty disables it.
	RegistryDir string
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
	closed  bool
	// stopping counts sessions being torn down outside m.mu; stopped is
	// signalled (on m.mu) whenever one finishes.
	stopping int
	stopped  *sync.Cond
	// inputConfErr is the last input.conf write failure, for the exit log.
	inputConfErr error
	// video is what StartEmbedded draws through.
	video *SixelOutput
	// keptLogin and keptVolume are the channel of the last session to end and the
	// volume it ended on. They outlive the session: a retry may follow a start that
	// failed and left none.
	keptLogin  string
	keptVolume int
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
	if defaultCommand {
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
		opts:           opts,
		events:         make(chan Event, opts.EventsBuffer),
		defaultCommand: defaultCommand,
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

// Start stops any previous session and starts playback for login in the mpv
// window. It returns the new generation. The first event is StateStarting.
func (m *Manager) Start(login string) (uint64, error) {
	return m.start(login, false)
}

// StartEmbedded is Start with the video drawn through the output given to
// SetVideoOutput. Without one it falls back to the window.
func (m *Manager) StartEmbedded(login string) (uint64, error) {
	return m.start(login, true)
}

func (m *Manager) start(login string, embedded bool) (uint64, error) {
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
		m.keepVolume(prev)
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

	var video *SixelOutput
	var videoWidth, videoHeight int
	if embedded {
		m.mu.Lock()
		video = m.video
		m.mu.Unlock()
	}
	if video != nil {
		videoWidth, videoHeight = video.FrameSize()
	}

	command := sessionCommandOf(m.opts.Command)
	noInputConf := sessionCommandOf(m.opts.CommandNoInputConf)
	if m.defaultCommand {
		command = defaultSessionCommand
		if video != nil {
			command = sixelSessionCommand(videoWidth, videoHeight, true)
			noInputConf = sixelSessionCommand(videoWidth, videoHeight, false)
		}
	}
	sess, err := startSession(sessionConfig{
		Generation:         gen,
		Login:              login,
		RuntimeDir:         m.opts.RuntimeDir,
		Command:            command,
		CommandNoInputConf: noInputConf,
		PlayerLog:          m.defaultCommand && video != nil,
		Video:              video,
		VideoWidth:         videoWidth,
		VideoHeight:        videoHeight,
		Volume:             m.startVolume(login),
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

// keepVolume records where sess left the volume, once it can no longer change.
func (m *Manager) keepVolume(sess *Session) {
	volume := sess.Volume()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.keptLogin, m.keptVolume = sess.cfg.Login, volume
}

// startVolume is the volume a new session of login opens at: where the same
// channel left it, so a retry changes nothing the user set, or the baseline for
// any other channel.
func (m *Manager) startVolume(login string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if login == m.keptLogin {
		return m.keptVolume
	}
	return baselineVolume
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
		m.keepVolume(sess)
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

// SetVideoOutput gives StartEmbedded somewhere to draw. It only enables that
// choice: Start keeps using the window. Call it before the first Start.
func (m *Manager) SetVideoOutput(out *SixelOutput) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.video = out
}

// ResizeVideo asks the active session for frames of a new size. A session still
// starting picks the size up when its IPC connects. A window session ignores it.
func (m *Manager) ResizeVideo(width, height int) error {
	m.mu.Lock()
	sess := m.session
	m.mu.Unlock()
	if sess == nil {
		return fmt.Errorf("player: no active session")
	}
	return sess.resizeVideo(width, height)
}

// SetVideoTrack turns the active session's video off and back on while its audio
// goes on. A session still starting applies it when its IPC connects. A window
// session ignores it: the panel says nothing about the window.
func (m *Manager) SetVideoTrack(on bool) error {
	m.mu.Lock()
	sess := m.session
	m.mu.Unlock()
	if sess == nil {
		return fmt.Errorf("player: no active session")
	}
	return sess.setVideoTrack(on)
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
// to the 0..100 range the TUI shows; mpv's default ceiling is 130. --no-osc drops
// the on-screen controller: it answers clicks outside our bindings and speaks English.
//
// endpoint goes to --input-ipc-server and inputConf to --input-conf; they are
// independent paths (the endpoint can be a pipe name).
func DefaultCommand(login, endpoint, inputConf string) *exec.Cmd {
	return playerCommand(login, endpoint, inputConf)
}

// commandNoInputConf is DefaultCommand for a session without an input.conf. The
// built-in bindings stay off: the window ignores keys and the TUI keeps working,
// which beats going back to bindings the user cannot see or undo.
func commandNoInputConf(login, endpoint string) *exec.Cmd {
	return playerCommand(login, endpoint, "")
}

// playerCommand omits --input-conf when inputConf is empty.
func playerCommand(login, endpoint, inputConf string) *exec.Cmd {
	return streamlinkCommand(login, mpvArgs(endpoint, inputConf))
}

// sixelSessionCommand is the session command that draws in the terminal.
func sixelSessionCommand(width, height int, withInputConf bool) sessionCommand {
	return func(login string, layout sessionLayout) *exec.Cmd {
		inputConf := ""
		if withInputConf {
			inputConf = inputConfPath(layout.Dir)
		}
		return sixelCommand(login, layout.Endpoint, inputConf, playerLogPath(layout.Dir), width, height)
	}
}

// sixelCommand runs mpv with its sixel output on stdout, which reaches a pipe of
// ours instead of the terminal.
//
// --no-terminal keeps mpv away from /dev/tty and off stdout with its own messages;
// the sixel output is not affected. -v hands the player's stdout to streamlink's,
// so --logfile moves streamlink's log off it: a line landing inside a frame would
// corrupt it, and the frame would swallow the line that announces an ad.
//
// logFile has to be a real file. streamlink resolves the path before opening it,
// and /dev/stderr on a pipe resolves to /proc/PID/fd/pipe:[N], which nothing can
// open: streamlink then dies in its logging setup, before any stream.
func sixelCommand(login, endpoint, inputConf, logFile string, width, height int) *exec.Cmd {
	return streamlinkCommand(login, sixelMPVArgs(endpoint, inputConf, width, height),
		"-v", "--logfile", logFile)
}

// sixelBox is the height and row count that make mpv draw frames exactly height
// tall. mpv keeps the last terminal row free and, with no terminal to ask, counts
// 25 rows: 360 would come out as 342. So each row is one sixel band, plus one band
// for the row it gives up. height is a multiple of the band.
func sixelBox(height int) (boxHeight, rows int) {
	return height + sixelBandHeight, height/sixelBandHeight + 1
}

// sixelMPVArgs undoes --force-window: it would keep the video output alive across
// vid=no, and resizeVideo depends on mpv rebuilding it.
func sixelMPVArgs(endpoint, inputConf string, width, height int) []string {
	boxHeight, rows := sixelBox(height)
	return append(mpvArgs(endpoint, inputConf),
		"--force-window=no",
		"--no-terminal",
		"--vo=sixel",
		fmt.Sprintf("--vo-sixel-width=%d", width),
		fmt.Sprintf("--vo-sixel-height=%d", boxHeight),
		fmt.Sprintf("--vo-sixel-rows=%d", rows),
		"--vo-sixel-buffered=yes",
		"--vo-sixel-alt-screen=no",
		"--vo-sixel-config-clear=no",
		fmt.Sprintf("--vf=fps=%d", sixelFPS),
	)
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

func streamlinkCommand(login string, mpv []string, extra ...string) *exec.Cmd {
	args := append([]string{"--no-config", "--loglevel", "info"}, extra...)
	args = append(args,
		fmt.Sprintf("https://www.twitch.tv/%s", login),
		"best",
		"--player", "mpv",
		"--player-args", buildPlayerArgs(mpv),
	)
	return exec.Command("streamlink", args...)
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
