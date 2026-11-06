package ui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/chat"
)

const (
	defaultChatPreviewLines = 10
	// twitchMessageRuneLimit is Twitch's PRIVMSG body limit in Unicode code points.
	twitchMessageRuneLimit = 500
)

func (m Model) hasChatPanel() bool {
	return m.chatPanel.channel != "" || m.chatPanel.state != ChatStateNone || m.composing
}

func (m Model) chatStatusSummary() string {
	ch := sanitizeScreenText(m.chatPanel.channel)
	switch m.chatPanel.state {
	case ChatStateConnecting:
		return fmt.Sprintf(m.t("chat.connecting"), ch)
	case ChatStateConnected:
		if m.chatPanel.anonymous {
			return fmt.Sprintf(m.t("chat.connected_anon"), ch)
		}
		return fmt.Sprintf(m.t("chat.connected"), ch)
	case ChatStateReconnecting:
		return m.chatConnectionReason(fmt.Sprintf(m.t("chat.reconnecting"), ch))
	case ChatStateDisconnected:
		return m.chatConnectionReason(m.t("chat.disconnected"))
	case ChatStateError:
		errText := sanitizeScreenText(m.chatPanel.errText)
		if errText == "" {
			errText = m.t("player.error_unknown")
		}
		return fmt.Sprintf(m.t("chat.error"), errText)
	default:
		return ""
	}
}

func (m Model) chatConnectionReason(state string) string {
	if reason := strings.TrimSpace(sanitizeScreenText(m.chatPanel.errText)); reason != "" {
		return state + ": " + reason
	}
	return state
}

func (m Model) chatRoomModesSummary() string {
	rs := m.chatPanel.roomState
	var tags []string
	if rs.Slow > 0 {
		tags = append(tags, fmt.Sprintf(m.t("chat.room.slow"), rs.Slow))
	}
	if rs.FollowersOnly >= 0 {
		tags = append(tags, m.t("chat.room.followers"))
	}
	if rs.SubsOnly {
		tags = append(tags, m.t("chat.room.subs"))
	}
	return strings.Join(tags, " · ")
}

func (m Model) chatHeaderState() string {
	switch m.chatPanel.state {
	case ChatStateConnecting:
		return m.t("chat.state.connecting")
	case ChatStateConnected:
		if m.chatPanel.anonymous {
			return m.t("chat.state.connected_anon")
		}
		return m.t("chat.state.connected")
	case ChatStateReconnecting:
		return m.chatConnectionReason(m.t("chat.state.reconnecting"))
	case ChatStateDisconnected:
		return m.chatConnectionReason(m.t("chat.disconnected"))
	case ChatStateError:
		errText := sanitizeScreenText(m.chatPanel.errText)
		if errText == "" {
			errText = m.t("player.error_unknown")
		}
		return fmt.Sprintf(m.t("chat.error"), errText)
	default:
		return ""
	}
}

func (m Model) renderChatHeaderLines(maxWidth int) []string {
	// Focus chrome only when both panels share the screen (split). Chat-only
	// and focus layouts have a single panel, so the caret would be noise.
	showFocusChrome := m.listVisible() && m.chatColumnVisible() && m.Focus() == FocusChat
	focusMarker := "  "
	if showFocusChrome {
		focusMarker = "▸ "
	}
	title := fmt.Sprintf(m.t("chat.title"), sanitizeScreenText(m.chatPanel.channel))
	state := m.chatHeaderState()
	modes := m.chatRoomModesSummary()

	head := focusMarker + title
	if showFocusChrome {
		head += " " + m.t("focus.active_tag")
	}

	var headerLines []string
	if maxWidth <= 0 {
		if state != "" {
			head += " · " + state
		}
		if modes != "" {
			head += " [" + modes + "]"
		}
		headerLines = []string{head}
	} else {
		full := head
		if state != "" {
			full += " · " + state
		}
		if modes != "" {
			full += " [" + modes + "]"
		}

		if ansi.StringWidth(full) <= maxWidth {
			headerLines = []string{full}
		} else if state == "" {
			headerLines = []string{TruncateVisual(head, maxWidth)}
		} else {
			withoutModes := head + " · " + state
			if ansi.StringWidth(withoutModes) <= maxWidth {
				headerLines = []string{withoutModes}
			} else {
				line1 := TruncateVisual(head, maxWidth)
				line2 := TruncateVisual("  "+state, maxWidth)
				headerLines = []string{line1, line2}
			}
		}
	}

	for i, line := range headerLines {
		line = TruncateVisual(line, maxWidth)
		if showFocusChrome {
			line = styleFocus(line)
		}
		headerLines[i] = line
	}
	return headerLines
}

func (m Model) renderChatLines(maxLines int, maxWidth int) []string {
	if !m.hasChatPanel() {
		return nil
	}
	headerLines := m.renderChatHeaderLines(maxWidth)

	// The footer (pending-new notice, warning, compose hint, input line) is
	// pinned to the bottom of the panel: messages fill the space above it and
	// blank rows pad the gap, so the input line is always the panel's last row
	// whatever the message count.
	var footer []string
	if notice := m.renderChatPendingNotice(maxWidth); notice != "" {
		footer = append(footer, notice)
	}
	if m.chatPanel.sendWarning != "" {
		if maxWidth <= 0 {
			warnLine := "  ! " + styleWarning(sanitizeScreenText(m.chatPanel.sendWarning))
			footer = append(footer, warnLine)
		} else {
			warnRows := wrapChatRow("  ! ", sanitizeScreenText(m.chatPanel.sendWarning), maxWidth)
			for _, r := range warnRows {
				footer = append(footer, styleWarning(r))
			}
		}
	}
	if m.composing && m.chatHasToken() {
		hint := m.renderComposeHintLine(maxWidth)
		if m.composeDraftOverLimit() {
			footer = append(footer, styleWarning(hint))
		} else {
			footer = append(footer, lipgloss.NewStyle().Faint(true).Render(hint))
		}
	}
	footer = append(footer, TruncateVisual(m.renderChatInputLine(maxWidth), maxWidth))

	msgs := m.ChatMessages()
	pinned := maxLines > 0 && m.height > 0
	if maxLines <= 0 {
		maxLines = defaultChatPreviewLines
	}
	msgBudget := maxLines - len(headerLines) - len(footer)
	if msgBudget < 0 {
		msgBudget = 0
	}

	body := make([]string, 0, msgBudget)
	if len(msgs) == 0 {
		if msgBudget > 0 && m.chatPanel.state == ChatStateConnected {
			emptyLine := "  " + fmt.Sprintf(m.t("chat.empty"), m.chatPanel.channel)
			body = append(body, TruncateVisual(emptyLine, maxWidth))
		}
	} else {
		end := m.chatViewportEnd(len(msgs))
		body = m.chatMessageRows(msgs[:end], msgBudget, maxWidth, m.chatSelectedIndex(len(msgs)))
	}

	lines := make([]string, 0, maxLines)
	lines = append(lines, headerLines...)
	lines = append(lines, body...)
	if pinned {
		for len(lines)+len(footer) < maxLines {
			lines = append(lines, "")
		}
	}
	lines = append(lines, footer...)
	if pinned && len(lines) > maxLines {
		// Too short for header plus footer: keep the bottom, where the input lives.
		lines = lines[len(lines)-maxLines:]
	}
	return lines
}

// composeCursor marks the insertion point; it is drawn only while composing.
const composeCursor = "█"

func (m Model) composeDraftRuneCount() int {
	return utf8.RuneCountInString(m.composeDraft)
}

func (m Model) composeDraftOverLimit() bool {
	return utf8.RuneCountInString(strings.TrimSpace(m.composeDraft)) > twitchMessageRuneLimit
}

func (m Model) composeCounterLabel() string {
	return fmt.Sprintf(m.t("chat.input.counter"), m.composeDraftRuneCount(), twitchMessageRuneLimit)
}

// renderComposeHintLine shows the compose hint and the live N/500 counter on one
// row above the input. The counter stays visible; the hint truncates first when
// the chat column is narrow (80-column layout).
func (m Model) renderComposeHintLine(maxWidth int) string {
	hint := m.t("chat.input.compose_hint")
	counter := m.composeCounterLabel()
	return formatComposeHintWithCounter(hint, counter, maxWidth)
}

func formatComposeHintWithCounter(hint, counter string, maxWidth int) string {
	prefix := "  "
	sep := " "
	if maxWidth <= 0 {
		return prefix + hint + sep + counter
	}
	reserved := ansi.StringWidth(prefix) + ansi.StringWidth(sep) + ansi.StringWidth(counter)
	room := maxWidth - reserved
	if room < 1 {
		return TruncateVisual(prefix+counter, maxWidth)
	}
	if ansi.StringWidth(hint) > room {
		hint = TruncateVisual(hint, room)
	}
	return prefix + hint + sep + counter
}

func (m Model) renderChatInputLine(maxWidth int) string {
	ch := sanitizeScreenText(m.chatPanel.channel)
	if maxWidth > 1 && hasWideRune(ch+normalizeScreenText(m.composeDraft)) {
		maxWidth--
	}
	if !m.chatHasToken() {
		hint := m.t("chat.input.no_token")
		if maxWidth > 0 && ansi.StringWidth("  > "+hint) > maxWidth {
			hint = m.t("chat.input.no_token_short")
		}
		line := "  > " + hint
		line = TruncateVisual(line, maxWidth)
		return lipgloss.NewStyle().Faint(true).Render(line)
	}
	if m.composing {
		prefix := "  > " + fmt.Sprintf(m.t("chat.input.composing"), ch)
		// A long channel name must not push the draft and cursor off the panel.
		const minDraftCells = 8
		if maxWidth > 0 && ansi.StringWidth(prefix)+minDraftCells+ansi.StringWidth(composeCursor) > maxWidth {
			if limit := maxWidth - minDraftCells - ansi.StringWidth(composeCursor); limit > 0 {
				prefix = TruncateVisual(prefix, limit)
			} else {
				prefix = ""
			}
		}
		draft := sanitizeScreenText(m.composeDraft)
		line := prefix + draftWindow(draft, prefix, maxWidth, ansi.StringWidth(composeCursor)) + composeCursor
		return lipgloss.NewStyle().Bold(true).Render(line)
	}
	if m.composeDraft != "" {
		draft := sanitizeScreenText(m.composeDraft)
		prefix := "  > " + fmt.Sprintf(m.t("chat.input.draft"), "")
		line := prefix + draftWindow(draft, prefix, maxWidth, 0)
		return lipgloss.NewStyle().Faint(true).Render(line)
	}
	ready := fmt.Sprintf(m.t("chat.input.ready"), ch)
	if maxWidth > 0 && ansi.StringWidth("  > "+ready) > maxWidth {
		ready = fmt.Sprintf(m.t("chat.input.ready_short"), ch)
	}
	line := TruncateVisual("  > "+ready, maxWidth)
	return lipgloss.NewStyle().Faint(true).Render(line)
}

// draftTail keeps the end of draft (and room for the compose cursor) visible.
// Older text is replaced by a leading ellipsis. maxWidth <= 0 means no limit.
func draftTail(draft, prefix string, maxWidth int) string {
	return draftWindow(draft, prefix, maxWidth, ansi.StringWidth(composeCursor))
}

// draftWindow returns the end of draft that fits after prefix, leaving cursorWidth
// cells free at the end. Older text is replaced by a leading ellipsis.
// maxWidth <= 0 means no limit.
func draftWindow(draft, prefix string, maxWidth, cursorWidth int) string {
	if maxWidth <= 0 {
		return draft
	}
	room := maxWidth - ansi.StringWidth(prefix) - cursorWidth
	if room <= 0 {
		return ""
	}
	if ansi.StringWidth(draft) <= room {
		return draft
	}
	room -= ansi.StringWidth("…")
	if room <= 0 {
		return "…"
	}
	runes := []rune(draft)
	start := len(runes)
	width := 0
	for start > 0 {
		w := ansi.StringWidth(string(runes[start-1]))
		if width+w > room {
			break
		}
		width += w
		start--
	}
	return "…" + string(runes[start:])
}

// chatContinuationIndent is the hanging indent of a wrapped message's second and
// later rows, deeper than the first row's lead so each message reads as a block.
const chatContinuationIndent = 4

// chatMessageRows lays out msgs bottom-up into at most budget rows. Each message
// wraps at word boundaries by visual width (wide runes count as two cells, ANSI
// styling counts as zero). A token longer than the column is hard-broken into
// fitting pieces — never cut with "…" — so a long URL stays complete in reading
// order. When the newest messages fill the budget, older ones leave the screen;
// the oldest visible message may show only its last rows. The buffer is not touched.
// selectedIdx is the index within msgs of the scroll highlight (or -1).
func (m Model) chatMessageRows(msgs []ChatMessage, budget, maxWidth, selectedIdx int) []string {
	if budget <= 0 {
		return nil
	}
	var rows []string
	for i := len(msgs) - 1; i >= 0 && len(rows) < budget; i-- {
		msg := msgs[i]
		lead, content := m.formatChatMessageParts(msg, i == selectedIdx)
		limit := maxWidth
		if limit > 1 && hasWideRune(lead+content) {
			limit--
		}
		// Drop the secondary author badge before it would force content to wrap.
		// Reformat a local copy so the stored IRC metadata remains intact.
		if maxWidth > 0 && m.authorBadge(msg.Badges) != "" && ansi.StringWidth(lead+content) > limit {
			msg.Badges = nil
			lead, content = m.formatChatMessageParts(msg, i == selectedIdx)
		}
		rows = append(wrapChatRow(lead, content, maxWidth), rows...)
	}
	if len(rows) > budget {
		rows = rows[len(rows)-budget:]
	}
	return rows
}

// wrapChatRow wraps lead+content into rows no wider than maxWidth cells. The
// first row starts with lead; later rows start with chatContinuationIndent
// spaces. Word boundaries are preferred; a single token wider than the column
// (a long URL, for example) is hard-broken into pieces that fit, without "…".
// Styles that span a break are closed at the end of the row and reopened on the
// next one, so colour never bleeds into padding or the other panel.
// maxWidth <= 0 means no limit (a single row).
func wrapChatRow(lead, content string, maxWidth int) []string {
	lead = normalizeScreenText(lead)
	content = normalizeScreenText(content)
	// Leave the final cell free before wrapping, so the screen-level guard
	// removes padding only and never drops the end of a chat message.
	if maxWidth > 1 && hasWideRune(lead+content) {
		maxWidth--
	}
	if maxWidth <= 0 {
		return []string{lead + content}
	}
	if ansi.StringWidth(lead)+ansi.StringWidth(content) <= maxWidth {
		return []string{lead + content}
	}
	indent := chatContinuationIndent
	if lw := ansi.StringWidth(lead); lw > indent {
		indent = lw
	}
	limit := maxWidth - indent
	if limit < 1 {
		// Lead alone fills the row: hard-break the combined string, no ellipsis.
		return hardWrapVisual(lead+content, maxWidth)
	}
	parts := carrySGR(strings.Split(ansi.Wrap(content, limit, ""), "\n"))
	pad := strings.Repeat(" ", indent)
	rows := make([]string, 0, len(parts))
	for i, part := range parts {
		prefix := lead
		if i > 0 {
			prefix = pad
		}
		rows = append(rows, fitPrefixedChatRow(prefix, part, maxWidth)...)
	}
	return rows
}

// fitPrefixedChatRow returns one or more rows of prefix+text that fit in
// maxWidth cells. Text that still overflows (wide grapheme edge cases after
// ansi.Wrap) is hard-broken; "…" is never inserted.
func fitPrefixedChatRow(prefix, text string, maxWidth int) []string {
	prefixW := ansi.StringWidth(prefix)
	room := maxWidth - prefixW
	if room < 1 {
		return hardWrapVisual(prefix+text, maxWidth)
	}
	if ansi.StringWidth(text) <= room {
		return []string{prefix + text}
	}
	chunks := carrySGR(hardWrapVisual(text, room))
	rows := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		rows = append(rows, prefix+chunk)
	}
	return rows
}

// hardWrapVisual splits s into chunks of at most limit visual cells, preserving
// ANSI sequences and every printable character. No ellipsis is inserted.
// limit < 1 returns s unchanged.
func hardWrapVisual(s string, limit int) []string {
	if limit < 1 {
		return []string{s}
	}
	if ansi.StringWidth(s) <= limit {
		return []string{s}
	}
	// preserveSpace keeps leading spaces that belong to the token stream so a
	// hard break never drops characters that soft-wrap would have discarded.
	return strings.Split(ansi.Hardwrap(s, limit, true), "\n")
}

// carrySGR makes each row self-contained: SGR styles still open at the end of a
// row are reset there and reapplied at the start of the next row.
func carrySGR(rows []string) []string {
	var active []string
	for i, row := range rows {
		reopen := strings.Join(active, "")
		var state byte
		for rest := row; rest != ""; {
			seq, _, n, next := ansi.DecodeSequence(rest, state, nil)
			if n <= 0 {
				break
			}
			state, rest = next, rest[n:]
			if !ansi.HasCsiPrefix(seq) || seq[len(seq)-1] != 'm' {
				continue
			}
			if isSGRReset(seq) {
				active = nil
			} else {
				active = append(active, seq)
			}
		}
		if reopen != "" {
			row = reopen + row
		}
		if len(active) > 0 {
			row += ansi.ResetStyle
		}
		rows[i] = row
	}
	return rows
}

// isSGRReset reports whether seq (a CSI ... m sequence) resets all styles:
// no parameters or a single 0.
func isSGRReset(seq string) bool {
	params := seq[1 : len(seq)-1] // after an 8-bit CSI
	if seq[0] == ansi.ESC {
		params = seq[2 : len(seq)-1] // after ESC [
	}
	return len(params) == 0 || (len(params) == 1 && params[0] == '0')
}

// formatChatMessageParts returns the row lead ("  ", "  * " or the highlight star) and
// the styled message text, so wrapping can indent continuation rows.
// selected marks the scroll highlight (bottom-visible message) when the chat
// panel holds focus; the lead becomes "▌ " so ticket 05 can point at a row.
// The deleted tag exists only here, so copy and buffer accounting never see it.
func (m Model) formatChatMessageParts(msg ChatMessage, selected bool) (string, string) {
	lead, content := m.formatChatRow(msg, selected)
	if msg.Deleted {
		content = styleWarning(m.t("chat.deleted_tag")) + " " + content
	}
	return lead, content
}

func (m Model) formatChatRow(msg ChatMessage, selected bool) (string, string) {
	selLead := "  "
	if selected && m.Focus() == FocusChat {
		selLead = "▌ "
	}
	body := sanitizeScreenText(msg.Body)
	if msg.Kind == chat.MessageNotice {
		prefix := m.t("chat.notice_prefix")
		return selLead, styleNotice(fmt.Sprintf("%s %s", prefix, body))
	}
	if msg.Kind == chat.MessageSystem {
		if chat.ClassifyUserNotice(msg.MsgID) == chat.UserNoticeRaid {
			// Star and tag carry the meaning; the reverse video only adds to them.
			lead := "★ "
			if selected && m.Focus() == FocusChat {
				lead = "▌★"
			}
			return lead, styleChatHighlight(m.t("chat.raid_prefix")) + " " + styleSystem(body)
		}
		prefix := m.systemLinePrefix(msg)
		return selLead, styleSystem(fmt.Sprintf("%s %s", prefix, body))
	}

	name := sanitizeScreenText(msg.DisplayName)
	if name == "" {
		name = sanitizeScreenText(msg.User)
	}

	// Nicks use only blue/white ANSI slots so they never collide with the
	// semantic palette (live/error/pause/focus).
	renderedName := styleNick(name, msg.Color)
	if badge := m.authorBadge(msg.Badges); badge != "" {
		renderedName = badge + " " + renderedName
	}

	deliveryBadge := m.formatDeliveryBadge(msg)
	suffix := ""
	if deliveryBadge != "" {
		suffix = " " + deliveryBadge
	}

	mentions := m.mentionsSelf(body)
	if msg.Highlighted || mentions {
		tag := m.t("chat.highlight_tag")
		styleTag := styleChatHighlight
		if mentions && !msg.Highlighted {
			tag = m.t("chat.mention_tag")
			styleTag = styleMention
		}
		lead := "★ "
		if selected && m.Focus() == FocusChat {
			lead = "▌★"
		}
		head := styleTag(tag) + " " + renderedName
		if mentions && !msg.Highlighted {
			// Tag + underline carry the mention; nick stays on the nick palette.
			text := styleMention(tag) + " " + renderedName + ": " + styleMention(body)
			if deliveryBadge != "" {
				return lead, text + " " + deliveryBadge
			}
			return lead, text
		}
		return lead, fmt.Sprintf("%s: %s%s", head, body, suffix)
	}
	if msg.Action {
		// The action marker stays on the first row; its "* " is part of the lead.
		lead := "  * "
		if selected && m.Focus() == FocusChat {
			lead = "▌* "
		}
		return lead, fmt.Sprintf("%s %s", renderedName, body+suffix)
	}
	return selLead, fmt.Sprintf("%s: %s%s", renderedName, body, suffix)
}

// authorBadge uses technical abbreviations shared by EN and PT-BR. Match the
// IRC badge name, ignoring its version, and keep only the highest-priority role.
func (m Model) authorBadge(badges []string) string {
	best, rank := "", 0
	for _, badge := range badges {
		name, _, _ := strings.Cut(badge, "/")
		switch name {
		case "moderator":
			return m.t("chat.author.mod")
		case "broadcaster":
			best, rank = m.t("chat.author.streamer"), 2
		case "vip":
			if rank < 1 {
				best, rank = m.t("chat.author.vip"), 1
			}
		}
	}
	return best
}

// systemMsgIDModeration marks CLEARCHAT/CLEARMSG lines so the chat view can
// show [mod] instead of the generic [sistema]/[system] prefix.
const systemMsgIDModeration = "moderation"

// systemMsgIDReconnect marks the line left in the history by a reconnect.
const systemMsgIDReconnect = "reconnect_gap"

func (m Model) systemLinePrefix(msg ChatMessage) string {
	if msg.MsgID == systemMsgIDModeration {
		return m.t("chat.mod_prefix")
	}
	return m.t("chat.system_prefix")
}

func (m Model) formatDeliveryBadge(msg ChatMessage) string {
	switch msg.Delivery {
	case chat.DeliverySending:
		return m.t("chat.send.sending")
	case chat.DeliveryConfirmed:
		return m.t("chat.send.confirmed")
	case chat.DeliveryRejected:
		reason := m.formatRejectReason(msg.RejectReason, msg.RejectDetail)
		return styleReject(fmt.Sprintf(m.t("chat.send.rejected"), reason))
	case chat.DeliveryDiscarded:
		reason := m.formatRejectReason(msg.RejectReason, msg.RejectDetail)
		return styleReject(fmt.Sprintf(m.t("chat.send.discarded"), reason))
	default:
		return ""
	}
}

func (m Model) formatRejectReason(reason chat.RejectReason, detail string) string {
	switch reason {
	case chat.RejectSlowMode:
		return m.t("chat.reject.slow_mode")
	case chat.RejectFollowersOnly:
		return m.t("chat.reject.followers_only")
	case chat.RejectSubsOnly:
		return m.t("chat.reject.subs_only")
	case chat.RejectEmoteOnly:
		return m.t("chat.reject.emote_only")
	case chat.RejectR9K:
		return m.t("chat.reject.r9k")
	case chat.RejectRateLimited:
		return m.t("chat.reject.rate_limit")
	case chat.RejectUnauthenticated:
		return m.t("chat.reject.unauthenticated")
	case chat.RejectChannelSwitched:
		return m.t("chat.reject.channel_switched")
	case chat.RejectTimeout:
		return m.t("chat.reject.timeout")
	case chat.RejectConnectionLost:
		return m.t("chat.reject.connection_lost")
	case chat.RejectReconnected:
		return m.t("chat.reject.reconnected")
	case chat.RejectBanned:
		return m.t("chat.reject.banned")
	default:
		if clean := sanitizeScreenText(strings.TrimSpace(detail)); clean != "" {
			return clean
		}
		return m.t("chat.reject.other")
	}
}
