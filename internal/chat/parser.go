// Package chat implements the Twitch IRC client, pure IRCv3 line parser,
// bounded message buffer, and coalesced session manager.
package chat

import (
	"strconv"
	"strings"
)

// EventKind identifies the parsed IRC event category.
type EventKind string

const (
	EventPrivMsg   EventKind = "privmsg"
	EventNotice    EventKind = "notice"
	EventRoomState EventKind = "roomstate"
	EventReconnect EventKind = "reconnect"
	EventClearChat EventKind = "clearchat"
	EventClearMsg  EventKind = "clearmsg"
	EventPing      EventKind = "ping"
	EventOther     EventKind = "other"
	EventMalformed EventKind = "malformed"
)

// RoomState describes channel moderation modes reported by ROOMSTATE tags.
// Nil pointer fields in Event.RoomStateUpdate allow partial updates when Twitch
// sends single-tag ROOMSTATE deltas after the initial join.
type RoomState struct {
	Slow          int  // seconds; 0 = off
	FollowersOnly int  // minutes; -1 = off, >= 0 = enabled (0 = any follower)
	SubsOnly      bool // true = subscribers-only mode
	EmoteOnly     bool // true = emote-only mode
	R9K           bool // true = unique-chat mode
}

// RoomStateDelta holds optional fields present in a single ROOMSTATE line.
type RoomStateDelta struct {
	Slow          *int
	FollowersOnly *int
	SubsOnly      *bool
	EmoteOnly     *bool
	R9K           *bool
}

// Apply merges non-nil fields from d into r and returns the updated RoomState.
func (d RoomStateDelta) Apply(r RoomState) RoomState {
	if d.Slow != nil {
		r.Slow = *d.Slow
	}
	if d.FollowersOnly != nil {
		r.FollowersOnly = *d.FollowersOnly
	}
	if d.SubsOnly != nil {
		r.SubsOnly = *d.SubsOnly
	}
	if d.EmoteOnly != nil {
		r.EmoteOnly = *d.EmoteOnly
	}
	if d.R9K != nil {
		r.R9K = *d.R9K
	}
	return r
}

// Event is the pure output of ParseLine for a single IRC line.
type Event struct {
	Kind        EventKind
	Raw         string
	Tags        map[string]string
	Command     string
	Channel     string // normalized without '#'
	User        string // login from prefix
	DisplayName string // display-name tag or fallback to User
	Color       string // #RRGGBB or empty
	Badges      []string
	ID          string // tag "id" on PRIVMSG
	MsgID       string // tag "msg-id" on PRIVMSG / NOTICE / USERNOTICE
	Highlighted bool   // true when MsgID indicates a highlighted/special message
	Action      bool   // true when PRIVMSG body is CTCP \x01ACTION ...\x01
	Body        string // message text or NOTICE text
	PingPayload string // exact raw suffix after "PING" to echo back in "PONG"
	TargetUser  string // CLEARCHAT target login (empty = full chat clear) or CLEARMSG login
	TargetMsgID string // CLEARMSG target-msg-id
	// BanDuration is set when CLEARCHAT carries ban-duration (timeout seconds).
	// Nil with a TargetUser means a permanent ban; nil without a target is a full clear.
	BanDuration *int
	RoomState   RoomState
	RoomDelta   RoomStateDelta
}

// ParseLine parses a single IRC line (with or without trailing \r\n) into a
// structured Event without performing any I/O.
func ParseLine(rawLine string) Event {
	line := strings.TrimRight(rawLine, "\r\n")
	ev := Event{
		Raw: line,
		RoomState: RoomState{
			FollowersOnly: -1,
		},
	}
	if strings.TrimSpace(line) == "" {
		ev.Kind = EventMalformed
		return ev
	}

	rest := line
	if strings.HasPrefix(rest, "@") {
		sp := strings.IndexByte(rest, ' ')
		if sp <= 1 {
			ev.Kind = EventMalformed
			return ev
		}
		ev.Tags = parseTags(rest[1:sp])
		rest = strings.TrimLeft(rest[sp+1:], " ")
		if rest == "" {
			ev.Kind = EventMalformed
			return ev
		}
	}

	// Fast path and exact payload preservation for PING (with or without prefix/tags).
	if strings.HasPrefix(rest, "PING") && (len(rest) == 4 || rest[4] == ' ') {
		ev.Kind = EventPing
		ev.Command = "PING"
		if len(rest) > 4 {
			ev.PingPayload = rest[4:] // includes leading space and ':' if present
		}
		return ev
	}

	prefix := ""
	if strings.HasPrefix(rest, ":") {
		sp := strings.IndexByte(rest, ' ')
		if sp <= 1 {
			ev.Kind = EventMalformed
			return ev
		}
		prefix = rest[1:sp]
		rest = strings.TrimLeft(rest[sp+1:], " ")
		if rest == "" {
			ev.Kind = EventMalformed
			return ev
		}
	}

	// Extract command and arguments.
	var cmd string
	var argsPart string
	if sp := strings.IndexByte(rest, ' '); sp >= 0 {
		cmd = rest[:sp]
		argsPart = strings.TrimLeft(rest[sp+1:], " ")
	} else {
		cmd = rest
	}

	if cmd == "" || !isValidCommand(cmd) {
		ev.Kind = EventMalformed
		return ev
	}
	ev.Command = cmd
	ev.User = extractNick(prefix)

	if cmd == "PING" {
		ev.Kind = EventPing
		if idx := strings.Index(line, "PING"); idx >= 0 {
			ev.PingPayload = line[idx+4:]
		}
		return ev
	}

	params, trailing, hasTrailing := splitParamsAndTrailing(argsPart)

	switch cmd {
	case "PRIVMSG":
		if len(params) < 1 || !strings.HasPrefix(params[0], "#") || !hasTrailing {
			ev.Kind = EventMalformed
			return ev
		}
		ev.Kind = EventPrivMsg
		ev.Channel = strings.TrimPrefix(params[0], "#")
		body, isAction := unwrapAction(trailing)
		ev.Body = body
		ev.Action = isAction
		populatePrivMsgTags(&ev)
		if ev.DisplayName == "" {
			ev.DisplayName = ev.User
		}
		if ev.DisplayName == "" {
			ev.Kind = EventMalformed
		}
		return ev

	case "NOTICE":
		if len(params) < 1 || !hasTrailing {
			ev.Kind = EventMalformed
			return ev
		}
		ev.Kind = EventNotice
		ev.Channel = strings.TrimPrefix(params[0], "#")
		ev.Body = trailing
		if ev.Tags != nil {
			ev.MsgID = ev.Tags["msg-id"]
		}
		return ev

	case "ROOMSTATE":
		if len(params) < 1 || !strings.HasPrefix(params[0], "#") {
			ev.Kind = EventMalformed
			return ev
		}
		ev.Kind = EventRoomState
		ev.Channel = strings.TrimPrefix(params[0], "#")
		populateRoomState(&ev)
		return ev

	case "RECONNECT":
		ev.Kind = EventReconnect
		return ev

	case "CLEARCHAT":
		if len(params) < 1 || !strings.HasPrefix(params[0], "#") {
			ev.Kind = EventMalformed
			return ev
		}
		ev.Kind = EventClearChat
		ev.Channel = strings.TrimPrefix(params[0], "#")
		if hasTrailing {
			ev.TargetUser = strings.TrimSpace(trailing)
		}
		if ev.Tags != nil {
			if raw, ok := ev.Tags["ban-duration"]; ok {
				if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
					ev.BanDuration = &n
				}
			}
		}
		return ev

	case "CLEARMSG":
		if len(params) < 1 || !strings.HasPrefix(params[0], "#") {
			ev.Kind = EventMalformed
			return ev
		}
		ev.Kind = EventClearMsg
		ev.Channel = strings.TrimPrefix(params[0], "#")
		ev.Body = trailing
		if ev.Tags != nil {
			ev.TargetUser = ev.Tags["login"]
			ev.TargetMsgID = ev.Tags["target-msg-id"]
		}
		return ev

	default:
		ev.Kind = EventOther
		if len(params) > 0 && strings.HasPrefix(params[0], "#") {
			ev.Channel = strings.TrimPrefix(params[0], "#")
		}
		ev.Body = trailing
		return ev
	}
}

func parseTags(raw string) map[string]string {
	parts := strings.Split(raw, ";")
	tags := make(map[string]string, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			tags[k] = ""
			continue
		}
		tags[k] = unescapeTagValue(v)
	}
	return tags
}

// unescapeTagValue decodes IRCv3 escaped tag values:
// \: -> ;, \s -> ' ', \\ -> \, \r -> CR, \n -> LF.
func unescapeTagValue(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			continue
		}
		if i+1 >= len(s) {
			break
		}
		i++
		switch s[i] {
		case ':':
			b.WriteByte(';')
		case 's':
			b.WriteByte(' ')
		case '\\':
			b.WriteByte('\\')
		case 'r':
			b.WriteByte('\r')
		case 'n':
			b.WriteByte('\n')
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func populatePrivMsgTags(ev *Event) {
	if ev.Tags == nil {
		return
	}
	ev.DisplayName = strings.TrimSpace(ev.Tags["display-name"])
	ev.Color = normalizeHexColor(ev.Tags["color"])
	ev.ID = ev.Tags["id"]
	ev.MsgID = strings.TrimSpace(ev.Tags["msg-id"])
	if rawBadges := strings.TrimSpace(ev.Tags["badges"]); rawBadges != "" {
		for _, b := range strings.Split(rawBadges, ",") {
			b = strings.TrimSpace(b)
			if b != "" {
				ev.Badges = append(ev.Badges, b)
			}
		}
	}
	if ev.MsgID != "" || ev.Tags["custom-reward-id"] != "" {
		ev.Highlighted = true
		if ev.MsgID == "" {
			ev.MsgID = "custom-reward"
		}
	}
}

func populateRoomState(ev *Event) {
	rs := RoomState{FollowersOnly: -1}
	var delta RoomStateDelta
	if ev.Tags != nil {
		if v, ok := ev.Tags["slow"]; ok {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				rs.Slow = n
				val := n
				delta.Slow = &val
			}
		}
		if v, ok := ev.Tags["followers-only"]; ok {
			if n, err := strconv.Atoi(v); err == nil {
				rs.FollowersOnly = n
				val := n
				delta.FollowersOnly = &val
			}
		}
		if v, ok := ev.Tags["subs-only"]; ok {
			b := v == "1"
			rs.SubsOnly = b
			delta.SubsOnly = &b
		}
		if v, ok := ev.Tags["emote-only"]; ok {
			b := v == "1"
			rs.EmoteOnly = b
			delta.EmoteOnly = &b
		}
		if v, ok := ev.Tags["r9k"]; ok {
			b := v == "1"
			rs.R9K = b
			delta.R9K = &b
		}
	}
	ev.RoomState = rs
	ev.RoomDelta = delta
}

func normalizeHexColor(c string) string {
	c = strings.TrimSpace(c)
	if len(c) != 7 || c[0] != '#' {
		return ""
	}
	for i := 1; i < 7; i++ {
		ch := c[i]
		isHex := (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')
		if !isHex {
			return ""
		}
	}
	return c
}

func extractNick(prefix string) string {
	if prefix == "" {
		return ""
	}
	if bang := strings.IndexByte(prefix, '!'); bang > 0 {
		return prefix[:bang]
	}
	return ""
}

func splitParamsAndTrailing(s string) (params []string, trailing string, hasTrailing bool) {
	if s == "" {
		return nil, "", false
	}
	if strings.HasPrefix(s, ":") {
		return nil, s[1:], true
	}
	if idx := strings.Index(s, " :"); idx >= 0 {
		head := strings.TrimSpace(s[:idx])
		if head != "" {
			params = strings.Fields(head)
		}
		return params, s[idx+2:], true
	}
	return strings.Fields(s), "", false
}

func unwrapAction(body string) (string, bool) {
	const prefix = "\x01ACTION "
	if strings.HasPrefix(body, prefix) && strings.HasSuffix(body, "\x01") && len(body) >= len(prefix)+1 {
		return body[len(prefix) : len(body)-1], true
	}
	return body, false
}

func isValidCommand(cmd string) bool {
	if len(cmd) == 3 && cmd[0] >= '0' && cmd[0] <= '9' && cmd[1] >= '0' && cmd[1] <= '9' && cmd[2] >= '0' && cmd[2] <= '9' {
		return true
	}
	for i := 0; i < len(cmd); i++ {
		ch := cmd[i]
		isAlphaNum := (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
		if !isAlphaNum {
			return false
		}
	}
	return true
}

// FormatPong builds the exact wire response for a PING event, preserving the
// server's payload byte-for-byte.
func FormatPong(pingPayload string) string {
	if pingPayload == "" {
		return "PONG\r\n"
	}
	if strings.HasPrefix(pingPayload, " ") {
		return "PONG" + pingPayload + "\r\n"
	}
	return "PONG " + pingPayload + "\r\n"
}
