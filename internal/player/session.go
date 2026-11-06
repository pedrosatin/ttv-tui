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
)

type sessionConfig struct {
	Generation         uint64
	Login              string
	RuntimeDir         string
	Command            sessionCommand
	CommandNoInputConf sessionCommand
	WriteInputConf     func(path string) error
	// Prepare builds the session's directory and IPC endpoint. Nil uses
	// prepareSession; tests inject a layout whose two parts share no root.
	Prepare      func(runtimeDir string, gen uint64) (sessionLayout, error)
	SocketWait   time.Duration
	StopWait     time.Duration
	BreakMaxHold time.Duration
	RegistryDir  string
	Emit         func(Event)
}

type pendingKind int

const (
	pendingPause pendingKind = iota
	pendingVolume
)

type pendingCommand struct {
	kind   pendingKind
	paused bool
	volume int
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
	group  processGroup
	record string
	// inputConfErr is why the session runs without its input.conf; set once
	// before the session starts, then only read.
	inputConfErr error

	cancel context.CancelFunc

	mu       sync.Mutex
	state    State
	lastLine string
	// failLine is the first error line with a recognized failure class and
	// failClass that class; failHint is a fallback class set by the supervisor
	// (e.g. the IPC socket deadline) when no line explains the failure.
	failLine  string
	failClass Failure
	failHint  Failure
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
	volume          int
	waitDone        chan struct{}
	waitErr         error
	// readers counts the stdout/stderr reader goroutines; outputs are their
	// read ends, closed once the readers are drained (or given up on).
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

	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{
		cfg:      cfg,
		dir:      dir,
		endpoint: endpoint,
		cancel:   cancel,
		state:    StateStarting,
		volume:   100,
		waitDone: make(chan struct{}),
		pending:  make(map[int64]pendingCommand),

		inputConfErr: inputConfErr,
	}

	cmd := command(cfg.Login, layout)
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
		Login:      cfg.Login,
		Volume:     s.volume,
	})

	cmd, err = startProcess(cmd)
	s.cmd = cmd
	// The child holds its own copies of the write ends; ours must go so the
	// readers see EOF when the process (and its descendants) are done.
	closeFiles(stdoutW, stderrW)
	if err != nil {
		closeFiles(stdout, stderr)
		s.cleanupFiles()
		cancel()
		return nil, fmt.Errorf("player: start: %w", err)
	}
	s.group, err = startedGroup(cmd)
	if err != nil {
		// Nothing could stop this process later, so it does not get to play.
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		closeFiles(stdout, stderr)
		s.cleanupFiles()
		cancel()
		return nil, err
	}

	if s.group.containmentDegraded() {
		emit := s.cfg.Emit
		s.cfg.Emit = func(ev Event) { ev.ContainmentDegraded = true; emit(ev) }
		s.emit(Event{Generation: cfg.Generation, State: StateStarting, Login: cfg.Login, Volume: s.volume})
	}

	if cfg.RegistryDir != "" {
		path, err := writeRecord(cfg.RegistryDir, Record{
			OwnerPID:   os.Getpid(),
			OwnerStart: selfStart,
			PGID:       cmd.Process.Pid,
			Socket:     endpoint,
			Dir:        dir,
			Login:      cfg.Login,
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
	s.closeIPC()
	s.terminateProcessGroup()
	<-s.waitDone
	s.cleanupFiles()
	s.group.release()

	if intentional {
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
// group whenever any member survives it.
func (s *Session) terminateProcessGroup() {
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}
	_ = s.group.terminate()
	if !waitGroupGone(s.group, s.waitDone, s.cfg.StopWait) {
		_ = s.group.kill()
		_ = waitGroupGone(s.group, s.waitDone, groupKillWait)
	}
	<-s.waitDone
}

func (s *Session) waitProcess() {
	err := s.cmd.Wait()
	s.mu.Lock()
	s.waitErr = err
	s.mu.Unlock()
	close(s.waitDone)
	s.drainReaders(readerDrainWait)

	s.mu.Lock()
	already := s.state == StateEnded || s.state == StateError || s.stopped
	last := s.lastLine
	classified := s.failClass != FailureNone
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

func (s *Session) consumeLines(ctx context.Context, r io.Reader) {
	defer s.readers.Done()
	sc := bufio.NewScanner(r)
	// streamlink lines can be long when dumping playlist metadata
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		f := FailureNone
		if strings.Contains(strings.ToLower(line), "error") {
			f = ClassifyLine(line)
		}
		s.mu.Lock()
		s.lastLine = line
		if f != FailureNone && s.failClass == FailureNone {
			s.failLine = line
			s.failClass = f
		}
		s.mu.Unlock()
		if isFatalLine(f) {
			// Playback cannot start (channel offline, player missing): report
			// it now and tear the process down instead of waiting for it.
			go s.failFast()
		}

		switch classifyAdLine(line) {
		case adSignalHoldStart:
			s.startAdHold()
		case adSignalHoldEnd:
			s.endAdHold()
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
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

	_ = writeIPC(conn, map[string]any{
		"command": []any{"observe_property", 1, "idle-active"},
	})
	_ = writeIPC(conn, map[string]any{
		"command": []any{"observe_property", 2, "pause"},
	})
	_ = writeIPC(conn, map[string]any{
		"command": []any{"observe_property", 3, "volume"},
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
	}
	if pending.osd {
		s.showControlOSD(pending.kind)
	}
}

// showControlOSD is called only after a requested control succeeds, never for
// baseline writes or property observations. OSD failure does not fail playback.
func (s *Session) showControlOSD(kind pendingKind) {
	text := "${volume}%"
	if kind == pendingPause {
		text = pauseText
	}
	s.cmdMu.Lock()
	defer s.cmdMu.Unlock()
	if s.ipcConn != nil {
		_ = writeIPC(s.ipcConn, map[string]any{"command": []any{"show-text", text}})
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

func (s *Session) emitStatus() {
	s.mu.Lock()
	ev := Event{
		Generation: s.cfg.Generation,
		State:      s.state,
		Login:      s.cfg.Login,
		LastLine:   s.lastLine,
		Volume:     s.volume,
	}
	if s.state == StateBreak && s.adHold {
		ev.BreakElapsed = time.Since(s.breakAt)
	}
	s.mu.Unlock()
	s.cfg.Emit(ev)
}

// applyPlaybackBaseline sets volume=100 and pause=false on the connected mpv.
// Chosen over get_property so softvol values above the UI clamp (0..100) are
// written back instead of only clamped in memory.
func (s *Session) applyPlaybackBaseline() {
	s.mu.Lock()
	s.desiredPaused = false
	s.hasDesiredPause = true
	s.mu.Unlock()
	_ = s.setVolumeWithOSD(100, false)
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
	s.desiredPaused = want
	s.hasDesiredPause = true
	s.mu.Unlock()

	return s.sendCommand([]any{"set_property", "pause", want}, pendingCommand{
		kind:   pendingPause,
		paused: want,
		osd:    true,
	})
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
	if err := writeIPC(s.ipcConn, map[string]any{
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
	s.mu.Unlock()
	s.emitStatus()
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
		Login:      s.cfg.Login,
		LastLine:   s.lastLine,
		Reason:     reason,
		Volume:     s.volume,
	}
	if state == StateError {
		if s.failClass != FailureNone {
			// The recognized error line is the detail, not whatever streamlink
			// printed after it.
			ev.Failure = s.failClass
			ev.LastLine = s.failLine
			s.lastLine = s.failLine
		} else {
			ev.Failure = classifyTerminal(s.lastLine, s.failHint)
		}
	}
	if state == StateBreak && s.adHold {
		ev.BreakElapsed = time.Since(s.breakAt)
	}
	s.mu.Unlock()
	s.cfg.Emit(ev)
}

// failFast ends a session whose streamlink output already proved playback
// cannot start. The error state is published before the process group is
// terminated, so the exit is not reported as a separate end.
func (s *Session) failFast() {
	s.mu.Lock()
	stopped := s.stopped
	s.mu.Unlock()
	if stopped {
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
	if s.record != "" && (s.cmd == nil || s.cmd.Process == nil || !s.group.alive()) {
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

// IPCEndpoint returns the endpoint mpv listens on: a socket path on Unix, a
// pipe name on Windows (tests/diagnostics).
func (s *Session) IPCEndpoint() string {
	return s.endpoint
}
