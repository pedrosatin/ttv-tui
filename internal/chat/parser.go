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
	EventPrivMsg    EventKind = "privmsg"
	EventNotice     EventKind = "notice"
	EventUserNotice EventKind = "usernotice"
	EventRoomState  EventKind = "roomstate"
	EventReconnect  EventKind = "reconnect"
	EventClearChat  EventKind = "clearchat"
	EventClearMsg   EventKind = "clearmsg"
	EventPing       EventKind = "ping"
	EventOther      EventKind = "other"
	EventMalformed  EventKind = "malformed"
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
	// CumulativeMonths is msg-param-cumulative-months on USERNOTICE (0 when absent).
	CumulativeMonths int
	CommunityGiftID  string
	GiftCount        int
	GiftRecipient    string
	// SystemMsg is the Twitch-provided system-msg tag on USERNOTICE (English).
	SystemMsg string
	RoomState RoomState
	RoomDelta RoomStateDelta
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
	argsPart, ok := parseEnvelope(&ev, line)
	if !ok {
		return ev
	}

	var args lineArgs
	args.params, args.trailing, args.hasTrailing = splitParamsAndTrailing(argsPart)

	switch ev.Command {
	case "PRIVMSG":
		parsePrivMsg(&ev, args)
	case "NOTICE":
		parseNotice(&ev, args)
	case "ROOMSTATE":
		parseRoomState(&ev, args)
	case "RECONNECT":
		ev.Kind = EventReconnect
	case "CLEARCHAT":
		parseClearChat(&ev, args)
	case "CLEARMSG":
		parseClearMsg(&ev, args)
	case "USERNOTICE":
		parseUserNotice(&ev, args)
	default:
		parseOther(&ev, args)
	}
	return ev
}

// parseEnvelope reads the tags, prefix and command of line into ev and returns
// the text after the command. ok is false when ev is already final: the line
// is malformed or a PING, which has no arguments to dispatch.
func parseEnvelope(ev *Event, line string) (argsPart string, ok bool) {
	if strings.TrimSpace(line) == "" {
		ev.Kind = EventMalformed
		return "", false
	}

	rest := line
	if strings.HasPrefix(rest, "@") {
		var rawTags string
		rawTags, rest = cutLead(rest)
		if rawTags == "" {
			ev.Kind = EventMalformed
			return "", false
		}
		ev.Tags = parseTags(rawTags)
		if rest == "" {
			ev.Kind = EventMalformed
			return "", false
		}
	}

	// Fast path and exact payload preservation for PING (with or without prefix/tags).
	if strings.HasPrefix(rest, "PING") && (len(rest) == 4 || rest[4] == ' ') {
		ev.Kind = EventPing
		ev.Command = "PING"
		if len(rest) > 4 {
			ev.PingPayload = rest[4:] // includes leading space and ':' if present
		}
		return "", false
	}

	prefix := ""
	if strings.HasPrefix(rest, ":") {
		prefix, rest = cutLead(rest)
		if prefix == "" || rest == "" {
			ev.Kind = EventMalformed
			return "", false
		}
	}

	cmd, argsPart, hasArgs := strings.Cut(rest, " ")
	if hasArgs {
		argsPart = strings.TrimLeft(argsPart, " ")
	}
	if cmd == "" || !isValidCommand(cmd) {
		ev.Kind = EventMalformed
		return "", false
	}
	ev.Command = cmd
	ev.User = extractNick(prefix)

	// A PING behind a prefix. The payload is the exact text after the command
	// word, leading space included, so "PING" inside the tags or the prefix
	// never shifts it.
	if cmd == "PING" {
		ev.Kind = EventPing
		ev.PingPayload = rest[len(cmd):]
		return "", false
	}
	return argsPart, true
}

// cutLead splits s, which starts with a one-byte marker ('@' or ':'), into the
// word after the marker and the text after the spaces that follow it. lead is
// empty when the word is empty or no space ends it; rest is then meaningless.
func cutLead(s string) (lead, rest string) {
	sp := strings.IndexByte(s, ' ')
	if sp <= 1 {
		return "", ""
	}
	return s[1:sp], strings.TrimLeft(s[sp+1:], " ")
}

// lineArgs is what follows the command: the middle parameters and the trailing
// one, which starts at " :".
type lineArgs struct {
	params      []string
	trailing    string
	hasTrailing bool
}

// channel returns the first parameter without its '#'. ok is false when the
// parameter is missing, does not start with '#', or is a bare '#'.
func (a lineArgs) channel() (name string, ok bool) {
	if len(a.params) < 1 {
		return "", false
	}
	name, ok = strings.CutPrefix(a.params[0], "#")
	if name == "" {
		return "", false
	}
	return name, ok
}

func parsePrivMsg(ev *Event, args lineArgs) {
	channel, ok := args.channel()
	if !ok || !args.hasTrailing {
		ev.Kind = EventMalformed
		return
	}
	ev.Kind = EventPrivMsg
	ev.Channel = channel
	ev.Body, ev.Action = unwrapAction(args.trailing)
	populatePrivMsgTags(ev)
	if ev.DisplayName == "" {
		ev.DisplayName = ev.User
	}
	if ev.DisplayName == "" {
		ev.Kind = EventMalformed
	}
}

// parseNotice accepts a target without '#': Twitch addresses some notices to "*".
func parseNotice(ev *Event, args lineArgs) {
	if len(args.params) < 1 || !args.hasTrailing {
		ev.Kind = EventMalformed
		return
	}
	ev.Kind = EventNotice
	ev.Channel = strings.TrimPrefix(args.params[0], "#")
	ev.Body = args.trailing
	if ev.Tags != nil {
		ev.MsgID = ev.Tags["msg-id"]
	}
}

func parseRoomState(ev *Event, args lineArgs) {
	channel, ok := args.channel()
	if !ok {
		ev.Kind = EventMalformed
		return
	}
	ev.Kind = EventRoomState
	ev.Channel = channel
	populateRoomState(ev)
}

func parseClearChat(ev *Event, args lineArgs) {
	channel, ok := args.channel()
	if !ok {
		ev.Kind = EventMalformed
		return
	}
	ev.Kind = EventClearChat
	ev.Channel = channel
	if args.hasTrailing {
		ev.TargetUser = strings.TrimSpace(args.trailing)
	}
	if ev.Tags != nil {
		if raw, ok := ev.Tags["ban-duration"]; ok {
			if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
				ev.BanDuration = &n
			}
		}
	}
}

func parseClearMsg(ev *Event, args lineArgs) {
	channel, ok := args.channel()
	if !ok {
		ev.Kind = EventMalformed
		return
	}
	ev.Kind = EventClearMsg
	ev.Channel = channel
	ev.Body = args.trailing
	if ev.Tags != nil {
		ev.TargetUser = ev.Tags["login"]
		ev.TargetMsgID = ev.Tags["target-msg-id"]
	}
}

func parseUserNotice(ev *Event, args lineArgs) {
	channel, ok := args.channel()
	if !ok {
		ev.Kind = EventMalformed
		return
	}
	ev.Kind = EventUserNotice
	ev.Channel = channel
	ev.Body = args.trailing
	populateUserNoticeTags(ev)
	if ev.DisplayName == "" {
		ev.DisplayName = ev.User
	}
}

// parseOther keeps every command the client has no handler for. The channel is
// set only when the first parameter names one.
func parseOther(ev *Event, args lineArgs) {
	ev.Kind = EventOther
	if channel, ok := args.channel(); ok {
		ev.Channel = channel
	}
	ev.Body = args.trailing
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

func populateUserNoticeTags(ev *Event) {
	if ev.Tags == nil {
		return
	}
	ev.MsgID = strings.TrimSpace(ev.Tags["msg-id"])
	ev.ID = ev.Tags["id"]
	ev.DisplayName = strings.TrimSpace(ev.Tags["display-name"])
	ev.Color = normalizeHexColor(ev.Tags["color"])
	ev.SystemMsg = ev.Tags["system-msg"]
	ev.CommunityGiftID = strings.TrimSpace(ev.Tags["msg-param-community-gift-id"])
	ev.GiftRecipient = strings.TrimSpace(ev.Tags["msg-param-recipient-display-name"])
	if ev.GiftRecipient == "" {
		ev.GiftRecipient = strings.TrimSpace(ev.Tags["msg-param-recipient-user-name"])
	}
	if n, err := strconv.Atoi(ev.Tags["msg-param-mass-gift-count"]); err == nil && n > 0 {
		ev.GiftCount = n
	}
	if login := strings.TrimSpace(ev.Tags["login"]); login != "" {
		ev.User = login
	}
	if rawBadges := strings.TrimSpace(ev.Tags["badges"]); rawBadges != "" {
		for _, b := range strings.Split(rawBadges, ",") {
			b = strings.TrimSpace(b)
			if b != "" {
				ev.Badges = append(ev.Badges, b)
			}
		}
	}
	if raw := strings.TrimSpace(ev.Tags["msg-param-cumulative-months"]); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			ev.CumulativeMonths = n
		}
	}
}

// UserNoticeKind groups USERNOTICE msg-id values by how the chat presents them.
type UserNoticeKind string

const (
	UserNoticeOther        UserNoticeKind = "other"
	UserNoticeSub          UserNoticeKind = "sub"
	UserNoticeResub        UserNoticeKind = "resub"
	UserNoticeGift         UserNoticeKind = "gift"
	UserNoticeRaid         UserNoticeKind = "raid"
	UserNoticeAnnouncement UserNoticeKind = "announcement"
)

// ClassifyUserNotice maps a USERNOTICE msg-id onto a UserNoticeKind. "unraid"
// stays Other on purpose: a cancelled raid must not read as an incoming one.
func ClassifyUserNotice(msgID string) UserNoticeKind {
	switch strings.ToLower(strings.TrimSpace(msgID)) {
	case "sub":
		return UserNoticeSub
	case "resub", "resubscribe":
		return UserNoticeResub
	case "subgift", "anonsubgift", "submysterygift", "anonsubmysterygift":
		return UserNoticeGift
	case "raid":
		return UserNoticeRaid
	case "announcement":
		return UserNoticeAnnouncement
	default:
		return UserNoticeOther
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
