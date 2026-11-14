package player

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/termtext"
)

type sessionConfig struct {
	Generation         uint64
	Target             Target
	RuntimeDir         string
	Command            sessionCommand
	CommandNoInputConf sessionCommand
	WriteInputConf     func(path string) error
	// Prepare builds the session's directory and IPC endpoint. Nil uses
	// prepareSession; tests inject a layout whose two parts share no root.
	Prepare      func(runtimeDir string, gen uint64) (sessionLayout, error)
	SocketWait   time.Duration
	StopWait     time.Duration
	WriteWait    time.Duration // bounds each IPC write; zero uses ipcWriteWait
	BreakMaxHold time.Duration
	RegistryDir  string
	Emit         func(Event)
	// Note records a problem for the exit log: the TUI owns the terminal while
	// the session runs. Nil drops it.
	Note func(string)
	// Volume is what the player is set to once its IPC connects.
	Volume int
	// Muted mutes the player once its IPC connects. False leaves mpv's own setting.
	Muted bool
}

type pendingKind int

const (
	pendingPause pendingKind = iota
	pendingVolume
	pendingMute
)

type pendingCommand struct {
	kind   pendingKind
	paused bool
	volume int
	muted  bool
	osd    bool
}

// Session is one supervised playback (one generation).
type Session struct {
	cfg sessionConfig
	// dir is the private session directory: input.conf lives in it and all of
	// it is removed with the session. endpoint is what mpv listens on and the
	// TUI dials (a socket file on Unix, a pipe name on Windows). They are not
	// derived from each other.
	dir      string
	endpoint string
	cmd      *exec.Cmd
	// group is set right after cmd starts; read it only once cmd.Process exists.
	group processGroup
	// groupGone is set when the group was found empty right after the leader was
	// reaped. From then on its id may lead another program's group, so the
	// session never signals or probes it again.
	groupGone atomic.Bool
	record    string
	// inputConfErr is why the session runs without its input.conf; set once
	// before the session starts, then only read.
	inputConfErr error

	cancel context.CancelFunc

	mu       sync.Mutex
	state    State
	lastLine string
	// failLine is the first error line with a recognized failure class (or a
	// later one that proves playback cannot start) and failClass that class;
	// failHint is a fallback class set by the supervisor (e.g. the IPC socket
	// deadline) when no line explains the failure.
	failLine  string
	failClass Failure
	failHint  Failure
	// traceback é verdadeiro depois da primeira linha "Traceback (most recent
	// call last):": a partir dela, um nome de exceção sozinho na linha conta.
	traceback bool
	// adHold is true between streamlink's verifiable hold-start and hold-end
	// lines. breakAt is when that hold started and resumeTo is the state to
	// show once it ends (starting/playing/paused as observed meanwhile).
	adHold          bool
	breakAt         time.Time
	resumeTo        State
	stopped         bool
	paused          bool
	desiredPaused   bool
	hasDesiredPause bool
	// desiredPauseSeq counts the wishes recorded, so a failed Space can tell
	// whether a newer one replaced its wish, even with the same value.
	desiredPauseSeq uint64
	volume          int
	// muted is mpv's mute as last observed; wantMuted is a toggle on its way to
	// mpv, so a second m before the reply flips it back.
	muted        bool
	wantMuted    bool
	hasWantMuted bool
	// wantMutedSeq counts the toggles recorded, like desiredPauseSeq: a failed
	// m undoes its wish only if no newer one replaced it, even with the same value.
	wantMutedSeq uint64
	waitDone     chan struct{}
	waitErr      error
	// failing is set once failFast has taken the session down: streamlink repeats
	// its error line in the log and on stdout, and only the first one acts.
	failing bool
	// readers counts the goroutines reading the process's output; outputs are
	// their read ends, closed once the readers are drained (or given up on).
	readers sync.WaitGroup
	outputs []*os.File

	cmdMu   sync.Mutex
	ipcConn net.Conn
	nextReq atomic.Int64
	pending map[int64]pendingCommand
}

// readerDrainWait bounds how long the exit path waits for the reader goroutines
// to consume what the process wrote before exiting. Without the wait a fast
// failure (error line, then exit) can be classified before its line is read.
// A descendant that inherited the pipes keeps them open past the exit; the
// deadline keeps that case from blocking the session.
const readerDrainWait = 500 * time.Millisecond

func startSession(cfg sessionConfig) (*Session, error) {
	prepare := cfg.Prepare
	if prepare == nil {
		prepare = prepareSession
	}
	layout, err := prepare(cfg.RuntimeDir, cfg.Generation)
	if err != nil {
		return nil, err
	}
	dir, endpoint := layout.Dir, layout.Endpoint

	// Written before streamlink starts: mpv reads it once, at startup. A failure
	// costs the keys in the mpv window, never the playback.
	command := cfg.Command
	inputConfErr := cfg.WriteInputConf(inputConfPath(dir))
	if inputConfErr != nil {
		if cfg.CommandNoInputConf != nil {
			command = cfg.CommandNoInputConf
		}
		emit := cfg.Emit
		cfg.Emit = func(ev Event) {
			ev.InputConfFailed = true
			emit(ev)
		}
	}

	// A plugin that cannot be written costs what it adds (the Kick ad filter),
	// never the playback: streamlink then plays with its own plugins.
	pluginDir, pluginErr := writePlugins(dir, cfg.Target.Plugins)
	if pluginErr != nil && cfg.Note != nil {
		cfg.Note(termtext.Line(fmt.Sprintf("player: streamlink plugins not written, playing with streamlink's own: %v", pluginErr)))
	}
	layout.PluginDir = pluginDir

	if cfg.WriteWait <= 0 {
		cfg.WriteWait = ipcWriteWait
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{
		cfg:      cfg,
		dir:      dir,
		endpoint: endpoint,
		cancel:   cancel,
		state:    StateStarting,
		volume:   cfg.Volume,
		muted:    cfg.Muted,
		waitDone: make(chan struct{}),
		pending:  make(map[int64]pendingCommand),

		inputConfErr: inputConfErr,
	}

	cmd := command(cfg.Target, layout)
	if cmd == nil {
		s.cleanupFiles()
		cancel()
		return nil, fmt.Errorf("player: command factory returned nil")
	}
	configureProcessGroup(cmd)
	if cmd.Stdout != nil || cmd.Stderr != nil {
		s.cleanupFiles()
		cancel()
		return nil, fmt.Errorf("player: command output already set")
	}
	// Own pipes instead of cmd.StdoutPipe/StderrPipe: Wait closes those read
	// ends as soon as the process exits, which can drop its final lines.
	stdout, stdoutW, err := os.Pipe()
	if err != nil {
		s.cleanupFiles()
		cancel()
		return nil, fmt.Errorf("player: stdout pipe: %w", err)
	}
	stderr, stderrW, err := os.Pipe()
	if err != nil {
		closeFiles(stdout, stdoutW)
		s.cleanupFiles()
		cancel()
		return nil, fmt.Errorf("player: stderr pipe: %w", err)
	}
	cmd.Stdout = stdoutW
	cmd.Stderr = stderrW
	s.outputs = []*os.File{stdout, stderr}

	s.emit(Event{
		Generation: cfg.Generation,
		State:      StateStarting,
		Target:     cfg.Target,
		Login:      cfg.Target.Identity(),
		Volume:     s.volume,
		Muted:      s.muted,
	})

	cmd, err = startProcess(cmd)
	s.cmd = cmd
	// The child holds its own copies of the write ends; ours must go so the
	// readers see EOF when the process (and its descendants) are done.
	closeFiles(stdoutW, stderrW)
	if err != nil {
		closeFiles(s.outputs...)
		s.cleanupFiles()
		cancel()
		return nil, &ExecError{Err: err}
	}
	s.group, err = startedGroup(cmd)
	if err != nil {
		// Nothing could stop this process later, so it does not get to play.
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		closeFiles(s.outputs...)
		s.cleanupFiles()
		cancel()
		return nil, err
	}

	if s.group.containmentDegraded() {
		emit := s.cfg.Emit
		s.cfg.Emit = func(ev Event) { ev.ContainmentDegraded = true; emit(ev) }
		s.emit(Event{Generation: cfg.Generation, State: StateStarting, Target: cfg.Target, Login: cfg.Target.Identity(), Volume: s.volume, Muted: s.muted})
	}

	if cfg.RegistryDir != "" {
		path, err := writeRecord(cfg.RegistryDir, Record{
			OwnerPID:   os.Getpid(),
			OwnerStart: selfStart,
			PGID:       cmd.Process.Pid,
			Socket:     endpoint,
			Dir:        dir,
			URL:        cfg.Target.URL,
			Label:      cfg.Target.Label,
			Login:      cfg.Target.Identity(),
			Generation: cfg.Generation,
			StartedAt:  time.Now(),
		})
		if err == nil {
			s.record = path
		}
	}

	s.readers.Add(2)
	go s.consumeLines(ctx, stdout)
	go s.consumeLines(ctx, stderr)
	go s.waitProcess()
	go s.superviseIPC(ctx)
	go s.superviseBreak(ctx)

	return s, nil
}

// Stop terminates the process group (SIGTERM, then SIGKILL; on Windows the Job
// Object at once), waits, and removes the session directory and endpoint.
func (s *Session) Stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		<-s.waitDone
		return
	}
	s.stopped = true
	intentional := s.state != StateError && s.state != StateEnded
	s.mu.Unlock()

	s.cancel()
	// The group goes first: closeIPC waits for cmdMu, which a command writing
	// to an mpv that stopped reading holds until its deadline. Once mpv is
	// gone, those writes fail at once.
	gone := s.terminateProcessGroup()
	s.closeIPC()
	<-s.waitDone
	s.cleanupFiles()
	s.group.release()

	if !gone {
		s.transition(StateError, "", s.survivorLine())
	} else if intentional {
		s.transition(StateEnded, EndReasonStopped, "")
	}
}

func (s *Session) closeIPC() {
	s.cmdMu.Lock()
	defer s.cmdMu.Unlock()
	if s.ipcConn != nil {
		_ = s.ipcConn.Close()
		s.ipcConn = nil
	}
	s.pending = make(map[int64]pendingCommand)
}

// terminateProcessGroup sends SIGTERM to the whole group and waits until both
// the supervised process has been reaped and no member of the group is left.
// The leader exiting is not enough: a descendant (mpv) that ignores SIGTERM keeps
// playing after streamlink is gone, so the deadline escalates to SIGKILL on the
// group whenever any member survives it. It reports false, and records it, when
// a member outlives SIGKILL too (stuck in D state).
func (s *Session) terminateProcessGroup() bool {
	if s.cmd == nil || s.cmd.Process == nil || s.groupGone.Load() {
		return true
	}
	_ = s.group.terminate()
	gone := waitGroupGone(s.group, s.waitDone, s.cfg.StopWait)
	if !gone {
		_ = s.group.kill()
		gone = waitGroupGone(s.group, s.waitDone, groupKillWait)
	}
	<-s.waitDone
	if !gone {
		s.note(s.survivorLine())
	}
	return gone
}

func (s *Session) survivorLine() string {
	return fmt.Sprintf("player: process group %d survived SIGKILL", s.cmd.Process.Pid)
}

// note records msg for the exit log already neutralized: it may quote a
// streamlink line, which the classification and the ad-hold detector read raw.
func (s *Session) note(msg string) {
	if s.cfg.Note != nil {
		s.cfg.Note(termtext.Line(msg))
	}
}

func (s *Session) waitProcess() {
	err := s.cmd.Wait()
	// Before waitDone: a Stop that sees it closed must also see groupGone.
	if !s.group.alive() {
		s.groupGone.Store(true)
	}
	s.mu.Lock()
	s.waitErr = err
	s.mu.Unlock()
	close(s.waitDone)
	s.drainReaders(readerDrainWait)

	s.mu.Lock()
	already := s.state == StateEnded || s.state == StateError || s.stopped
	last := s.lastLine
	classified := s.failClass != FailureNone
	var exitErr *exec.ExitError
	if !already && !classified && s.failHint == FailureNone && s.state == StateStarting && errors.As(err, &exitErr) {
		// Exiting with an error status before any stream reached the player,
		// and no output that explains it as the channel or the network (that
		// is checked on the last line when the failure is classified): this
		// streamlink build failed to run.
		s.failHint = FailureStreamlinkBroken
	}
	s.mu.Unlock()
	if already {
		s.cleanupFiles()
		return
	}
	if classified {
		// streamlink printed a recognized error line; some of them (offline)
		// can come with exit status 0, which is still a failure to the user.
		s.transition(StateError, "", "")
	} else if err != nil {
		msg := last
		if msg == "" {
			msg = err.Error()
		}
		s.transition(StateError, "", msg)
	} else {
		s.transition(StateEnded, EndReasonStreamlinkExit, last)
	}
	s.cleanupFiles()
}

// drainReaders waits up to wait for the reader goroutines to reach EOF, then
// closes the read ends. Past the deadline the session goes on with the lines
// read so far, and closing the files releases readers still blocked on a pipe
// that a descendant keeps open.
func (s *Session) drainReaders(wait time.Duration) {
	done := make(chan struct{})
	go func() {
		s.readers.Wait()
		close(done)
	}()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}
	closeFiles(s.outputs...)
}

func closeFiles(files ...*os.File) {
	for _, f := range files {
		if f != nil {
			_ = f.Close()
		}
	}
}

const streamlinkLineLimit = 1024 * 1024
const truncatedLineSuffix = " […]"

func (s *Session) consumeLines(ctx context.Context, r io.Reader) {
	defer s.readers.Done()
	reader := bufio.NewReaderSize(r, 64*1024)
	var line []byte
	truncated := false
	for {
		fragment, prefix, err := reader.ReadLine()
		if err != nil && (!errors.Is(err, io.EOF) || len(line) == 0) {
			return
		}
		remaining := streamlinkLineLimit - len(line)
		if len(fragment) > remaining {
			fragment = fragment[:remaining]
			truncated = true
		}
		line = append(line, fragment...)
		if !prefix {
			if truncated {
				// A marca ocupa parte do limite; o restante da linha já foi drenado.
				line = append(line[:streamlinkLineLimit-len(truncatedLineSuffix)], truncatedLineSuffix...)
			}
			s.handleLine(string(line))
			line = line[:0]
			truncated = false
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func (s *Session) handleLine(line string) {
	if isPluginFallbackLine(line) {
		// The plugin stepped aside and the built-in one plays: a problem for
		// the exit log, not evidence about the stream. Its detail quotes a
		// Python exception, which the classification would blame on streamlink.
		s.note("player: " + line)
		return
	}
	s.mu.Lock()
	f := FailureNone
	if strings.Contains(strings.ToLower(line), "error") {
		f = classifyLine(maskTarget(line, s.cfg.Target), s.traceback)
	}
	if isTracebackStart(line) {
		s.traceback = true
	}
	s.lastLine = line
	// A primeira classe fica, salvo quando uma linha posterior prova que a
	// reprodução não pode começar (canal offline, sem plugin, ferramenta
	// ausente): essa causa verificável vale mais que uma anterior mais fraca.
	if f != FailureNone && (s.failClass == FailureNone || isFatalLine(f) && !isFatalLine(s.failClass)) {
		s.failLine = line
		s.failClass = f
	}
	s.mu.Unlock()
	if isFatalLine(f) {
		// Playback cannot start (channel offline, player missing): report
		// it now and tear the process down instead of waiting for it.
		go s.failFast()
	}

	if !isYouTubeTarget(s.cfg.Target) {
		switch classifyAdLine(line) {
		case adSignalHoldStart:
			s.startAdHold()
		case adSignalHoldEnd:
			s.endAdHold()
		case adSignalUnknown:
			// Neither start nor end can be verified from here on: drop any hold
			// instead of waiting for BreakMaxHold.
			s.note("player: unrecognized streamlink ad-hold line: " + line)
			s.endAdHold()
		}
	}
}

func isYouTubeTarget(t Target) bool {
	if strings.HasPrefix(strings.ToLower(t.Label), "youtube:") {
		return true
	}
	u := strings.ToLower(t.URL)
	return strings.Contains(u, "://youtube.com/") || strings.Contains(u, "://www.youtube.com/") ||
		strings.Contains(u, "://m.youtube.com/") || strings.Contains(u, "://youtu.be/")
}

func (s *Session) superviseIPC(ctx context.Context) {
	// Being able to dial is the readiness criterion: it holds for a socket file
	// and for a named pipe alike, and needs no file to exist beforehand.
	conn, err := dialIPCUntil(ctx, s.waitDone, s.endpoint, time.Now().Add(s.cfg.SocketWait))
	if err != nil {
		if errors.Is(err, errIPCWaitAborted) {
			return
		}
		var last string
		s.mu.Lock()
		if errors.Is(err, errIPCNoListener) {
			// The endpoint exists but mpv never listened on it: not a timeout.
			last = "mpv did not open its control channel"
		} else {
			last = s.lastLine
			s.failHint = FailureTimeout
		}
		s.mu.Unlock()
		if last == "" {
			last = "mpv IPC socket did not appear"
		}
		s.transition(StateError, "", last)
		s.cancel()
		s.terminateProcessGroup()
		return
	}

	s.cmdMu.Lock()
	s.ipcConn = conn
	s.cmdMu.Unlock()
	defer s.closeIPC()

	_ = writeIPC(conn, s.cfg.WriteWait, map[string]any{
		"command": []any{"observe_property", 1, "idle-active"},
	})
	_ = writeIPC(conn, s.cfg.WriteWait, map[string]any{
		"command": []any{"observe_property", 2, "pause"},
	})
	_ = writeIPC(conn, s.cfg.WriteWait, map[string]any{
		"command": []any{"observe_property", 3, "volume"},
	})
	_ = writeIPC(conn, s.cfg.WriteWait, map[string]any{
		"command": []any{"observe_property", 4, "mute"},
	})
	// Explicit baseline after connect: mpv softvol can open above 100 (observed
	// 102) while the status bar starts at 100, and pause must not be presumed
	// from the Session zero value. Setting both here keeps the UI and mpv aligned
	// without reading get_property; later observe_property events still track
	// manual/window changes.
	s.applyPlaybackBaseline()
	readErr := make(chan error, 1)
	go func() {
		readErr <- readIPCMessages(conn, s.handleIPCMessage)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.waitDone:
			return
		case err := <-readErr:
			s.handleIPCReadDone(err)
			return
		}
	}
}

func (s *Session) handleIPCMessage(msg ipcMessage) {
	if msg.isCommandReply() {
		s.handleCommandReply(msg)
		return
	}
	if msg.indicatesPlaying() {
		s.mu.Lock()
		paused := s.paused
		s.mu.Unlock()
		if paused {
			s.transition(StatePaused, "", "")
		} else {
			s.transition(StatePlaying, "", "")
		}
	}
	if msg.Event == "property-change" {
		switch msg.Name {
		case "pause":
			if paused, ok := parseBoolData(msg.Data); ok {
				s.applyPauseObservation(paused)
			}
		case "volume":
			if vol, ok := parseVolumeData(msg.Data); ok {
				s.applyVolumeObservation(vol)
			}
		case "mute":
			if muted, ok := parseBoolData(msg.Data); ok {
				s.applyMuteObservation(muted)
			}
		}
	}
}

func (s *Session) handleCommandReply(msg ipcMessage) {
	s.cmdMu.Lock()
	pending, ok := s.pending[msg.RequestID]
	if ok {
		delete(s.pending, msg.RequestID)
	}
	s.cmdMu.Unlock()
	if !ok {
		// Reply for an unknown, superseded, or previous-generation command.
		return
	}
	if msg.Error != "success" {
		return
	}
	switch pending.kind {
	case pendingPause:
		s.applyPauseFromCommand(pending.paused)
	case pendingVolume:
		s.applyVolumeFromCommand(pending.volume)
	case pendingMute:
		s.applyMuteObservation(pending.muted)
	}
	if pending.osd {
		// Not from this goroutine: it reads what mpv writes, and a write that
		// waits for an mpv not reading would stop the replies behind it.
		go s.showControlOSD(pending.kind)
	}
}

// showControlOSD is called only after a requested control succeeds, never for
// baseline writes or property observations. OSD failure does not fail playback.
func (s *Session) showControlOSD(kind pendingKind) {
	text := "${volume}%"
	switch kind {
	case pendingPause:
		text = pauseText
	case pendingMute:
		text = muteText
	}
	s.cmdMu.Lock()
	defer s.cmdMu.Unlock()
	if s.ipcConn != nil {
		// A plain list reaches the screen as the literal text; the flag asks mpv to expand it.
		_ = writeIPC(s.ipcConn, s.cfg.WriteWait, map[string]any{"command": map[string]any{"name": "show-text", "text": text, "_flags": []any{"expand-properties"}}})
	}
}

func (s *Session) applyPauseObservation(paused bool) {
	s.mu.Lock()
	if s.stopped || s.state == StateEnded || s.state == StateError {
		s.mu.Unlock()
		return
	}
	s.paused = paused
	s.hasDesiredPause = false
	st := s.state
	s.mu.Unlock()

	switch {
	case paused && (st == StatePlaying || st == StatePaused || st == StateBreak):
		s.transition(StatePaused, "", "")
	case !paused && st == StatePaused:
		s.transition(StatePlaying, "", "")
	}
}

func (s *Session) applyPauseFromCommand(paused bool) {
	s.mu.Lock()
	if s.stopped || s.state == StateEnded || s.state == StateError {
		s.mu.Unlock()
		return
	}
	s.paused = paused
	if s.hasDesiredPause && s.desiredPaused == paused {
		s.hasDesiredPause = false
	}
	st := s.state
	s.mu.Unlock()

	if paused {
		if st == StatePlaying || st == StatePaused || st == StateBreak || st == StateStarting {
			// Only leave starting via explicit playing confirmation; still record pause.
			if st == StateStarting {
				s.emitStatus()
				return
			}
			s.transition(StatePaused, "", "")
		}
		return
	}
	if st == StatePaused {
		s.transition(StatePlaying, "", "")
		return
	}
	s.emitStatus()
}

func (s *Session) applyVolumeObservation(vol int) {
	vol = clampVolume(vol)
	s.mu.Lock()
	if s.stopped || s.state == StateEnded || s.state == StateError {
		s.mu.Unlock()
		return
	}
	if s.volume == vol {
		s.mu.Unlock()
		return
	}
	s.volume = vol
	s.mu.Unlock()
	s.emitStatus()
}

func (s *Session) applyVolumeFromCommand(vol int) {
	vol = clampVolume(vol)
	s.mu.Lock()
	if s.stopped || s.state == StateEnded || s.state == StateError {
		s.mu.Unlock()
		return
	}
	s.volume = vol
	s.mu.Unlock()
	s.emitStatus()
}

// applyMuteObservation records mpv's mute, observed or confirmed by a command.
func (s *Session) applyMuteObservation(muted bool) {
	s.mu.Lock()
	if s.stopped || s.state == StateEnded || s.state == StateError {
		s.mu.Unlock()
		return
	}
	if s.hasWantMuted && s.wantMuted == muted {
		s.hasWantMuted = false
	}
	if s.muted == muted {
		s.mu.Unlock()
		return
	}
	s.muted = muted
	s.mu.Unlock()
	s.emitStatus()
}

func (s *Session) emitStatus() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emitStatusLocked()
}

// emitStatusLocked emits the current state. The caller holds s.mu, so no other
// change can be emitted between this one and its event.
func (s *Session) emitStatusLocked() {
	ev := Event{
		Generation: s.cfg.Generation,
		State:      s.state,
		Target:     s.cfg.Target,
		Login:      s.cfg.Target.Identity(),
		LastLine:   s.lastLine,
		Volume:     s.volume,
		Muted:      s.muted,
	}
	if s.state == StateBreak && s.adHold {
		ev.BreakElapsed = time.Since(s.breakAt)
	}
	s.cfg.Emit(ev)
}

// applyPlaybackBaseline sets the session's volume and pause=false on the connected
// mpv. Chosen over get_property so softvol values above the UI clamp (0..100) are
// written back instead of only clamped in memory.
func (s *Session) applyPlaybackBaseline() {
	s.mu.Lock()
	s.desiredPaused = false
	s.hasDesiredPause = true
	s.desiredPauseSeq++
	s.mu.Unlock()
	_ = s.setVolumeWithOSD(s.cfg.Volume, false)
	if s.cfg.Muted {
		_ = s.sendCommand([]any{"set_property", "mute", true}, pendingCommand{kind: pendingMute, muted: true})
	}
	_ = s.sendCommand([]any{"set_property", "pause", false}, pendingCommand{
		kind:   pendingPause,
		paused: false,
	})
}

func (s *Session) togglePause() error {
	s.mu.Lock()
	if s.stopped || s.state == StateEnded || s.state == StateError {
		s.mu.Unlock()
		return fmt.Errorf("player: session not controllable")
	}
	base := s.paused
	if s.hasDesiredPause {
		base = s.desiredPaused
	}
	want := !base
	prevDesired, prevHas := s.desiredPaused, s.hasDesiredPause
	s.desiredPaused = want
	s.hasDesiredPause = true
	s.desiredPauseSeq++
	seq := s.desiredPauseSeq
	s.mu.Unlock()

	err := s.sendCommand([]any{"set_property", "pause", want}, pendingCommand{
		kind:   pendingPause,
		paused: want,
		osd:    true,
	})
	if err != nil {
		// The command never reached mpv, so the wish it carried is undone
		// unless something newer replaced it meanwhile.
		s.mu.Lock()
		if s.hasDesiredPause && s.desiredPauseSeq == seq {
			s.desiredPaused, s.hasDesiredPause = prevDesired, prevHas
		}
		s.mu.Unlock()
	}
	return err
}

// toggleMute flips mpv's mute. A toggle still waiting for its reply counts as
// done, so two quick presses end where they started.
func (s *Session) toggleMute() error {
	s.mu.Lock()
	if s.stopped || s.state == StateEnded || s.state == StateError {
		s.mu.Unlock()
		return fmt.Errorf("player: session not controllable")
	}
	base := s.muted
	if s.hasWantMuted {
		base = s.wantMuted
	}
	want := !base
	prevWant, prevHas := s.wantMuted, s.hasWantMuted
	s.wantMuted, s.hasWantMuted = want, true
	s.wantMutedSeq++
	seq := s.wantMutedSeq
	s.mu.Unlock()

	err := s.sendCommand([]any{"set_property", "mute", want}, pendingCommand{kind: pendingMute, muted: want, osd: true})
	if err != nil {
		s.mu.Lock()
		if s.hasWantMuted && s.wantMutedSeq == seq {
			s.wantMuted, s.hasWantMuted = prevWant, prevHas
		}
		s.mu.Unlock()
	}
	return err
}

func (s *Session) adjustVolume(delta int) error {
	s.mu.Lock()
	if s.stopped || s.state == StateEnded || s.state == StateError {
		s.mu.Unlock()
		return fmt.Errorf("player: session not controllable")
	}
	want := clampVolume(s.volume + delta)
	s.mu.Unlock()
	return s.setVolume(want)
}

func (s *Session) setVolume(v int) error {
	return s.setVolumeWithOSD(v, true)
}

func (s *Session) setVolumeWithOSD(v int, osd bool) error {
	v = clampVolume(v)
	s.mu.Lock()
	if s.stopped || s.state == StateEnded || s.state == StateError {
		s.mu.Unlock()
		return fmt.Errorf("player: session not controllable")
	}
	s.mu.Unlock()

	return s.sendCommand([]any{"set_property", "volume", v}, pendingCommand{
		kind:   pendingVolume,
		volume: v,
		osd:    osd,
	})
}

func (s *Session) sendCommand(cmd []any, pending pendingCommand) error {
	s.cmdMu.Lock()
	defer s.cmdMu.Unlock()
	if s.ipcConn == nil {
		return fmt.Errorf("player: ipc not connected")
	}
	for id, p := range s.pending {
		if p.kind == pending.kind {
			delete(s.pending, id)
		}
	}
	id := s.nextReq.Add(1)
	s.pending[id] = pending
	if err := writeIPC(s.ipcConn, s.cfg.WriteWait, map[string]any{
		"command":    cmd,
		"request_id": id,
	}); err != nil {
		delete(s.pending, id)
		return fmt.Errorf("player: ipc write: %w", err)
	}
	return nil
}

func (s *Session) handleIPCReadDone(err error) {
	s.mu.Lock()
	already := s.state == StateEnded || s.state == StateError || s.stopped
	s.mu.Unlock()
	if already {
		return
	}

	procAlive := false
	select {
	case <-s.waitDone:
	default:
		procAlive = s.cmd != nil && s.cmd.Process != nil
	}

	// Clean EOF and abrupt peer death (ECONNRESET when unread writes remain) both
	// mean the IPC endpoint is gone; that is an ended playback, not a soft error.
	if isIPCPeerGone(err) {
		if procAlive {
			// Parent still running but IPC closed → treat as mpv death with socket path open.
			s.transition(StateEnded, EndReasonMPVDead, "")
			return
		}
		s.transition(StateEnded, EndReasonSocketEOF, "")
		return
	}
	s.mu.Lock()
	last := s.lastLine
	s.mu.Unlock()
	if last == "" {
		last = err.Error()
	}
	s.transition(StateError, "", last)
}

// breakTick is how often an active hold refreshes its elapsed counter and
// checks the BreakMaxHold ceiling.
const breakTick = 250 * time.Millisecond

// superviseBreak drives the interval counter. It only emits while a verifiable
// hold is active; silence on the IPC socket never starts one.
func (s *Session) superviseBreak(ctx context.Context) {
	ticker := time.NewTicker(breakTick)
	defer ticker.Stop()
	lastSec := -1
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.waitDone:
			return
		case <-ticker.C:
		}
		s.mu.Lock()
		hold := s.adHold
		st := s.state
		var elapsed time.Duration
		if hold {
			elapsed = time.Since(s.breakAt)
		}
		s.mu.Unlock()
		if !hold {
			lastSec = -1
			continue
		}
		if s.cfg.BreakMaxHold > 0 && elapsed >= s.cfg.BreakMaxHold {
			// Start without a matching end: stop claiming an interval instead
			// of counting forever.
			s.endAdHold()
			lastSec = -1
			continue
		}
		if st != StateBreak {
			continue
		}
		if sec := int(elapsed / time.Second); sec != lastSec {
			lastSec = sec
			s.emitStatus()
		}
	}
}

// startAdHold records streamlink's hold-start line. While paused the state stays
// paused; the interval shows if playback is unpaused before the hold ends.
func (s *Session) startAdHold() {
	s.mu.Lock()
	if s.stopped || s.state == StateEnded || s.state == StateError || s.adHold {
		s.mu.Unlock()
		return
	}
	s.adHold = true
	s.breakAt = time.Now()
	s.resumeTo = s.state
	if s.state == StatePaused {
		s.mu.Unlock()
		return
	}
	s.state = StateBreak
	s.emitStatusLocked()
	s.mu.Unlock()
}

// endAdHold handles streamlink's hold-end line (or the BreakMaxHold ceiling)
// and returns to the state observed before or during the hold.
func (s *Session) endAdHold() {
	s.mu.Lock()
	if !s.adHold {
		s.mu.Unlock()
		return
	}
	s.adHold = false
	s.breakAt = time.Time{}
	target := s.resumeTo
	inBreak := s.state == StateBreak
	s.mu.Unlock()
	if !inBreak {
		return
	}
	if target == "" || target == StateBreak {
		target = StatePlaying
	}
	s.transition(target, "", "")
}

func (s *Session) transition(state State, reason EndReason, lastLine string) {
	s.mu.Lock()
	if s.stopped && state != StateEnded && state != StateError {
		s.mu.Unlock()
		return
	}
	if s.state == StateEnded || s.state == StateError {
		// Keep first terminal state.
		s.mu.Unlock()
		return
	}
	if state == StatePaused {
		s.paused = true
	}
	if state == StatePlaying {
		s.paused = false
	}
	switch {
	case state == StateEnded || state == StateError:
		s.adHold = false
		s.breakAt = time.Time{}
	case s.adHold && (state == StatePlaying || state == StateStarting):
		// The hold is still active: remember where to go when it ends.
		s.resumeTo = state
		state = StateBreak
	case s.adHold && state == StatePaused:
		s.resumeTo = StatePaused
	}
	s.state = state
	if lastLine != "" {
		s.lastLine = lastLine
	}
	ev := Event{
		Generation: s.cfg.Generation,
		State:      state,
		Target:     s.cfg.Target,
		Login:      s.cfg.Target.Identity(),
		LastLine:   s.lastLine,
		Reason:     reason,
		Volume:     s.volume,
		Muted:      s.muted,
	}
	if state == StateError {
		if s.failClass != FailureNone {
			// The recognized error line is the detail, not whatever streamlink
			// printed after it.
			ev.Failure = s.failClass
			ev.LastLine = s.failLine
			s.lastLine = s.failLine
		} else {
			ev.Failure = classifyTerminal(s.lastLine, s.cfg.Target, s.failHint)
		}
	}
	if state == StateBreak && s.adHold {
		ev.BreakElapsed = time.Since(s.breakAt)
	}
	// Under s.mu: events leave in the order of the changes they report.
	s.cfg.Emit(ev)
	s.mu.Unlock()
}

// failFast ends a session whose streamlink output already proved playback
// cannot start. The error state is published before the process group is
// terminated, so the exit is not reported as a separate end.
func (s *Session) failFast() {
	s.mu.Lock()
	skip := s.stopped || s.failing
	s.failing = true
	s.mu.Unlock()
	if skip {
		return
	}
	s.transition(StateError, "", "")
	s.cancel()
	s.terminateProcessGroup()
}

func (s *Session) emit(ev Event) {
	s.cfg.Emit(ev)
}

func (s *Session) cleanupFiles() {
	removeEndpoint(s.endpoint)
	if s.dir != "" {
		_ = os.RemoveAll(s.dir)
	}
	// Keep the record while any group member lives, so a later run can still
	// find the player if this process dies before Stop.
	if s.record != "" && (s.cmd == nil || s.cmd.Process == nil || s.groupGone.Load() || !s.group.alive()) {
		_ = os.Remove(s.record)
	}
}

// Pid returns the supervised process PID, or 0.
func (s *Session) Pid() int {
	if s == nil || s.cmd == nil || s.cmd.Process == nil {
		return 0
	}
	return s.cmd.Process.Pid
}

// Volume returns the last known volume (0..100).
func (s *Session) Volume() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.volume
}

// Muted reports mpv's mute as last known.
func (s *Session) Muted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.muted
}

// IPCEndpoint returns the endpoint mpv listens on: a socket path on Unix, a
// pipe name on Windows (tests/diagnostics).
func (s *Session) IPCEndpoint() string {
	return s.endpoint
}
