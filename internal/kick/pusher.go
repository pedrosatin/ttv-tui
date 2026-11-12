package kick

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
)

// Pusher protocol 7 events the chat exchanges, and the Kick events it reads.
const (
	eventEstablished  = "pusher:connection_established"
	eventError        = "pusher:error"
	eventPing         = "pusher:ping"
	eventPong         = "pusher:pong"
	eventSubscribe    = "pusher:subscribe"
	eventSubscribed   = "pusher_internal:subscription_succeeded"
	eventSubscribeErr = "pusher:subscription_error"
	eventChatMessage  = `App\Events\ChatMessageEvent`
)

// frame is one Pusher message. Data is a JSON document inside a string in most
// events and a plain object in pusher:error.
type frame struct {
	Event   string          `json:"event"`
	Data    json.RawMessage `json:"data"`
	Channel string          `json:"channel"`
}

func parseFrame(text string) (frame, error) {
	var f frame
	if err := json.Unmarshal([]byte(text), &f); err != nil {
		return frame{}, fmt.Errorf("pusher: decode frame: %w", err)
	}
	if f.Event == "" {
		return frame{}, errors.New("pusher: frame without event")
	}
	return f, nil
}

// payload decodes the data of f into v, whether it came as an object or as a
// JSON document inside a string.
func (f frame) payload(v any) error {
	raw := []byte(f.Data)
	if len(raw) > 0 && raw[0] == '"' {
		var inner string
		if err := json.Unmarshal(raw, &inner); err != nil {
			return fmt.Errorf("pusher: decode %s data: %w", f.Event, err)
		}
		raw = []byte(inner)
	}
	if len(raw) == 0 {
		return fmt.Errorf("pusher: %s without data", f.Event)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("pusher: decode %s data: %w", f.Event, err)
	}
	return nil
}

// established is the data of pusher:connection_established. ActivityTimeout is
// in seconds: after that long without a message the client pings.
type established struct {
	SocketID        string `json:"socket_id"`
	ActivityTimeout int    `json:"activity_timeout"`
}

// pusherError is the data of pusher:error. Codes 4000-4099 mean the connection
// must not be retried unchanged, 4100-4199 retry after a wait and 4200-4299
// retry at once.
type pusherError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e pusherError) Error() string {
	return fmt.Sprintf("pusher: error %d: %s", e.Code, strings.TrimSpace(e.Message))
}

// pusherAppMissing is the code for a key no application answers to.
const pusherAppMissing = 4001

// permanent reports an error the same connection would get again.
func (e pusherError) permanent() bool {
	return e.Code >= 4000 && e.Code <= 4099
}

func subscribeFrame(channel string) string {
	b, _ := json.Marshal(map[string]any{
		"event": eventSubscribe,
		"data":  map[string]string{"auth": "", "channel": channel},
	})
	return string(b)
}

func controlFrame(event string) string {
	b, _ := json.Marshal(map[string]any{"event": event, "data": map[string]string{}})
	return string(b)
}

// chatMessage is the data of a ChatMessageEvent.
type chatMessage struct {
	ID        string `json:"id"`
	Content   string `json:"content"`
	Type      string `json:"type"`
	CreatedAt string `json:"created_at"`
	Sender    struct {
		Username string `json:"username"`
		Slug     string `json:"slug"`
		Identity struct {
			Color  string `json:"color"`
			Badges []struct {
				Type string `json:"type"`
			} `json:"badges"`
		} `json:"identity"`
	} `json:"sender"`
	Metadata struct {
		OriginalSender struct {
			Username string `json:"username"`
		} `json:"original_sender"`
	} `json:"metadata"`
}

// emoteToken matches an emote as the chat sends it: [emote:<id>:<name>].
var emoteToken = regexp.MustCompile(`\[emote:\d+:([^\]]*)\]`)

// renderContent turns message content into terminal text: an emote becomes its
// name, since there is no picture to show.
func renderContent(content string) string {
	out := emoteToken.ReplaceAllStringFunc(content, func(tok string) string {
		name := strings.TrimSpace(emoteToken.FindStringSubmatch(tok)[1])
		if name == "" {
			return ""
		}
		return name
	})
	return strings.TrimSpace(out)
}

// parseChatMessage reads a ChatMessageEvent. ok is false for a message with
// nothing to show: no author or no text.
func parseChatMessage(f frame, now time.Time) (msg catalog.ChatMessage, ok bool, err error) {
	var p chatMessage
	if err := f.payload(&p); err != nil {
		return catalog.ChatMessage{}, false, err
	}
	body := renderContent(p.Content)
	name := strings.TrimSpace(p.Sender.Username)
	user := strings.TrimSpace(p.Sender.Slug)
	if user == "" {
		user = strings.ToLower(name)
	}
	if body == "" || (name == "" && user == "") {
		return catalog.ChatMessage{}, false, nil
	}
	// A reply reaches the terminal without the message it answers; naming who it
	// answers keeps it readable.
	if p.Type == "reply" {
		if to := strings.TrimSpace(p.Metadata.OriginalSender.Username); to != "" && !strings.HasPrefix(body, "@"+to) {
			body = "@" + to + " " + body
		}
	}
	received := now
	if t, perr := time.Parse(time.RFC3339, p.CreatedAt); perr == nil {
		received = t
	}
	msg = catalog.ChatMessage{
		Kind:        catalog.ChatMessageChat,
		ReceivedAt:  received,
		ID:          p.ID,
		User:        user,
		DisplayName: name,
		Color:       strings.TrimSpace(p.Sender.Identity.Color),
		Body:        body,
	}
	for _, b := range p.Sender.Identity.Badges {
		if t := strings.TrimSpace(b.Type); t != "" {
			msg.Badges = append(msg.Badges, t)
		}
	}
	return msg, true, nil
}
