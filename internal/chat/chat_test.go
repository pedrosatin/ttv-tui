package chat

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func TestParseLine_Table(t *testing.T) {
	cases := []struct {
		name     string
		line     string
		wantKind EventKind
		check    func(t *testing.T, ev Event)
	}{
		{
			name:     "PRIVMSG with full tags, color, badges, escaped display-name",
			line:     "@badge-info=subscriber/12;badges=broadcaster/1,subscriber/12;color=#FF4500;display-name=Gaules\\sTribo;emotes=;id=msg-123;mod=1;room-id=181077473;subscriber=1;tmi-sent-ts=1700000000000;user-id=181077473;user-type=mod :gaules!gaules@gaules.tmi.twitch.tv PRIVMSG #gaules :Salve tribo! Bora pro jogo\r\n",
			wantKind: EventPrivMsg,
			check: func(t *testing.T, ev Event) {
				if ev.Channel != "gaules" {
					t.Fatalf("Channel = %q, want gaules", ev.Channel)
				}
				if ev.User != "gaules" {
					t.Fatalf("User = %q, want gaules", ev.User)
				}
				if ev.DisplayName != "Gaules Tribo" {
					t.Fatalf("DisplayName = %q, want 'Gaules Tribo'", ev.DisplayName)
				}
				if ev.Color != "#FF4500" {
					t.Fatalf("Color = %q, want #FF4500", ev.Color)
				}
				wantBadges := []string{"broadcaster/1", "subscriber/12"}
				if !reflect.DeepEqual(ev.Badges, wantBadges) {
					t.Fatalf("Badges = %v, want %v", ev.Badges, wantBadges)
				}
				if ev.ID != "msg-123" {
					t.Fatalf("ID = %q, want msg-123", ev.ID)
				}
				if ev.Highlighted {
					t.Fatalf("Highlighted = true, want false for normal message")
				}
				if ev.Body != "Salve tribo! Bora pro jogo" {
					t.Fatalf("Body = %q", ev.Body)
				}
			},
		},
		{
			name:     "PRIVMSG without tags falls back to prefix nick",
			line:     ":someuser!someuser@someuser.tmi.twitch.tv PRIVMSG #xqc :hello without tags",
			wantKind: EventPrivMsg,
			check: func(t *testing.T, ev Event) {
				if ev.Channel != "xqc" || ev.User != "someuser" || ev.DisplayName != "someuser" {
					t.Fatalf("got ch=%q user=%q display=%q", ev.Channel, ev.User, ev.DisplayName)
				}
				if ev.Color != "" || ev.Highlighted {
					t.Fatalf("expected empty color and not highlighted, got color=%q hl=%v", ev.Color, ev.Highlighted)
				}
				if ev.Body != "hello without tags" {
					t.Fatalf("Body = %q", ev.Body)
				}
			},
		},
		{
			name:     "PRIVMSG highlighted via msg-id=highlighted-message",
			line:     "@badges=vip/1;color=#00FF7F;display-name=VIPUser;id=hl-99;msg-id=highlighted-message :vipuser!vipuser@vipuser.tmi.twitch.tv PRIVMSG #gaules :Mensagem destacada com pontos!",
			wantKind: EventPrivMsg,
			check: func(t *testing.T, ev Event) {
				if !ev.Highlighted {
					t.Fatal("expected Highlighted=true for msg-id=highlighted-message")
				}
				if ev.MsgID != "highlighted-message" {
					t.Fatalf("MsgID = %q, want highlighted-message", ev.MsgID)
				}
				if ev.DisplayName != "VIPUser" || ev.Color != "#00FF7F" {
					t.Fatalf("DisplayName=%q Color=%q", ev.DisplayName, ev.Color)
				}
			},
		},
		{
			name:     "PRIVMSG CTCP ACTION (/me)",
			line:     "@color=#1E90FF;display-name=Streamer :streamer!streamer@streamer.tmi.twitch.tv PRIVMSG #streamer :\x01ACTION acena para o chat\x01",
			wantKind: EventPrivMsg,
			check: func(t *testing.T, ev Event) {
				if !ev.Action {
					t.Fatal("expected Action=true")
				}
				if ev.Body != "acena para o chat" {
					t.Fatalf("Body = %q, want 'acena para o chat'", ev.Body)
				}
			},
		},
		{
			name:     "PING preserves exact payload",
			line:     "PING :tmi.twitch.tv\r\n",
			wantKind: EventPing,
			check: func(t *testing.T, ev Event) {
				if ev.PingPayload != " :tmi.twitch.tv" {
					t.Fatalf("PingPayload = %q, want ' :tmi.twitch.tv'", ev.PingPayload)
				}
				if got := FormatPong(ev.PingPayload); got != "PONG :tmi.twitch.tv\r\n" {
					t.Fatalf("FormatPong = %q, want 'PONG :tmi.twitch.tv\\r\\n'", got)
				}
			},
		},
		{
			name:     "NOTICE slow mode message",
			line:     "@msg-id=slow_on :tmi.twitch.tv NOTICE #gaules :This room is now in slow mode. You may send messages every 30 seconds.",
			wantKind: EventNotice,
			check: func(t *testing.T, ev Event) {
				if ev.Channel != "gaules" || ev.MsgID != "slow_on" {
					t.Fatalf("Channel=%q MsgID=%q", ev.Channel, ev.MsgID)
				}
				if !strings.Contains(ev.Body, "slow mode") {
					t.Fatalf("Body=%q", ev.Body)
				}
			},
		},
		{
			name:     "ROOMSTATE full and partial tags",
			line:     "@emote-only=0;followers-only=10;r9k=1;room-id=181077473;slow=30;subs-only=1 :tmi.twitch.tv ROOMSTATE #gaules",
			wantKind: EventRoomState,
			check: func(t *testing.T, ev Event) {
				if ev.Channel != "gaules" {
					t.Fatalf("Channel=%q", ev.Channel)
				}
				if ev.RoomState.Slow != 30 || ev.RoomState.FollowersOnly != 10 || !ev.RoomState.SubsOnly || !ev.RoomState.R9K || ev.RoomState.EmoteOnly {
					t.Fatalf("unexpected RoomState: %+v", ev.RoomState)
				}
			},
		},
		{
			name:     "RECONNECT command",
			line:     ":tmi.twitch.tv RECONNECT\r\n",
			wantKind: EventReconnect,
			check: func(t *testing.T, ev Event) {
				if ev.Command != "RECONNECT" {
					t.Fatalf("Command=%q", ev.Command)
				}
			},
		},
		{
			name:     "CLEARCHAT user timeout with ban-duration",
			line:     "@ban-duration=600;room-id=181077473;target-user-id=999 :tmi.twitch.tv CLEARCHAT #gaules :baduser",
			wantKind: EventClearChat,
			check: func(t *testing.T, ev Event) {
				if ev.Channel != "gaules" || ev.TargetUser != "baduser" {
					t.Fatalf("Channel=%q TargetUser=%q", ev.Channel, ev.TargetUser)
				}
				if ev.BanDuration == nil || *ev.BanDuration != 600 {
					t.Fatalf("BanDuration=%v, want 600", ev.BanDuration)
				}
			},
		},
		{
			name:     "CLEARCHAT permanent ban without ban-duration",
			line:     "@room-id=181077473;target-user-id=999;tmi-sent-ts=1700000000 :tmi.twitch.tv CLEARCHAT #gaules :spammer",
			wantKind: EventClearChat,
			check: func(t *testing.T, ev Event) {
				if ev.Channel != "gaules" || ev.TargetUser != "spammer" {
					t.Fatalf("Channel=%q TargetUser=%q", ev.Channel, ev.TargetUser)
				}
				if ev.BanDuration != nil {
					t.Fatalf("BanDuration=%v, want nil for permanent ban", ev.BanDuration)
				}
			},
		},
		{
			name:     "CLEARCHAT full room clear without target user",
			line:     "@room-id=181077473;tmi-sent-ts=1700000000 :tmi.twitch.tv CLEARCHAT #gaules",
			wantKind: EventClearChat,
			check: func(t *testing.T, ev Event) {
				if ev.Channel != "gaules" || ev.TargetUser != "" || ev.BanDuration != nil {
					t.Fatalf("Channel=%q TargetUser=%q BanDuration=%v", ev.Channel, ev.TargetUser, ev.BanDuration)
				}
			},
		},
		{
			name:     "CLEARMSG single message removal",
			line:     "@login=baduser;room-id=;target-msg-id=abc-123-def;tmi-sent-ts=1700000000 :tmi.twitch.tv CLEARMSG #gaules :mensagem removida",
			wantKind: EventClearMsg,
			check: func(t *testing.T, ev Event) {
				if ev.Channel != "gaules" || ev.TargetUser != "baduser" || ev.TargetMsgID != "abc-123-def" {
					t.Fatalf("got %+v", ev)
				}
			},
		},
		{
			name:     "USERNOTICE resub with user text",
			line:     "@badge-info=subscriber/15;badges=subscriber/12;color=#00FF7F;display-name=CoolSub;id=resub-with-text;login=coolsub;mod=0;msg-id=resub;msg-param-cumulative-months=15;msg-param-months=0;msg-param-sub-plan=1000;room-id=181077473;subscriber=1;system-msg=CoolSub\\ssubscribed\\sfor\\s15\\smonths!;tmi-sent-ts=1700000000000;user-id=42;user-type= :coolsub!coolsub@coolsub.tmi.twitch.tv USERNOTICE #gaules :Great stream -- keep it up!",
			wantKind: EventUserNotice,
			check: func(t *testing.T, ev Event) {
				if ev.Channel != "gaules" {
					t.Fatalf("Channel=%q", ev.Channel)
				}
				if ev.MsgID != "resub" || ClassifyUserNotice(ev.MsgID) != UserNoticeResub {
					t.Fatalf("MsgID=%q", ev.MsgID)
				}
				if ev.User != "coolsub" || ev.DisplayName != "CoolSub" {
					t.Fatalf("User=%q DisplayName=%q", ev.User, ev.DisplayName)
				}
				if ev.ID != "resub-with-text" || ev.CumulativeMonths != 15 {
					t.Fatalf("ID=%q CumulativeMonths=%d", ev.ID, ev.CumulativeMonths)
				}
				if ev.Body != "Great stream -- keep it up!" {
					t.Fatalf("Body=%q", ev.Body)
				}
				if ev.SystemMsg != "CoolSub subscribed for 15 months!" {
					t.Fatalf("SystemMsg=%q", ev.SystemMsg)
				}
			},
		},
		{
			name:     "USERNOTICE resub without user text",
			line:     "@badge-info=subscriber/6;badges=subscriber/6,premium/1;color=#0000FF;display-name=TWW2;id=resub-no-text;login=tww2;mod=0;msg-id=resub;msg-param-cumulative-months=6;msg-param-months=0;msg-param-sub-plan=Prime;room-id=1337;subscriber=1;system-msg=TWW2\\ssubscribed\\swith\\sPrime.\\sThey've\\ssubscribed\\sfor\\s6\\smonths!;tmi-sent-ts=1507246572675;user-id=1337;user-type= :tmi.twitch.tv USERNOTICE #dallas",
			wantKind: EventUserNotice,
			check: func(t *testing.T, ev Event) {
				if ev.Channel != "dallas" || ev.MsgID != "resub" {
					t.Fatalf("Channel=%q MsgID=%q", ev.Channel, ev.MsgID)
				}
				if ev.User != "tww2" || ev.DisplayName != "TWW2" || ev.CumulativeMonths != 6 {
					t.Fatalf("User=%q DisplayName=%q Months=%d", ev.User, ev.DisplayName, ev.CumulativeMonths)
				}
				if ev.Body != "" {
					t.Fatalf("Body=%q, want empty for resub without message", ev.Body)
				}
			},
		},
		{
			name:     "USERNOTICE raid",
			line:     "@display-name=Raider;login=raider;msg-id=raid;msg-param-viewerCount=5;system-msg=5\\sraiders\\sfrom\\sRaider\\shave\\sjoined! :tmi.twitch.tv USERNOTICE #gaules",
			wantKind: EventUserNotice,
			check: func(t *testing.T, ev Event) {
				if ev.Channel != "gaules" || ev.MsgID != "raid" {
					t.Fatalf("Channel=%q MsgID=%q", ev.Channel, ev.MsgID)
				}
				if ClassifyUserNotice(ev.MsgID) != UserNoticeRaid {
					t.Fatalf("raid classified as %q", ClassifyUserNotice(ev.MsgID))
				}
				if ev.User != "raider" || ev.DisplayName != "Raider" {
					t.Fatalf("User=%q DisplayName=%q", ev.User, ev.DisplayName)
				}
			},
		},
		{
			name:     "Unicode display name and body (CJK, accents, emojis)",
			line:     "@color=#AABBCC;display-name=João_日本語 :joao!joao@joao.tmi.twitch.tv PRIVMSG #gaules :Olá mundo! 🇧🇷🎮 こんにちは :)",
			wantKind: EventPrivMsg,
			check: func(t *testing.T, ev Event) {
				if ev.DisplayName != "João_日本語" {
					t.Fatalf("DisplayName = %q", ev.DisplayName)
				}
				if ev.Body != "Olá mundo! 🇧🇷🎮 こんにちは :)" {
					t.Fatalf("Body = %q", ev.Body)
				}
			},
		},
		{
			name:     "Malformed empty or broken lines do not panic",
			line:     "@onlytags_without_space",
			wantKind: EventMalformed,
			check:    func(t *testing.T, ev Event) {},
		},
		{
			name:     "Malformed PRIVMSG without trailing body",
			line:     ":user!user@user.tmi.twitch.tv PRIVMSG #channel",
			wantKind: EventMalformed,
			check:    func(t *testing.T, ev Event) {},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev := ParseLine(tc.line)
			if ev.Kind != tc.wantKind {
				t.Fatalf("ParseLine(%q) Kind = %q, want %q", tc.line, ev.Kind, tc.wantKind)
			}
			tc.check(t, ev)
		})
	}
}

func TestBuffer_LimitsAndGenerationIsolation(t *testing.T) {
	const (
		maxLines = 200
		maxBytes = 16 * 1024 // 16 KiB
	)
	buf := NewBuffer(maxLines, maxBytes)
	buf.SetGeneration(1)

	// Feed 5,000 messages in a tight loop and verify invariants on every insertion.
	for i := 0; i < 5000; i++ {
		msg := Message{
			Generation:  1,
			Kind:        MessageChat,
			ID:          fmt.Sprintf("id-%d", i),
			User:        "user",
			DisplayName: "User",
			Color:       "#FF0000",
			Body:        fmt.Sprintf("Mensagem de teste número %d com conteúdo extra para ocupar bytes 🎉", i),
		}
		if !buf.Append(msg) {
			t.Fatalf("Append failed at message %d", i)
		}
		if buf.Len() > maxLines {
			t.Fatalf("at %d: Len() = %d exceeds maxLines %d", i, buf.Len(), maxLines)
		}
		if buf.Bytes() > maxBytes {
			t.Fatalf("at %d: Bytes() = %d exceeds maxBytes %d", i, buf.Bytes(), maxBytes)
		}
	}

	msgs := buf.Messages()
	if len(msgs) == 0 {
		t.Fatal("expected non-empty buffer after 5,000 messages")
	}
	if msgs[len(msgs)-1].ID != "id-4999" {
		t.Fatalf("last message ID = %q, want id-4999", msgs[len(msgs)-1].ID)
	}

	// Oversized single message with multi-byte UTF-8 runes must truncate cleanly without invalid UTF-8.
	hugeBody := strings.Repeat("🔥á", maxBytes)
	if !buf.Append(Message{
		Generation:  1,
		Kind:        MessageChat,
		ID:          "huge",
		DisplayName: "tester",
		Body:        hugeBody,
	}) {
		t.Fatal("expected huge message to be truncated and appended")
	}
	if buf.Bytes() > maxBytes {
		t.Fatalf("Bytes() after huge message = %d, want <= %d", buf.Bytes(), maxBytes)
	}
	last := buf.Messages()[buf.Len()-1]
	if !utf8.ValidString(last.Body) {
		t.Fatal("truncated message body is not valid UTF-8")
	}

	// Switch to generation 2: clears gen 1 and rejects late gen 1 messages.
	buf.SetGeneration(2)
	if buf.Len() != 0 || buf.Bytes() != 0 {
		t.Fatalf("expected empty buffer after SetGeneration(2), got Len=%d Bytes=%d", buf.Len(), buf.Bytes())
	}
	if buf.Append(Message{Generation: 1, Kind: MessageChat, DisplayName: "stale", Body: "do not show"}) {
		t.Fatal("stale generation 1 message must be rejected when active generation is 2")
	}
	if buf.Len() != 0 {
		t.Fatalf("Len() = %d after stale append, want 0", buf.Len())
	}
	if !buf.Append(Message{Generation: 2, Kind: MessageChat, ID: "m1", User: "bob", DisplayName: "Bob", Body: "current gen"}) {
		t.Fatal("generation 2 message should be accepted")
	}

	// CLEARCHAT for specific user marks only that user's messages.
	buf.Append(Message{Generation: 2, Kind: MessageChat, ID: "m2", User: "alice", DisplayName: "Alice", Body: "stay"})
	buf.ClearChat(2, "bob")
	remaining := buf.Messages()
	if len(remaining) != 2 || !remaining[0].Deleted || remaining[1].Deleted {
		t.Fatalf("after ClearChat(bob) = %+v, want bob marked and alice untouched", remaining)
	}

	// CLEARMSG by message ID marks that specific message.
	buf.ClearMsg(2, "m2")
	if remaining = buf.Messages(); len(remaining) != 2 || !remaining[1].Deleted {
		t.Fatalf("after ClearMsg(m2) = %+v, want m2 marked", remaining)
	}

	// CLEARMSG for an ID already outside the buffer is a no-op (does not panic).
	buf.Append(Message{Generation: 2, Kind: MessageChat, ID: "keep", User: "carol", DisplayName: "Carol", Body: "hi"})
	before := buf.Len()
	buf.ClearMsg(2, "already-gone")
	if buf.Len() != before {
		t.Fatalf("ClearMsg on missing ID changed Len %d -> %d", before, buf.Len())
	}
}

func TestBuffer_ClearMsgMissingIDIsSafe(t *testing.T) {
	buf := NewBuffer(10, 1024)
	buf.SetGeneration(1)
	buf.Append(Message{Generation: 1, Kind: MessageChat, ID: "a", User: "u", DisplayName: "U", Body: "one"})
	buf.ClearMsg(1, "missing-id")
	msgs := buf.Messages()
	if len(msgs) != 1 || msgs[0].ID != "a" {
		t.Fatalf("buffer mutated by missing ClearMsg: %+v", msgs)
	}
	buf.ClearMsg(1, "")
	if buf.Len() != 1 {
		t.Fatalf("empty ClearMsgID should be ignored, Len=%d", buf.Len())
	}
}

// fakeIRCServer runs an in-process TCP listener for deterministic IRC protocol tests.
type fakeIRCServer struct {
	t        *testing.T
	ln       net.Listener
	mu       sync.Mutex
	conns    []net.Conn
	handlers chan func(net.Conn)
}

func newFakeIRCServer(t *testing.T) *fakeIRCServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	s := &fakeIRCServer{
		t:        t,
		ln:       ln,
		handlers: make(chan func(net.Conn), 16),
	}
	go s.acceptLoop()
	t.Cleanup(s.Close)
	return s
}

func (s *fakeIRCServer) Addr() string {
	return s.ln.Addr().String()
}

func (s *fakeIRCServer) Dial(ctx context.Context, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "tcp", addr)
}

func (s *fakeIRCServer) acceptLoop() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns = append(s.conns, conn)
		s.mu.Unlock()

		select {
		case h := <-s.handlers:
			go h(conn)
		default:
			_ = conn.Close()
		}
	}
}

func (s *fakeIRCServer) Close() {
	_ = s.ln.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.conns {
		_ = c.Close()
	}
}

func readClientLines(conn net.Conn, n int, timeout time.Duration) ([]string, error) {
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	r := bufio.NewReader(conn)
	var out []string
	for len(out) < n {
		line, err := r.ReadString('\n')
		if err != nil {
			return out, err
		}
		out = append(out, line)
	}
	return out, nil
}

func TestManager_AnonymousHandshakeAndExactPong(t *testing.T) {
	srv := newFakeIRCServer(t)

	pongCh := make(chan string, 1)
	handshakeCh := make(chan []string, 1)

	srv.handlers <- func(conn net.Conn) {
		defer func() { _ = conn.Close() }()
		// Anonymous mode sends 3 lines: CAP REQ, NICK justinfan..., JOIN #channel
		lines, err := readClientLines(conn, 3, 2*time.Second)
		if err != nil {
			t.Errorf("read handshake: %v", err)
			return
		}
		handshakeCh <- lines

		// Send PING with custom payload and a PRIVMSG.
		_, _ = io.WriteString(conn, "PING :tmi.twitch.tv/custom-payload-987\r\n")
		_, _ = io.WriteString(conn, "@color=#123456;display-name=Tester;msg-id=highlighted-message :tester!tester@tester.tmi.twitch.tv PRIVMSG #gaules :Olá chat!\r\n")

		pongLine, err := readClientLines(conn, 1, 2*time.Second)
		if err == nil && len(pongLine) == 1 {
			pongCh <- pongLine[0]
		}
		<-time.After(2 * time.Second)
	}

	mgr := New(Options{
		Addr:             srv.Addr(),
		Dial:             srv.Dial,
		CoalesceInterval: 10 * time.Millisecond,
	})
	defer mgr.Close()

	gen, err := mgr.Join("Gaules")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	if gen != 1 {
		t.Fatalf("gen = %d, want 1", gen)
	}

	select {
	case lines := <-handshakeCh:
		if lines[0] != "CAP REQ :twitch.tv/tags twitch.tv/commands\r\n" {
			t.Fatalf("line[0] = %q", lines[0])
		}
		if !strings.HasPrefix(lines[1], "NICK justinfan") || !strings.HasSuffix(lines[1], "\r\n") {
			t.Fatalf("expected anonymous NICK justinfan..., got %q", lines[1])
		}
		if lines[2] != "JOIN #gaules\r\n" {
			t.Fatalf("line[2] = %q, want 'JOIN #gaules\\r\\n'", lines[2])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for anonymous handshake")
	}

	// Verify byte-for-byte PONG reply.
	select {
	case pong := <-pongCh:
		want := "PONG :tmi.twitch.tv/custom-payload-987\r\n"
		if pong != want {
			t.Fatalf("PONG mismatch:\ngot:  %q\nwant: %q", pong, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for PONG")
	}

	// Verify coalesced message arrives with generation 1.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-mgr.Events():
			if len(ev.Messages) > 0 {
				m := ev.Messages[0]
				if m.Generation != 1 || m.DisplayName != "Tester" || m.Color != "#123456" || !m.Highlighted || m.Body != "Olá chat!" {
					t.Fatalf("unexpected message: %+v", m)
				}
				return
			}
		case <-deadline:
			t.Fatal("timeout waiting for chat message event")
		}
	}
}

func TestManager_AuthenticatedHandshake(t *testing.T) {
	srv := newFakeIRCServer(t)
	handshakeCh := make(chan []string, 1)

	srv.handlers <- func(conn net.Conn) {
		defer func() { _ = conn.Close() }()
		// Authenticated mode sends 4 lines: CAP REQ, PASS oauth:..., NICK <user>, JOIN #<channel>
		lines, err := readClientLines(conn, 4, 2*time.Second)
		if err == nil {
			handshakeCh <- lines
		}
		<-time.After(2 * time.Second)
	}

	mgr := New(Options{
		Addr:     srv.Addr(),
		Dial:     srv.Dial,
		Token:    "oauth:secret_tok_123",
		Username: "PedroSatin",
	})
	defer mgr.Close()

	if _, err := mgr.Join("#alanzoka"); err != nil {
		t.Fatalf("Join: %v", err)
	}

	select {
	case lines := <-handshakeCh:
		want := []string{
			"CAP REQ :twitch.tv/tags twitch.tv/commands\r\n",
			"PASS oauth:secret_tok_123\r\n",
			"NICK pedrosatin\r\n",
			"JOIN #alanzoka\r\n",
		}
		if !reflect.DeepEqual(lines, want) {
			t.Fatalf("handshake lines:\ngot:  %#v\nwant: %#v", lines, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for authenticated handshake")
	}
}

func TestManager_ReconnectOnDisconnectAndServerReconnectAndCancelOnSwitch(t *testing.T) {
	srv := newFakeIRCServer(t)

	conn1Joined := make(chan string, 1)
	conn2Joined := make(chan string, 1)
	conn3Joined := make(chan string, 1)

	// Connection 1: joins #chanA, sends RECONNECT to trigger immediate backoff & reconnect.
	srv.handlers <- func(conn net.Conn) {
		defer func() { _ = conn.Close() }()
		lines, err := readClientLines(conn, 3, 2*time.Second)
		if err == nil {
			conn1Joined <- lines[2]
		}
		_, _ = io.WriteString(conn, ":tmi.twitch.tv RECONNECT\r\n")
	}

	// Connection 2: reconnects to #chanA, sends 001 welcome so connectedOnce=true,
	// then drops connection (EOF) to trigger a backoff sleep.
	srv.handlers <- func(conn net.Conn) {
		defer func() { _ = conn.Close() }()
		lines, err := readClientLines(conn, 3, 2*time.Second)
		if err == nil {
			conn2Joined <- lines[2]
		}
		_, _ = io.WriteString(conn, ":tmi.twitch.tv 001 justinfan123 :Welcome, GLHF!\r\n")
		time.Sleep(15 * time.Millisecond)
		// Close to cause EOF and enter backoff sleep.
	}

	// Connection 3: after user switches channel to #chanB while #chanA is in backoff,
	// manager cancels #chanA's backoff and immediately connects to #chanB.
	srv.handlers <- func(conn net.Conn) {
		defer func() { _ = conn.Close() }()
		lines, err := readClientLines(conn, 3, 2*time.Second)
		if err == nil {
			conn3Joined <- lines[2]
		}
		_, _ = io.WriteString(conn, ":bob!bob@bob.tmi.twitch.tv PRIVMSG #chanb :welcome to B\r\n")
		<-time.After(2 * time.Second)
	}

	mgr := New(Options{
		Addr:             srv.Addr(),
		Dial:             srv.Dial,
		CoalesceInterval: 10 * time.Millisecond,
		InitialBackoff:   40 * time.Millisecond,
		MaxBackoff:       500 * time.Millisecond,
	})
	defer mgr.Close()

	gen1, err := mgr.Join("chanA")
	if err != nil || gen1 != 1 {
		t.Fatalf("Join(chanA) = %d, %v", gen1, err)
	}

	select {
	case j := <-conn1Joined:
		if j != "JOIN #chana\r\n" {
			t.Fatalf("conn1 join = %q", j)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for conn1")
	}

	// Wait for automatic reconnect to #chana (conn2).
	select {
	case j := <-conn2Joined:
		if j != "JOIN #chana\r\n" {
			t.Fatalf("conn2 join = %q", j)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for conn2 reconnect")
	}

	// Wait until the second StateReconnecting is emitted for generation 1 (after conn2 EOF).
	reconnectEvents := 0
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-mgr.Events():
			if ev.Generation == 1 && ev.State == StateReconnecting {
				reconnectEvents++
				if reconnectEvents >= 2 {
					goto switchChannel
				}
			}
		case <-deadline:
			t.Fatal("timeout waiting for second StateReconnecting")
		}
	}

switchChannel:
	// Switch to chanB while chanA is waiting in backoff: cancels chanA immediately and connects chanB.
	gen2, err := mgr.Join("chanB")
	if err != nil || gen2 != 2 {
		t.Fatalf("Join(chanB) = %d, %v", gen2, err)
	}

	select {
	case j := <-conn3Joined:
		if j != "JOIN #chanb\r\n" {
			t.Fatalf("conn3 join = %q, want JOIN #chanb", j)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for conn3 (#chanb)")
	}

	// Verify no generation 1 events arrive after switching to generation 2, and chanB message arrives.
	deadline = time.After(2 * time.Second)
	for {
		select {
		case ev := <-mgr.Events():
			if ev.Generation < gen2 {
				t.Fatalf("received stale generation %d event after switching to generation %d: %+v", ev.Generation, gen2, ev)
			}
			if len(ev.Messages) > 0 && ev.Messages[0].Body == "welcome to B" {
				return
			}
		case <-deadline:
			t.Fatal("timeout waiting for chanB message")
		}
	}
}

type fakeClock struct {
	mu      sync.Mutex
	current time.Time
	waiters []clockWaiter
}

type clockWaiter struct {
	deadline time.Time
	ch       chan time.Time
}

func newFakeClock(start time.Time) *fakeClock {
	return &fakeClock{current: start}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- c.current
		return ch
	}
	c.waiters = append(c.waiters, clockWaiter{
		deadline: c.current.Add(d),
		ch:       ch,
	})
	return ch
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.current = c.current.Add(d)
	now := c.current
	var remaining []clockWaiter
	for _, w := range c.waiters {
		if !w.deadline.After(now) {
			w.ch <- now
		} else {
			remaining = append(remaining, w)
		}
	}
	c.waiters = remaining
	c.mu.Unlock()
}

func (c *fakeClock) WaiterCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.waiters)
}

func TestRateLimiter_Enforces1PerSecondAnd20Per30SecondsAndDiscardsExcess(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 11, 4, 1, 0, 0, 0, time.UTC))
	rl := newRateLimiter(clk.Now)

	// First message in #gaules is allowed immediately.
	wait, allowed, windowFull := rl.DelayUntilReady("gaules")
	if !allowed || windowFull || wait != 0 {
		t.Fatalf("first message: wait=%v allowed=%v windowFull=%v", wait, allowed, windowFull)
	}
	rl.Record("gaules")

	// Second message 200ms later in #gaules must wait 800ms (1 msg/s per channel).
	clk.Advance(200 * time.Millisecond)
	wait, allowed, windowFull = rl.DelayUntilReady("gaules")
	if allowed || windowFull || wait != 800*time.Millisecond {
		t.Fatalf("after 200ms: wait=%v allowed=%v windowFull=%v, want 800ms/false/false", wait, allowed, windowFull)
	}

	// Advancing remaining 800ms makes it ready.
	clk.Advance(800 * time.Millisecond)
	wait, allowed, windowFull = rl.DelayUntilReady("gaules")
	if !allowed || windowFull || wait != 0 {
		t.Fatalf("after 1s: wait=%v allowed=%v windowFull=%v", wait, allowed, windowFull)
	}
	rl.Record("gaules")

	// Send 18 more messages at 1s intervals (total 20 messages within 19 seconds < 30s window).
	for i := 2; i < RateLimitWindowCount; i++ {
		clk.Advance(time.Second)
		wait, allowed, windowFull = rl.DelayUntilReady("gaules")
		if !allowed || windowFull || wait != 0 {
			t.Fatalf("msg #%d: wait=%v allowed=%v windowFull=%v", i+1, wait, allowed, windowFull)
		}
		rl.Record("gaules")
	}

	// 21st message within the 30s window must be rejected (windowFull=true, NextAllowed ok=false).
	clk.Advance(time.Second) // t = 20s since first message
	if _, ok := rl.NextAllowed("gaules", 0); ok {
		t.Fatal("expected 21st message within 30s window to be rejected by NextAllowed")
	}
	_, allowed, windowFull = rl.DelayUntilReady("gaules")
	if allowed || !windowFull {
		t.Fatalf("expected windowFull=true on 21st message, got allowed=%v windowFull=%v", allowed, windowFull)
	}

	// Advance past 30s from the first message (t = 30s + 1ms): window frees 1 slot.
	clk.Advance(10*time.Second + time.Millisecond)
	if _, ok := rl.NextAllowed("gaules", 0); !ok {
		t.Fatal("expected slot to open after 30s window elapsed")
	}
}

func TestManager_SendRateLimitQueueAndExcessDiscardWithFakeIRC(t *testing.T) {
	srv := newFakeIRCServer(t)
	clk := newFakeClock(time.Date(2026, 11, 4, 1, 0, 0, 0, time.UTC))

	privmsgCh := make(chan string, 32)
	handshakeDone := make(chan struct{})

	srv.handlers <- func(conn net.Conn) {
		defer func() { _ = conn.Close() }()
		if _, err := readClientLines(conn, 4, 2*time.Second); err != nil {
			t.Errorf("handshake read: %v", err)
			return
		}
		close(handshakeDone)

		r := bufio.NewReader(conn)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			if strings.Contains(line, "PRIVMSG") {
				privmsgCh <- line
			}
		}
	}

	mgr := New(Options{
		Addr:             srv.Addr(),
		Dial:             srv.Dial,
		Token:            "oauth:valid_token_abc",
		Username:         "pedrosatin",
		CoalesceInterval: 10 * time.Millisecond,
		Now:              clk.Now,
		After:            clk.After,
	})
	defer mgr.Close()

	gen, err := mgr.Join("gaules")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	<-handshakeDone

	// Wait until StateConnected is emitted.
	waitConnected(t, mgr, gen)

	// Enqueue 20 messages rapidly at t=0:
	// - Msg 1 is sent immediately over the wire.
	// - Msgs 2..20 are queued (19 pending, within the 20/30s budget).
	for i := 1; i <= 20; i++ {
		upd, err := mgr.Send(SendRequest{
			Generation: gen,
			Channel:    "gaules",
			Nonce:      fmt.Sprintf("n-%d", i),
			Body:       fmt.Sprintf("msg-%d", i),
		})
		if err != nil {
			t.Fatalf("Send #%d unexpected error: %v", i, err)
		}
		if upd.Status != DeliverySending {
			t.Fatalf("Send #%d status = %q, want %q", i, upd.Status, DeliverySending)
		}
	}

	// 21st message within the 30s window exceeds 20/30s and must be discarded immediately with RejectRateLimited.
	upd21, err21 := mgr.Send(SendRequest{
		Generation: gen,
		Channel:    "gaules",
		Nonce:      "n-21",
		Body:       "msg-21-excess",
	})
	if !errors.Is(err21, ErrRateLimited) {
		t.Fatalf("Send #21 err = %v, want ErrRateLimited", err21)
	}
	if upd21.Status != DeliveryDiscarded || upd21.Reason != RejectRateLimited {
		t.Fatalf("Send #21 update = %+v, want discarded/rate_limit", upd21)
	}

	// Only msg-1 should have been written to the wire at t=0 (1 msg/s per channel).
	select {
	case line := <-privmsgCh:
		if !strings.Contains(line, "PRIVMSG #gaules :msg-1\r\n") {
			t.Fatalf("first wire line = %q", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for msg-1 on wire")
	}

	select {
	case extra := <-privmsgCh:
		t.Fatalf("unexpected second PRIVMSG before 1s elapsed: %q", extra)
	case <-time.After(30 * time.Millisecond):
	}

	// Advance fake clock by 1s -> msg-2 is released.
	waitForTimerRegistration(t, clk)
	clk.Advance(time.Second)

	select {
	case line := <-privmsgCh:
		if !strings.Contains(line, "PRIVMSG #gaules :msg-2\r\n") {
			t.Fatalf("second wire line = %q, want msg-2", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for msg-2 after advancing 1s")
	}
}

func TestManager_ChannelSwitchDiscardsPendingAndNeverSendsToNewChannel(t *testing.T) {
	srv := newFakeIRCServer(t)
	clk := newFakeClock(time.Date(2026, 11, 4, 1, 0, 0, 0, time.UTC))

	chanALines := make(chan string, 16)
	chanBLines := make(chan string, 16)
	chanAReady := make(chan struct{})
	chanBReady := make(chan struct{})

	// Handler 1: #gaules
	srv.handlers <- func(conn net.Conn) {
		defer func() { _ = conn.Close() }()
		if _, err := readClientLines(conn, 4, 2*time.Second); err != nil {
			return
		}
		close(chanAReady)
		r := bufio.NewReader(conn)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			if strings.Contains(line, "PRIVMSG") {
				chanALines <- line
			}
		}
	}

	// Handler 2: #alanzoka
	srv.handlers <- func(conn net.Conn) {
		defer func() { _ = conn.Close() }()
		if _, err := readClientLines(conn, 4, 2*time.Second); err != nil {
			return
		}
		close(chanBReady)
		r := bufio.NewReader(conn)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			if strings.Contains(line, "PRIVMSG") {
				chanBLines <- line
			}
		}
	}

	mgr := New(Options{
		Addr:             srv.Addr(),
		Dial:             srv.Dial,
		Token:            "oauth:tok_switch",
		Username:         "pedrosatin",
		CoalesceInterval: 10 * time.Millisecond,
		Now:              clk.Now,
		After:            clk.After,
	})
	defer mgr.Close()

	gen1, err := mgr.Join("gaules")
	if err != nil {
		t.Fatalf("Join(gaules): %v", err)
	}
	<-chanAReady
	waitConnected(t, mgr, gen1)

	// Send msg 1 (transmitted immediately to #gaules) and msg 2 & 3 (queued waiting for 1s rate limit).
	if _, err := mgr.Send(SendRequest{Generation: gen1, Channel: "gaules", Nonce: "a1", Body: "hello gaules 1"}); err != nil {
		t.Fatalf("Send a1: %v", err)
	}
	if _, err := mgr.Send(SendRequest{Generation: gen1, Channel: "gaules", Nonce: "a2", Body: "queued for gaules 2"}); err != nil {
		t.Fatalf("Send a2: %v", err)
	}
	if _, err := mgr.Send(SendRequest{Generation: gen1, Channel: "gaules", Nonce: "a3", Body: "queued for gaules 3"}); err != nil {
		t.Fatalf("Send a3: %v", err)
	}

	// Wait for msg 1 to arrive on #gaules and msg 2 to be queued on timer.
	select {
	case l := <-chanALines:
		if !strings.Contains(l, "PRIVMSG #gaules :hello gaules 1") {
			t.Fatalf("unexpected line on chanA: %q", l)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for a1 on chanA")
	}
	waitForTimerRegistration(t, clk)

	// Switch to #alanzoka before the 1s timer fires!
	gen2, err := mgr.Join("alanzoka")
	if err != nil {
		t.Fatalf("Join(alanzoka): %v", err)
	}
	<-chanBReady

	// Also attempt to send a late SendRequest still carrying gen1/#gaules after switching to #alanzoka.
	staleUpd, staleErr := mgr.Send(SendRequest{
		Generation: gen1,
		Channel:    "gaules",
		Nonce:      "a4-stale",
		Body:       "composed on gaules before switch",
	})
	if !errors.Is(staleErr, ErrChannelSwitched) {
		t.Fatalf("stale Send err = %v, want ErrChannelSwitched", staleErr)
	}
	if staleUpd.Status != DeliveryDiscarded || staleUpd.Reason != RejectChannelSwitched {
		t.Fatalf("stale Send upd = %+v, want discarded/channel_switched", staleUpd)
	}

	// Advance clock by 5 seconds and send a valid message on #alanzoka (gen2).
	clk.Advance(5 * time.Second)
	if _, err := mgr.Send(SendRequest{Generation: gen2, Channel: "alanzoka", Nonce: "b1", Body: "oi alanzoka"}); err != nil {
		t.Fatalf("Send b1: %v", err)
	}

	select {
	case l := <-chanBLines:
		if !strings.Contains(l, "PRIVMSG #alanzoka :oi alanzoka") {
			t.Fatalf("chanB received wrong PRIVMSG: %q (must NEVER receive gaules messages)", l)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for b1 on chanB")
	}

	// Verify no further PRIVMSG was sent to #alanzoka or #gaules.
	select {
	case leaked := <-chanBLines:
		t.Fatalf("leaked PRIVMSG to #alanzoka: %q", leaked)
	case leaked := <-chanALines:
		t.Fatalf("leaked PRIVMSG to #gaules after switch: %q", leaked)
	case <-time.After(40 * time.Millisecond):
	}
}

func TestManager_ConfirmAndNoticeRejectReasons(t *testing.T) {
	srv := newFakeIRCServer(t)
	clk := newFakeClock(time.Date(2026, 11, 4, 1, 0, 0, 0, time.UTC))

	clientLines := make(chan string, 16)
	var serverConn net.Conn
	connReady := make(chan struct{})

	srv.handlers <- func(conn net.Conn) {
		defer func() { _ = conn.Close() }()
		if _, err := readClientLines(conn, 4, 2*time.Second); err != nil {
			return
		}
		serverConn = conn
		close(connReady)

		r := bufio.NewReader(conn)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			if strings.Contains(line, "PRIVMSG") {
				clientLines <- line
			}
		}
	}

	mgr := New(Options{
		Addr:             srv.Addr(),
		Dial:             srv.Dial,
		Token:            "oauth:tok_confirm",
		Username:         "pedrosatin",
		CoalesceInterval: 10 * time.Millisecond,
		Now:              clk.Now,
		After:            clk.After,
	})
	defer mgr.Close()

	gen, err := mgr.Join("gaules")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	<-connReady
	waitConnected(t, mgr, gen)

	// 1. Send message and confirm via USERSTATE echo with client-nonce.
	if _, err := mgr.Send(SendRequest{Generation: gen, Channel: "gaules", Nonce: "nonce-ok", Body: "vai brasil"}); err != nil {
		t.Fatalf("Send 1: %v", err)
	}
	<-clientLines
	_, _ = io.WriteString(serverConn, "@client-nonce=nonce-ok;color=#00FF00;display-name=PedroSatin :tmi.twitch.tv USERSTATE #gaules\r\n")

	upd := waitSendUpdate(t, mgr, "nonce-ok")
	if upd.Status != DeliveryConfirmed {
		t.Fatalf("nonce-ok status = %q, want %q", upd.Status, DeliveryConfirmed)
	}

	// 2. Test rejection NOTICE reasons: slow mode, followers-only, subscribers-only, invalid login.
	rejectCases := []struct {
		nonce      string
		noticeLine string
		wantReason RejectReason
	}{
		{
			nonce:      "nonce-slow",
			noticeLine: "@msg-id=msg_slowmode :tmi.twitch.tv NOTICE #gaules :This room is in slow mode and you are sending messages too quickly.\r\n",
			wantReason: RejectSlowMode,
		},
		{
			nonce:      "nonce-follow",
			noticeLine: "@msg-id=msg_followersonly :tmi.twitch.tv NOTICE #gaules :This room is in 10 minutes followers-only mode.\r\n",
			wantReason: RejectFollowersOnly,
		},
		{
			nonce:      "nonce-subs",
			noticeLine: "@msg-id=msg_subsonly :tmi.twitch.tv NOTICE #gaules :This room is in subscribers-only mode.\r\n",
			wantReason: RejectSubsOnly,
		},
		{
			nonce:      "nonce-auth",
			noticeLine: ":tmi.twitch.tv NOTICE * :Login authentication failed\r\n",
			wantReason: RejectUnauthenticated,
		},
	}

	for _, tc := range rejectCases {
		clk.Advance(2 * time.Second)
		if _, err := mgr.Send(SendRequest{Generation: gen, Channel: "gaules", Nonce: tc.nonce, Body: "try " + tc.nonce}); err != nil {
			t.Fatalf("Send(%s): %v", tc.nonce, err)
		}
		<-clientLines
		_, _ = io.WriteString(serverConn, tc.noticeLine)

		got := waitSendUpdate(t, mgr, tc.nonce)
		if got.Status != DeliveryRejected || got.Reason != tc.wantReason {
			t.Fatalf("case %s: got status=%q reason=%q, want rejected/%q", tc.nonce, got.Status, got.Reason, tc.wantReason)
		}
	}
}

func TestManager_SendWithoutTokenAndEmptyMessageBlocked(t *testing.T) {
	mgr := New(Options{
		Token: "", // anonymous
	})
	defer mgr.Close()

	// Empty message is rejected with ErrEmptyMessage.
	if _, err := mgr.Send(SendRequest{Body: "   \t\n  "}); !errors.Is(err, ErrEmptyMessage) {
		t.Fatalf("empty Send err = %v, want ErrEmptyMessage", err)
	}

	// Non-empty message without token is rejected with ErrNoToken and RejectUnauthenticated.
	upd, err := mgr.Send(SendRequest{Body: "hello"})
	if !errors.Is(err, ErrNoToken) {
		t.Fatalf("no-token Send err = %v, want ErrNoToken", err)
	}
	if upd.Status != DeliveryRejected || upd.Reason != RejectUnauthenticated {
		t.Fatalf("no-token Send upd = %+v, want rejected/unauthenticated", upd)
	}
}

func waitConnected(t *testing.T, mgr *Manager, gen uint64) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-mgr.Events():
			if ev.Generation == gen && ev.State == StateConnected {
				return
			}
		case <-deadline:
			t.Fatalf("timeout waiting for StateConnected (gen=%d)", gen)
		}
	}
}

func waitSendUpdate(t *testing.T, mgr *Manager, nonce string) SendStatusUpdate {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-mgr.Events():
			for _, u := range ev.SendUpdates {
				if u.Nonce == nonce {
					return u
				}
			}
		case <-deadline:
			t.Fatalf("timeout waiting for SendStatusUpdate nonce=%q", nonce)
		}
	}
}

func waitForTimerRegistration(t *testing.T, clk *fakeClock) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if clk.WaiterCount() > 0 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("timeout waiting for rate limiter timer registration")
}

func TestManager_RoomStateInitialAndIncrementalMerge(t *testing.T) {
	srv := newFakeIRCServer(t)
	var serverConn net.Conn
	connReady := make(chan struct{})

	srv.handlers <- func(conn net.Conn) {
		defer func() { _ = conn.Close() }()
		if _, err := readClientLines(conn, 3, 2*time.Second); err != nil {
			return
		}
		serverConn = conn
		close(connReady)
		<-time.After(2 * time.Second)
	}

	mgr := New(Options{
		Addr:             srv.Addr(),
		Dial:             srv.Dial,
		CoalesceInterval: 10 * time.Millisecond,
	})
	defer mgr.Close()

	gen, err := mgr.Join("gaules")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	<-connReady
	waitConnected(t, mgr, gen)

	// 1. Initial ROOMSTATE on join sets slow=30, followers-only=10, subs-only=1.
	_, _ = io.WriteString(serverConn, "@emote-only=0;followers-only=10;r9k=0;slow=30;subs-only=1 :tmi.twitch.tv ROOMSTATE #gaules\r\n")

	var rs1 RoomState
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-mgr.Events():
			if ev.Generation == gen && ev.RoomState != nil {
				rs1 = *ev.RoomState
				goto gotRS1
			}
		case <-deadline:
			t.Fatal("timeout waiting for initial RoomState event")
		}
	}
gotRS1:
	if rs1.Slow != 30 || rs1.FollowersOnly != 10 || !rs1.SubsOnly || rs1.EmoteOnly || rs1.R9K {
		t.Fatalf("initial RoomState = %+v, want Slow=30 FollowersOnly=10 SubsOnly=true", rs1)
	}

	// 2. Partial ROOMSTATE update (@slow=0) updates Slow while preserving FollowersOnly=10 and SubsOnly=true.
	_, _ = io.WriteString(serverConn, "@slow=0 :tmi.twitch.tv ROOMSTATE #gaules\r\n")

	var rs2 RoomState
	deadline = time.After(2 * time.Second)
	for {
		select {
		case ev := <-mgr.Events():
			if ev.Generation == gen && ev.RoomState != nil {
				rs2 = *ev.RoomState
				goto gotRS2
			}
		case <-deadline:
			t.Fatal("timeout waiting for partial RoomState delta event")
		}
	}
gotRS2:
	if rs2.Slow != 0 || rs2.FollowersOnly != 10 || !rs2.SubsOnly {
		t.Fatalf("merged RoomState = %+v, want Slow=0 FollowersOnly=10 SubsOnly=true", rs2)
	}
}

func TestManager_UserNoticeResubWithAndWithoutText(t *testing.T) {
	srv := newFakeIRCServer(t)
	var serverConn net.Conn
	connReady := make(chan struct{})

	srv.handlers <- func(conn net.Conn) {
		defer func() { _ = conn.Close() }()
		if _, err := readClientLines(conn, 3, 2*time.Second); err != nil {
			return
		}
		serverConn = conn
		close(connReady)
		<-time.After(3 * time.Second)
	}

	mgr := New(Options{
		Addr:             srv.Addr(),
		Dial:             srv.Dial,
		CoalesceInterval: 10 * time.Millisecond,
	})
	defer mgr.Close()

	gen, err := mgr.Join("gaules")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	<-connReady
	waitConnected(t, mgr, gen)

	_, _ = io.WriteString(serverConn, "@color=#123456;display-name=Alice;id=chat-1 :alice!alice@alice.tmi.twitch.tv PRIVMSG #gaules :hello\r\n")
	_, _ = io.WriteString(serverConn, "@badge-info=subscriber/15;badges=subscriber/12;color=#00FF7F;display-name=CoolSub;id=resub-1;login=coolsub;msg-id=resub;msg-param-cumulative-months=15;system-msg=CoolSub\\ssubscribed\\sfor\\s15\\smonths! :tmi.twitch.tv USERNOTICE #gaules :Great stream!\r\n")
	_, _ = io.WriteString(serverConn, "@badge-info=subscriber/6;badges=subscriber/6;display-name=TWW2;id=resub-2;login=tww2;msg-id=resub;msg-param-cumulative-months=6;system-msg=TWW2\\ssubscribed! :tmi.twitch.tv USERNOTICE #gaules\r\n")
	_, _ = io.WriteString(serverConn, "@display-name=Raider;login=raider;msg-id=raid;system-msg=raiders! :tmi.twitch.tv USERNOTICE #gaules\r\n")
	_, _ = io.WriteString(serverConn, "@color=#ABCDEF;display-name=Bob;id=chat-2 :bob!bob@bob.tmi.twitch.tv PRIVMSG #gaules :after\r\n")

	var got []Message
	deadline := time.After(3 * time.Second)
	for len(got) < 5 {
		select {
		case ev := <-mgr.Events():
			if ev.Generation != gen {
				continue
			}
			got = append(got, ev.Messages...)
		case <-deadline:
			t.Fatalf("timeout waiting for messages; got %d: %+v", len(got), got)
		}
	}

	if len(got) != 5 {
		t.Fatalf("got %d messages, want 5 (2 chat + 2 resub + raid): %+v", len(got), got)
	}
	if got[0].Kind != MessageChat || got[0].Body != "hello" {
		t.Fatalf("msg0 = %+v", got[0])
	}
	if got[1].Kind != MessageSystem || got[1].MsgID != "resub" || got[1].Body != "Great stream!" || got[1].Months != 15 || got[1].DisplayName != "CoolSub" {
		t.Fatalf("resub with text = %+v", got[1])
	}
	if got[2].Kind != MessageSystem || got[2].MsgID != "resub" || got[2].Body != "" || got[2].Months != 6 || got[2].User != "tww2" {
		t.Fatalf("resub without text = %+v", got[2])
	}
	if got[3].Kind != MessageSystem || got[3].MsgID != "raid" || got[3].SystemMsg != "raiders!" {
		t.Fatalf("raid = %+v", got[3])
	}
	if got[4].Kind != MessageChat || got[4].Body != "after" {
		t.Fatalf("msg4 = %+v", got[4])
	}

	// Buffer accounting: system lines count once each, same as chat lines.
	buf := NewBuffer(DefaultMaxLines, DefaultMaxBytes)
	buf.SetGeneration(gen)
	for _, m := range got {
		if !buf.Append(m) {
			t.Fatalf("Append rejected %+v", m)
		}
	}
	if buf.Len() != 5 {
		t.Fatalf("buffer Len=%d, want 5", buf.Len())
	}
}
