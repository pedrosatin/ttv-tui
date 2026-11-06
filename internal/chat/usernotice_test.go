package chat

import (
	"io"
	"net"
	"testing"
	"time"
)

// userNoticeLines follow the shape captured from live channels: announcement
// arrives with an empty system-msg and its text only in the trailing parameter.
var userNoticeLines = []struct {
	name      string
	line      string
	msgID     string
	systemMsg string
	body      string
}{
	{
		name:      "raid",
		line:      "@badges=;display-name=Raider;id=n-raid;login=raider;msg-id=raid;msg-param-viewerCount=1234;system-msg=1234\\sraiders\\sfrom\\sRaider\\shave\\sjoined! :tmi.twitch.tv USERNOTICE #gaules",
		msgID:     "raid",
		systemMsg: "1234 raiders from Raider have joined!",
	},
	{
		name:      "sub",
		line:      "@display-name=Nova;id=n-sub;login=nova;msg-id=sub;system-msg=Nova\\ssubscribed\\sat\\sTier\\s1.;user-type= :tmi.twitch.tv USERNOTICE #gaules",
		msgID:     "sub",
		systemMsg: "Nova subscribed at Tier 1.",
	},
	{
		name:      "subgift",
		line:      "@display-name=Santa;id=n-gift;login=santa;msg-id=subgift;msg-param-recipient-display-name=Kid;system-msg=Santa\\sgifted\\sa\\sTier\\s1\\ssub\\sto\\sKid! :tmi.twitch.tv USERNOTICE #gaules",
		msgID:     "subgift",
		systemMsg: "Santa gifted a Tier 1 sub to Kid!",
	},
	{
		name:      "submysterygift",
		line:      "@display-name=Santa;id=n-bomb;login=santa;msg-id=submysterygift;msg-param-mass-gift-count=5;system-msg=Santa\\sis\\sgifting\\s5\\sTier\\s1\\sSubs\\sto\\sGaules's\\scommunity! :tmi.twitch.tv USERNOTICE #gaules",
		msgID:     "submysterygift",
		systemMsg: "Santa is gifting 5 Tier 1 Subs to Gaules's community!",
	},
	{
		name:  "announcement",
		line:  "@badges=moderator/1;display-name=ModOne;id=n-ann;login=modone;mod=1;msg-id=announcement;msg-param-color=PRIMARY;system-msg=;user-type=mod :tmi.twitch.tv USERNOTICE #gaules :Sorteio as 20h",
		msgID: "announcement",
		body:  "Sorteio as 20h",
	},
	{
		name:      "kind this client has never seen",
		line:      "@display-name=Fan;id=n-new;login=fan;msg-id=viewermilestone;system-msg=Fan\\swatched\\s5\\sconsecutive\\sstreams! :tmi.twitch.tv USERNOTICE #gaules :boa",
		msgID:     "viewermilestone",
		systemMsg: "Fan watched 5 consecutive streams!",
		body:      "boa",
	},
	{
		name: "no tags at all",
		line: ":tmi.twitch.tv USERNOTICE #gaules",
	},
}

// startUserNoticeSession joins #gaules against the fake server and writes lines
// once the client is connected, returning the manager and generation to read.
func startUserNoticeSession(t *testing.T, lines ...string) (*Manager, uint64) {
	t.Helper()
	srv := newFakeIRCServer(t)
	connReady := make(chan net.Conn, 1)
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })

	srv.handlers <- func(conn net.Conn) {
		defer func() { _ = conn.Close() }()
		if _, err := readClientLines(conn, 3, 2*time.Second); err != nil {
			return
		}
		connReady <- conn
		<-done
	}

	mgr := New(Options{Addr: srv.Addr(), Dial: srv.Dial, CoalesceInterval: 10 * time.Millisecond})
	t.Cleanup(mgr.Close)
	gen, err := mgr.Join("gaules")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	conn := <-connReady
	waitConnected(t, mgr, gen)
	for _, line := range lines {
		if _, err := io.WriteString(conn, line+"\r\n"); err != nil {
			t.Fatalf("write %q: %v", line, err)
		}
	}
	return mgr, gen
}

func collectMessages(t *testing.T, mgr *Manager, gen uint64, want int) []Message {
	t.Helper()
	var got []Message
	deadline := time.After(3 * time.Second)
	for len(got) < want {
		select {
		case ev := <-mgr.Events():
			if ev.Generation == gen {
				got = append(got, ev.Messages...)
			}
		case <-deadline:
			t.Fatalf("timeout: got %d of %d messages: %+v", len(got), want, got)
		}
	}
	return got
}

func TestManager_EveryUserNoticeBecomesOneSystemMessage(t *testing.T) {
	for _, tc := range userNoticeLines {
		t.Run(tc.name, func(t *testing.T) {
			mgr, gen := startUserNoticeSession(t,
				tc.line,
				"@display-name=Bob;id=after :bob!bob@bob.tmi.twitch.tv PRIVMSG #gaules :depois",
			)
			got := collectMessages(t, mgr, gen, 2)
			if len(got) != 2 {
				t.Fatalf("got %d messages, want the notice and the chat line: %+v", len(got), got)
			}
			notice := got[0]
			if notice.Kind != MessageSystem || !notice.UserNotice {
				t.Fatalf("notice = %+v, want a system line marked as USERNOTICE", notice)
			}
			if notice.MsgID != tc.msgID || notice.SystemMsg != tc.systemMsg || notice.Body != tc.body {
				t.Fatalf("notice = %+v, want msg-id %q, system-msg %q, body %q", notice, tc.msgID, tc.systemMsg, tc.body)
			}
			if got[1].Kind != MessageChat || got[1].UserNotice || got[1].Body != "depois" {
				t.Fatalf("chat line after the notice = %+v", got[1])
			}
		})
	}
}

// A quiet channel has no following line to push the batch out: the raid must
// still arrive on its own.
func TestManager_LoneRaidIsDelivered(t *testing.T) {
	mgr, gen := startUserNoticeSession(t, userNoticeLines[0].line)
	got := collectMessages(t, mgr, gen, 1)
	if got[0].MsgID != "raid" || got[0].Kind != MessageSystem {
		t.Fatalf("got %+v", got[0])
	}
}

func TestManager_UserNoticeFromAnotherChannelIsDropped(t *testing.T) {
	mgr, gen := startUserNoticeSession(t,
		"@display-name=Raider;login=raider;msg-id=raid;system-msg=raiders! :tmi.twitch.tv USERNOTICE #outrocanal",
		"@display-name=Bob;id=after :bob!bob@bob.tmi.twitch.tv PRIVMSG #gaules :depois",
	)
	got := collectMessages(t, mgr, gen, 1)
	if len(got) != 1 || got[0].Kind != MessageChat {
		t.Fatalf("got %+v, want only the chat line", got)
	}
}

func TestByteSizeCountsSystemMsg(t *testing.T) {
	plain := Message{Kind: MessageSystem, UserNotice: true}
	withText := Message{Kind: MessageSystem, UserNotice: true, SystemMsg: "1234 raiders"}
	if got, want := withText.ByteSize()-plain.ByteSize(), len(withText.SystemMsg); got != want {
		t.Fatalf("ByteSize grew by %d, want %d", got, want)
	}
}

func TestClassifyUserNotice(t *testing.T) {
	cases := map[string]UserNoticeKind{
		"sub":                UserNoticeSub,
		"resub":              UserNoticeResub,
		"resubscribe":        UserNoticeResub,
		"subgift":            UserNoticeGift,
		"anonsubgift":        UserNoticeGift,
		"submysterygift":     UserNoticeGift,
		"anonsubmysterygift": UserNoticeGift,
		"raid":               UserNoticeRaid,
		" RAID ":             UserNoticeRaid,
		"announcement":       UserNoticeAnnouncement,
		"unraid":             UserNoticeOther,
		"viewermilestone":    UserNoticeOther,
		"":                   UserNoticeOther,
	}
	for msgID, want := range cases {
		if got := ClassifyUserNotice(msgID); got != want {
			t.Errorf("ClassifyUserNotice(%q) = %q, want %q", msgID, got, want)
		}
	}
}

func TestManager_CommunityGiftTagsReachBuffer(t *testing.T) {
	mgr, gen := startUserNoticeSession(t,
		"@id=header;login=santa;display-name=Santa;msg-id=submysterygift;msg-param-community-gift-id=community;msg-param-mass-gift-count=50 :tmi.twitch.tv USERNOTICE #gaules",
		"@id=child;login=santa;display-name=Santa;msg-id=subgift;msg-param-community-gift-id=community;msg-param-recipient-user-name=alice :tmi.twitch.tv USERNOTICE #gaules")
	msgs := collectMessages(t, mgr, gen, 2)
	if msgs[0].CommunityGiftID != "community" || msgs[0].GiftCount != 50 || msgs[1].GiftRecipient != "alice" {
		t.Fatalf("gift metadata = %+v", msgs)
	}
	b := NewBuffer(0, 0)
	b.SetGeneration(gen)
	for _, msg := range msgs {
		b.Append(msg)
	}
	if b.Len() != 1 || b.Messages()[0].GiftCount != 50 {
		t.Fatalf("summary = %+v", b.Messages())
	}
	b.ClearChat(gen, "santa")
	if !b.Messages()[0].Deleted {
		t.Fatal("donor timeout did not mark group deleted")
	}
	b.SetGeneration(gen + 1)
	if b.Len() != 0 {
		t.Fatal("group survived channel change")
	}
}
