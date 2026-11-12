package chat

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

// lineResult is one complete IRC line read from the socket, or the error that
// ended the read loop.
type lineResult struct {
	line string
	err  error
}

// connSession holds the state of a single IRC connection: one dial, one
// handshake and the serve loop that follows. A reconnect builds a fresh one.
type connSession struct {
	m       *Manager
	ctx     context.Context
	conn    net.Conn
	gen     uint64
	channel string
	anon    bool
	nick    string

	roomState   *RoomState
	readTimeout time.Duration

	batch      []Message
	updates    []SendStatusUpdate
	sendWaitCh <-chan time.Time

	// connectedOnce turns true on welcome or our JOIN echo; the
	// caller uses it to reset the reconnect backoff.
	connectedOnce bool
}

// closeOnCancel closes the connection as soon as ctx is cancelled, so a pending
// Read or Write unblocks immediately. The returned stop ends the watcher and
// waits for it.
func closeOnCancel(ctx context.Context, closeConn func()) (stop func()) {
	stopWatch := make(chan struct{})
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		select {
		case <-ctx.Done():
			closeConn()
		case <-stopWatch:
		}
	}()
	return func() { close(stopWatch); <-watchDone }
}

// open sends the handshake, arms the first read deadline and announces the
// connected state.
func (s *connSession) open() error {
	m := s.m
	if err := s.conn.SetWriteDeadline(time.Now().Add(min(m.opts.PongTimeout, m.opts.SendTimeout))); err != nil {
		return err
	}
	nick, err := m.writeHandshake(s.conn, s.channel, s.anon)
	if err != nil {
		return fmt.Errorf("handshake: %w", err)
	}

	s.nick = nick
	s.readTimeout = m.opts.PingInterval + m.opts.PongTimeout
	if err := s.conn.SetReadDeadline(time.Now().Add(s.readTimeout)); err != nil {
		return err
	}
	m.mu.Lock()
	if m.gen == s.gen {
		m.connected = true
	}
	m.mu.Unlock()
	s.emit(SessionEvent{})
	return nil
}

// write sends one raw line, bounded by the socket write allowance and by
// expiresAt when it is set and earlier.
func (s *connSession) write(line string, expiresAt time.Time) error {
	deadline := time.Now().Add(min(s.m.opts.PongTimeout, s.m.opts.SendTimeout))
	if !expiresAt.IsZero() && expiresAt.Before(deadline) {
		deadline = expiresAt
	}
	if err := s.conn.SetWriteDeadline(deadline); err != nil {
		return err
	}
	_, err := io.WriteString(s.conn, line)
	return err
}

// emit delivers ev as an event of this connection, in the connected state.
func (s *connSession) emit(ev SessionEvent) {
	ev.Generation = s.gen
	ev.Channel = s.channel
	ev.State = StateConnected
	ev.Anonymous = s.anon
	s.m.emit(s.ctx, ev)
}

// flush delivers the coalesced messages and delivery updates, if any.
func (s *connSession) flush() {
	if len(s.batch) == 0 && len(s.updates) == 0 {
		return
	}
	outMsgs := s.batch
	outUpds := s.updates
	s.batch = nil
	s.updates = nil
	s.emit(SessionEvent{Messages: outMsgs, SendUpdates: outUpds})
}

// addUpdate queues a delivery update for req to go out on the next flush.
func (s *connSession) addUpdate(req SendRequest, status DeliveryStatus, reason RejectReason, detail string) {
	s.updates = append(s.updates, SendStatusUpdate{
		Generation: s.gen,
		Channel:    s.channel,
		Nonce:      req.Nonce,
		Body:       req.Body,
		Status:     status,
		Reason:     reason,
		Detail:     detail,
	})
}

// readLines feeds complete lines into linesCh, renewing the read deadline on
// each one, and ends with the error that stopped the scan. It closes linesCh.
func readLines(ctx context.Context, conn net.Conn, readTimeout time.Duration, linesCh chan<- lineResult) {
	defer close(linesCh)
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, scannerInitialBuf), scannerMaxBuf)
	for scanner.Scan() {
		if err := conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
			return
		}
		select {
		case linesCh <- lineResult{line: scanner.Text()}:
		case <-ctx.Done():
			return
		}
	}
	scanErr := scanner.Err()
	if scanErr == nil {
		scanErr = io.EOF
	}
	select {
	case linesCh <- lineResult{err: scanErr}:
	case <-ctx.Done():
	}
}

// servesLocked reports whether the manager still runs this very session.
// Callers hold m.mu.
func (m *Manager) servesLocked(gen uint64, channel string) bool {
	return !m.closed && m.gen == gen && m.channel == channel
}

// belongsElsewhere reports whether a queued request was composed for another
// generation or channel.
func belongsElsewhere(req SendRequest, gen uint64, channel string) bool {
	return (req.Generation != 0 && req.Generation != gen) ||
		(req.Channel != "" && !strings.EqualFold(req.Channel, channel))
}

// sendQueued writes every queued message the rate limit allows right now.
func (s *connSession) sendQueued() error {
	for {
		more, err := s.sendNext()
		if err != nil || !more {
			return err
		}
	}
}

// sendNext handles the head of the send queue: it writes it, discards it, or
// arms the rate limit wait. more is false when the queue has to rest.
func (s *connSession) sendNext() (more bool, err error) {
	m := s.m
	s.updates = append(s.updates, m.expireSends(s.gen, s.channel)...)
	m.mu.Lock()
	if !m.servesLocked(s.gen, s.channel) || len(m.pending) == 0 {
		m.mu.Unlock()
		s.sendWaitCh = nil
		return false, nil
	}
	head := m.pending[0]
	if belongsElsewhere(head.req, s.gen, s.channel) {
		head.stopTimer()
		m.pending = m.pending[1:]
		m.mu.Unlock()
		s.addUpdate(head.req, DeliveryDiscarded, RejectChannelSwitched, "")
		s.flush()
		return true, nil
	}

	wait, allowedNow, windowFull := m.limiter.DelayUntilReady(s.channel)
	if windowFull {
		head.stopTimer()
		m.pending = m.pending[1:]
		m.mu.Unlock()
		s.addUpdate(head.req, DeliveryDiscarded, RejectRateLimited, "")
		s.flush()
		return true, nil
	}
	if !allowedNow {
		m.mu.Unlock()
		if s.sendWaitCh == nil {
			s.sendWaitCh = m.opts.After(wait)
		}
		return false, nil
	}

	// Re-verify generation and active channel under lock right before writing to wire.
	if s.ctx.Err() != nil || !m.servesLocked(s.gen, s.channel) {
		m.mu.Unlock()
		return false, nil
	}
	m.pending = m.pending[1:]
	m.limiter.Record(s.channel)
	m.inFlight = append(m.inFlight, head)
	m.mu.Unlock()
	s.sendWaitCh = nil

	wire := FormatPrivMsg(s.channel, head.req.Nonce, head.req.Body)
	if werr := s.write(wire, head.expiresAt); werr != nil {
		return false, fmt.Errorf("write privmsg: %w", werr)
	}
	return true, nil
}

// serve runs the connection until it fails, the server asks for a reconnect or
// ctx is cancelled. Whatever was coalesced is flushed before a failure returns.
func (s *connSession) serve(sendCh <-chan struct{}, linesCh <-chan lineResult) error {
	m := s.m
	ticker := time.NewTicker(m.opts.CoalesceInterval)
	defer ticker.Stop()

	heartbeat := time.NewTicker(m.opts.PingInterval)
	defer heartbeat.Stop()

	// Drain any pending messages queued right as the connection came up.
	if err := s.sendQueued(); err != nil {
		s.flush()
		return err
	}

	for {
		var err error
		select {
		case <-s.ctx.Done():
			return s.ctx.Err()

		case <-sendCh:
			err = s.sendQueued()

		case <-s.sendWaitCh:
			s.sendWaitCh = nil
			err = s.sendQueued()

		case <-heartbeat.C:
			if werr := s.write("PING :ttv-tui\r\n", time.Time{}); werr != nil {
				err = fmt.Errorf("heartbeat write: %w", werr)
			}

		case <-ticker.C:
			s.flush()

		case res, ok := <-linesCh:
			err = s.handleRead(res, ok)
		}
		if err != nil {
			s.flush()
			return err
		}
	}
}

// handleRead turns the end of the read loop into the error that closes the
// connection, and hands every complete line to handleLine.
func (s *connSession) handleRead(res lineResult, ok bool) error {
	if !ok {
		return io.EOF
	}
	if res.err != nil {
		var netErr net.Error
		if errors.As(res.err, &netErr) && netErr.Timeout() {
			return fmt.Errorf("chat heartbeat timeout: no PONG or traffic for %s: %w", s.readTimeout, res.err)
		}
		return fmt.Errorf("read: %w", res.err)
	}
	return s.handleLine(res.line)
}

// handleLine dispatches one IRC line. Any complete line is proof of life.
func (s *connSession) handleLine(line string) error {
	ev := ParseLine(line)
	if ev.Command == "001" || (ev.Command == "JOIN" && strings.EqualFold(ev.User, s.nick) && ev.Channel == s.channel) {
		s.connectedOnce = true
	}
	switch ev.Kind {
	case EventPing:
		if werr := s.write(FormatPong(ev.PingPayload), time.Time{}); werr != nil {
			return fmt.Errorf("write pong: %w", werr)
		}
	case EventPrivMsg:
		s.handlePrivMsg(ev)
	case EventNotice:
		s.handleNotice(ev)
		if reason, rejected := ClassifyRejectNotice(ev.MsgID, ev.Body); rejected && reason == RejectUnauthenticated {
			return ErrAuthentication
		}
	case EventRoomState:
		s.handleRoomState(ev)
	case EventClearChat:
		s.handleClearChat(ev)
	case EventClearMsg:
		s.flush()
		s.emit(SessionEvent{ClearUser: ev.TargetUser, ClearMsgID: ev.TargetMsgID})
	case EventUserNotice:
		s.handleUserNotice(ev)
	case EventReconnect:
		return errors.New("server requested RECONNECT")
	case EventOther:
		s.handleOther(ev)
	}
	return nil
}

// forThisChannel reports whether ev targets the channel this session serves.
func (s *connSession) forThisChannel(ev Event) bool {
	return ev.Channel == "" || strings.EqualFold(ev.Channel, s.channel)
}

// handlePrivMsg confirms an own message echoed back by the server, or queues
// the chat line for the next batch.
func (s *connSession) handlePrivMsg(ev Event) {
	if !s.forThisChannel(ev) {
		return
	}
	clientNonce := ""
	if ev.Tags != nil {
		clientNonce = ev.Tags["client-nonce"]
	}
	if matched, ok := s.m.matchInFlight(clientNonce, ev.User); ok {
		s.addUpdate(matched, DeliveryConfirmed, "", "")
		s.flush()
		return
	}
	s.batch = append(s.batch, Message{
		Generation:  s.gen,
		Kind:        MessageChat,
		FirstMsg:    ev.Tags["first-msg"] == "1",
		ReceivedAt:  time.Now(),
		ID:          ev.ID,
		User:        ev.User,
		DisplayName: ev.DisplayName,
		Color:       ev.Color,
		Badges:      ev.Badges,
		MsgID:       ev.MsgID,
		Highlighted: ev.Highlighted,
		Action:      ev.Action,
		Body:        ev.Body,
	})
}

// handleNotice rejects the earliest in-flight message when the notice is a
// refusal, and shows the notice text.
func (s *connSession) handleNotice(ev Event) {
	if reason, isReject := ClassifyRejectNotice(ev.MsgID, ev.Body); isReject {
		if matched, ok := s.m.popEarliestInFlight(); ok {
			s.addUpdate(matched, DeliveryRejected, reason, ev.Body)
		}
	}
	if ev.Body != "" {
		s.batch = append(s.batch, Message{
			Generation: s.gen,
			Kind:       MessageNotice,
			MsgID:      ev.MsgID,
			Body:       ev.Body,
		})
	}
	s.flush()
}

// handleRoomState folds the delta into the room state kept across reconnects
// and publishes a copy of it.
func (s *connSession) handleRoomState(ev Event) {
	*s.roomState = ev.RoomDelta.Apply(*s.roomState)
	rsCopy := *s.roomState
	s.flush()
	s.emit(SessionEvent{RoomState: &rsCopy})
}

// handleClearChat publishes a chat clear, a timeout or a ban.
func (s *connSession) handleClearChat(ev Event) {
	s.flush()
	var banDur *int
	if ev.BanDuration != nil {
		d := *ev.BanDuration
		banDur = &d
	}
	s.emit(SessionEvent{ClearChat: true, ClearUser: ev.TargetUser, BanDuration: banDur})
}

// handleUserNotice queues a subscription, gift or raid line.
func (s *connSession) handleUserNotice(ev Event) {
	if !s.forThisChannel(ev) {
		return
	}
	// Every kind becomes a system line, including the ones this client
	// does not know: the UI falls back on msg-id when system-msg is empty.
	s.batch = append(s.batch, Message{
		Generation:      s.gen,
		Kind:            MessageSystem,
		UserNotice:      true,
		ID:              ev.ID,
		User:            ev.User,
		DisplayName:     ev.DisplayName,
		Color:           ev.Color,
		Badges:          ev.Badges,
		MsgID:           ev.MsgID,
		SystemMsg:       ev.SystemMsg,
		Body:            ev.Body,
		Months:          ev.CumulativeMonths,
		CommunityGiftID: ev.CommunityGiftID,
		GiftCount:       ev.GiftCount,
		GiftRecipient:   ev.GiftRecipient,
	})
}

// handleOther confirms an in-flight message when the server answers it with
// USERSTATE. Every other command is only proof of life.
func (s *connSession) handleOther(ev Event) {
	if ev.Command != "USERSTATE" || !s.forThisChannel(ev) {
		return
	}
	clientNonce := ""
	if ev.Tags != nil {
		clientNonce = ev.Tags["client-nonce"]
	}
	if matched, ok := s.m.matchInFlightByUserState(clientNonce); ok {
		s.addUpdate(matched, DeliveryConfirmed, "", "")
		s.flush()
	}
}

// connectAndServe dials, authenticates and serves one connection. It returns
// when the connection ends, with connectedOnce telling whether the server sent
// a welcome or our JOIN echo.
func (m *Manager) connectAndServe(
	ctx context.Context,
	gen uint64,
	channel string,
	anon bool,
	roomState *RoomState,
	sendCh <-chan struct{},
) (connectedOnce bool, err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	conn, err := m.opts.Dial(ctx, m.opts.Addr)
	if err != nil {
		return false, fmt.Errorf("dial: %w", err)
	}

	var closeOnce sync.Once
	closeConn := func() {
		closeOnce.Do(func() {
			_ = conn.Close()
		})
	}
	defer closeConn()

	// Ensure context cancellation unblocks any pending Read/Write immediately.
	defer closeOnCancel(ctx, closeConn)()

	s := &connSession{
		m:         m,
		ctx:       ctx,
		conn:      conn,
		gen:       gen,
		channel:   channel,
		anon:      anon,
		roomState: roomState,
	}
	if err := s.open(); err != nil {
		return false, err
	}

	linesCh := make(chan lineResult, 64)
	readerDone := make(chan struct{})
	defer func() { cancel(); closeConn(); <-readerDone }()
	go func() {
		defer close(readerDone)
		readLines(ctx, conn, s.readTimeout, linesCh)
	}()

	err = s.serve(sendCh, linesCh)
	return s.connectedOnce, err
}
