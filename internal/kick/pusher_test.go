package kick

import (
	"strings"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
)

// chatFixture returns the frames of testdata/pusher_chat.ndjson: a real
// session with the senders renamed.
func chatFixture(t *testing.T) []string {
	t.Helper()
	var lines []string
	for line := range strings.SplitSeq(string(fixture(t, "pusher_chat.ndjson")), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) < 3 {
		t.Fatalf("fixture has %d frames", len(lines))
	}
	return lines
}

func TestParseFrame_HandshakeFixture(t *testing.T) {
	lines := chatFixture(t)
	f, err := parseFrame(lines[0])
	if err != nil {
		t.Fatalf("parseFrame: %v", err)
	}
	if f.Event != eventEstablished {
		t.Fatalf("event = %q, want %q", f.Event, eventEstablished)
	}
	var est established
	if err := f.payload(&est); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if est.ActivityTimeout != 120 || est.SocketID == "" {
		t.Errorf("established = %+v", est)
	}

	f, err = parseFrame(lines[1])
	if err != nil || f.Event != eventSubscribed || f.Channel != "chatrooms.66973867.v2" {
		t.Errorf("subscription frame = %+v, err %v", f, err)
	}
}

func TestParseChatMessage_Fixture(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	type want struct {
		name   string
		user   string
		body   string
		badges string
	}
	wants := []want{
		{"Viewer01", "viewer01", "@Viewer02 hmmm", ""},
		{"Viewer03", "viewer03", "collectiblesGoldenW", ""},
		{"Viewer04", "viewer04", "gggg", "subscriber"},
		{"Viewer05", "viewer05", "gud", "moderator,founder,sub_gifter"},
		{"Viewer06", "viewer06", "OOOO OH", ""},
		{"Viewer07", "viewer07", "KEKLEO KEKLEO", "subscriber"},
		{"Viewer08", "viewer08", "Acesse a Lojinha e troque seus Gaupoints da Kick! https://gaupoints.com.br/", "broadcaster,sub_gifter,verified"},
		{"Viewer09", "viewer09", "@Viewer02 collectiblesPOGGIES", "subscriber"},
	}
	var got []catalog.ChatMessage
	for _, line := range chatFixture(t) {
		f, err := parseFrame(line)
		if err != nil {
			t.Fatalf("parseFrame: %v", err)
		}
		if f.Event != eventChatMessage {
			continue
		}
		msg, ok, err := parseChatMessage(f, now)
		if err != nil || !ok {
			t.Fatalf("parseChatMessage(%s) = ok %v, err %v", line, ok, err)
		}
		got = append(got, msg)
	}
	if len(got) != len(wants) {
		t.Fatalf("parsed %d messages, want %d", len(got), len(wants))
	}
	for i, w := range wants {
		m := got[i]
		if m.DisplayName != w.name || m.User != w.user || m.Body != w.body || strings.Join(m.Badges, ",") != w.badges {
			t.Errorf("message %d = name %q user %q body %q badges %v, want %+v", i, m.DisplayName, m.User, m.Body, m.Badges, w)
		}
		if m.Kind != catalog.ChatMessageChat || m.ID == "" || !strings.HasPrefix(m.Color, "#") {
			t.Errorf("message %d = kind %q id %q color %q", i, m.Kind, m.ID, m.Color)
		}
		if m.ReceivedAt.Year() != 2026 || m.ReceivedAt.Month() != time.November {
			t.Errorf("message %d received at %v, want the time the server stamped", i, m.ReceivedAt)
		}
	}
}

func TestParseChatMessage_NothingToShow(t *testing.T) {
	now := time.Now()
	for name, data := range map[string]string{
		"empty content":  `{"id":"1","content":"  ","sender":{"username":"a","slug":"a"}}`,
		"no sender":      `{"id":"1","content":"oi","sender":{}}`,
		"nameless emote": `{"id":"1","content":"[emote:1:]","sender":{"username":"a","slug":"a"}}`,
	} {
		f := frame{Event: eventChatMessage, Data: []byte(data)}
		if _, ok, err := parseChatMessage(f, now); ok || err != nil {
			t.Errorf("%s: ok %v err %v, want a skipped message", name, ok, err)
		}
	}
	bad := frame{Event: eventChatMessage, Data: []byte(`"not json"`)}
	if _, _, err := parseChatMessage(bad, now); err == nil {
		t.Error("undecodable data: want an error")
	}
	// A time the client cannot read falls back to the clock.
	f := frame{Event: eventChatMessage, Data: []byte(`{"id":"1","content":"oi","created_at":"ontem","sender":{"username":"A"}}`)}
	msg, ok, err := parseChatMessage(f, now)
	if !ok || err != nil || !msg.ReceivedAt.Equal(now) || msg.User != "a" {
		t.Errorf("fallbacks: msg %+v ok %v err %v", msg, ok, err)
	}
}

func TestServerError_BadKeyFixture(t *testing.T) {
	f, err := parseFrame(strings.TrimSpace(string(fixture(t, "pusher_error_bad_key.json"))))
	if err != nil {
		t.Fatalf("parseFrame: %v", err)
	}
	got := serverError(f)
	if reason := catalog.ChatErrorReasonOf(got); reason != catalog.ChatErrorKeyRefused {
		t.Fatalf("reason = %q, want key_refused (err %v)", reason, got)
	}
	if !strings.Contains(got.Error(), "4001") {
		t.Errorf("error %q does not carry the code", got)
	}
}

func TestPusherError_Classes(t *testing.T) {
	tests := []struct {
		code      int
		permanent bool
	}{{4000, true}, {4001, true}, {4099, true}, {4100, false}, {4200, false}, {4201, false}}
	for _, tc := range tests {
		if got := (pusherError{Code: tc.code}).permanent(); got != tc.permanent {
			t.Errorf("code %d permanent = %v, want %v", tc.code, got, tc.permanent)
		}
	}
}

func TestParseFrame_Rejects(t *testing.T) {
	for _, text := range []string{"", "nope", `{"data":"{}"}`} {
		if _, err := parseFrame(text); err == nil {
			t.Errorf("parseFrame(%q): want an error", text)
		}
	}
}
