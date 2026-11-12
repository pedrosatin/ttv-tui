package ui

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

func (m Model) errorMessage(err error) string {
	switch catalog.KindOf(err) {
	case catalog.Rede:
		return m.t("error.network")
	case catalog.RateLimit:
		return m.t("error.rate_limit")
	case catalog.Servidor:
		return m.t("error.server")
	case catalog.Contrato:
		return m.t("error.contract")
	default:
		return m.t("error.network")
	}
}

// clearStaleStatusErrors drops list and player errors from the status bar when
// the user changes screen or mode. The last player error line stays so help (?)
// can still show the technical detail. Transient notices (favorite added,
// copied, auth) are left alone — they have their own lifetime.
func (m *Model) clearStaleStatusErrors() {
	m.list.err = nil
	if m.player.state != PlayerStateError {
		return
	}
	m.player.state = PlayerStateNone
	m.player.failure = PlayerFailureNone
}

func (m Model) listErrorAction() string {
	switch catalog.KindOf(m.list.err) {
	case catalog.Rede:
		return m.t("error.list.network")
	case catalog.Contrato:
		return m.t("error.list.contract")
	case catalog.RateLimit:
		return m.t("error.list.rate_limit")
	case catalog.Servidor:
		return m.t("error.list.server")
	default:
		return m.t("error.list.unknown")
	}
}

func (m Model) listErrorDetail() string {
	if m.list.err == nil {
		return ""
	}
	var te *catalog.Error
	if errors.As(m.list.err, &te) && te != nil && te.Err != nil {
		return fmt.Sprintf(m.t("error.fail.detail"), sanitizeScreenText(te.Err.Error()))
	}
	if msg := strings.TrimSpace(sanitizeScreenText(m.list.err.Error())); msg != "" {
		return fmt.Sprintf(m.t("error.fail.detail"), msg)
	}
	return ""
}

func (m Model) activeContextSummary() string {
	var parts []string
	if m.list.selectedChannel != nil {
		playerHasLogin := m.player.login != "" && strings.EqualFold(m.player.login, m.list.selectedChannel.PlayerLabel())
		name := sanitizeScreenText(m.list.selectedChannel.DisplayName)
		login := sanitizeScreenText(m.list.selectedChannel.Login)
		chText := ""
		if !playerHasLogin {
			chText = login
			if name != "" && !strings.EqualFold(name, login) {
				chText = fmt.Sprintf("%s (%s)", name, login)
			} else if name != "" {
				chText = name
			}
		}
		if m.list.selectedChannel.Viewers != nil {
			vText := m.formatChannelViewers(m.list.selectedChannel.Viewers)
			if chText != "" {
				chText += " (" + vText + ")"
			} else {
				chText = vText
			}
		}
		if chText != "" {
			parts = append(parts, chText)
		}
		// Category of the playing/selected channel — never the last list visited.
		if cat := channelCategoryName(m.list.selectedChannel); cat != "" {
			parts = append(parts, cat)
		}
		return strings.Join(parts, " · ")
	}
	if m.list.selectedCategory != nil && m.list.selectedCategory.Name != "" {
		catText := sanitizeScreenText(m.list.selectedCategory.Name)
		if m.list.selectedCategory.Viewers != nil {
			catText += " (" + m.formatViewers(m.list.selectedCategory.Viewers) + ")"
		}
		parts = append(parts, catText)
	}
	return strings.Join(parts, " · ")
}

// channelCategoryName returns the sanitized category label carried by ch, or
// empty when unknown so the status bar can omit it.
func channelCategoryName(ch *catalog.Channel) string {
	if ch == nil || ch.CategoryName == nil {
		return ""
	}
	return sanitizeScreenText(strings.TrimSpace(*ch.CategoryName))
}

// statusContent is the state half of the bar, grouped by how long each piece
// survives a narrow terminal.
type statusContent struct {
	tone    statusTone
	primary string
	// keep outlasts the title, the language and the layout token.
	keep []string
	// extra is context, best first. The title yields to the first entry; the
	// rest only fill the room left.
	extra []string
	// detail is the technical line after the message, the first thing to drop.
	detail string
	// hint follows the detail: how to fix the problem, dropped before the detail.
	hint string
}

// softStatusPrimary reports states whose lead message may yield to a connected
// chat channel. Player actions, errors, search/filter and one-shot notices keep
// the lead (ticket 07 / orientation).
func (m Model) softStatusPrimary() bool {
	if m.list.err != nil || m.list.search.active || m.list.filtering || m.list.prefs.menuOpen || m.list.loading {
		return false
	}
	if m.notices.auth != "" || m.notices.corrupt != "" || m.chat.copyNotice != "" || m.notices.status != "" || m.notices.ctrlC != "" {
		return false
	}
	if m.player.windowNoticeShown() || m.player.state == PlayerStateError || m.player.state == PlayerStateEnded {
		return false
	}
	if m.player.state != PlayerStateNone && m.player.state != PlayerStateEnded {
		return false
	}
	return true
}

// statusHasChannel reports whether the chat summary includes a room name.
func (c chatState) statusHasChannel() bool {
	switch c.panel.state {
	case ChatStateConnecting, ChatStateConnected, ChatStateReconnecting:
		return c.panel.channel != ""
	default:
		return false
	}
}

// primaryNamesChannel reports whether primary already cites the chat room
// (playing/selected), so promoting the chat status would only duplicate it.
func (c chatState) primaryNamesChannel(primary string) bool {
	ch := strings.TrimSpace(c.panel.channel)
	if ch == "" {
		return false
	}
	return strings.Contains(strings.ToLower(primary), strings.ToLower(ch))
}

// chatChannelRef is the short "#login" form used when the full chat status
// does not fit but the channel name still can.
func (m Model) chatChannelRef() string {
	ch := strings.TrimSpace(sanitizeScreenText(m.chat.panel.channel))
	if ch == "" || !m.chat.statusHasChannel() {
		return ""
	}
	return fmt.Sprintf(m.t("status.channel_ref"), ch)
}

// focusStatusText names the focused panel when both panels are visible.
func (m Model) focusStatusText() string {
	if m.hosted.host != nil {
		if m.shouldShowNarrowChatNotice() {
			return m.t("status.chat_narrow")
		}
		if !m.listVisible() && !m.chatColumnVisible() {
			return m.t("status.video_only")
		}
	}
	if !m.listVisible() || !m.chatColumnVisible() {
		return ""
	}
	if m.Focus() == FocusChat {
		return m.t("status.focus_chat")
	}
	return m.t("status.focus_list")
}

// layoutStatusText is the compact layout token for the status bar.
func (m Model) layoutStatusText() string {
	return fmt.Sprintf(m.t("status.layout"), m.layoutModeLabel(m.LayoutMode()))
}

// includeLayoutStatus reports when the layout token may join the bar. Default
// split is omitted (the two-panel chrome already implies it). Filter, search,
// error and notice leads stay clear so "Connected to #ch" and "detalhe:" keep
// their columns.
func (m Model) includeLayoutStatus() bool {
	if m.LayoutMode() == LayoutSplit {
		return false
	}
	if m.list.err != nil || m.list.prefs.menuOpen || m.list.search.active || m.list.filtering {
		return false
	}
	if m.player.state == PlayerStateError {
		return false
	}
	if m.notices.ctrlC != "" || m.notices.auth != "" || m.notices.corrupt != "" || m.chat.copyNotice != "" || m.notices.status != "" {
		return false
	}
	return true
}

// statusChrome is one rung of the cut order: the frame around the state text.
type statusChrome struct {
	left, right string
	layoutToken bool
}

// statusLadder lists the frames from richest to barest, so walking it down is
// the cut order: title, language, layout token, focus hint, help hint. The state
// message, the volume, the active filter and the quit hint outlast all of them.
func (m Model) statusLadder(lang string) []statusChrome {
	help, quit := m.t("status.help_hint"), m.t("status.quit_hint")
	hints := fmt.Sprintf(" | %s · %s ", help, quit)
	focusHints := hints
	// Focus lives in the chrome, not in the state message, so Tab has a stable
	// place to announce the active panel. Skip it while an error detail needs
	// the columns.
	if focus := m.focusStatusText(); focus != "" && m.list.err == nil {
		focusHints = fmt.Sprintf(" | %s · %s · %s ", focus, help, quit)
	}
	last := " | " + quit + " "
	// The window is narrow exactly when this hint matters: it outlasts the help hint.
	if m.hosted.host != nil && m.shouldShowNarrowChatNotice() {
		last = fmt.Sprintf(" | %s · %s ", m.t("status.chat_narrow"), quit)
		hints, focusHints = last, last
	}
	return []statusChrome{
		{fmt.Sprintf(" %s | [%s] ", m.t("app.title"), lang), focusHints, true},
		{fmt.Sprintf(" [%s] ", lang), focusHints, true},
		{" ", focusHints, true},
		{" ", focusHints, false},
		{" ", hints, false},
		{" ", last, false},
	}
}

// StatusBar renders the one-line bar: state on the left, key hints pinned to the
// right edge so they stay put when the state text changes length.
//
// The rung is chosen for the message, the volume, the filter and the layout
// token; the title also needs the main context to fit. What room is left goes to
// the context, then to the technical detail. A channel-bearing chat status
// prefers the full string, then the short "#login" ref, then a cut name.
func (m Model) StatusBar() string {
	lang := i18n.LangEN
	if m.catalog != nil {
		lang = m.catalog.Lang()
	}
	// Every piece of the bar passes here, Twitch names and the player login included.
	c := m.statusParts()
	c.primary = sanitizeScreenText(c.primary)
	c.detail = sanitizeScreenText(c.detail)
	for i := range c.keep {
		c.keep[i] = sanitizeScreenText(c.keep[i])
	}
	for i := range c.extra {
		c.extra[i] = sanitizeScreenText(c.extra[i])
	}
	token := ""
	if m.includeLayoutStatus() {
		token = m.layoutStatusText()
	}
	glyph := c.tone.glyph()
	if glyph != "" {
		glyph += " "
	}

	ladder := m.statusLadder(lang)
	essentials := func(ch statusChrome) []string {
		parts := slices.Clone(c.keep)
		if ch.layoutToken && token != "" {
			parts = append(parts, token)
		}
		return parts
	}
	chosen := ladder[0]
	if m.width > 0 {
		chosen = ladder[len(ladder)-1]
		for i, ch := range ladder {
			parts := essentials(ch)
			if i == 0 {
				parts = append(parts, c.extra[:min(len(c.extra), 1)]...)
			}
			lead := strings.Join(append([]string{glyph + c.primary}, parts...), statusSep)
			if ansi.StringWidth(ch.left+lead+ch.right) <= m.width {
				chosen = ch
				break
			}
		}
	}

	rest := slices.Clone(c.extra)
	if c.detail != "" {
		rest = append(rest, c.detail)
	}
	if c.hint != "" {
		rest = append(rest, sanitizeScreenText(c.hint))
	}
	primary := c.primary
	var shown []string
	if m.width <= 0 {
		shown = append(essentials(chosen), rest...)
	} else {
		room := m.width - ansi.StringWidth(chosen.left+chosen.right+glyph)
		if ansi.StringWidth(primary) > room {
			primary = TruncateVisual(primary, max(room, 1))
		} else {
			room -= ansi.StringWidth(primary)
			var complete bool
			shown, room, complete = m.fitStatusParts(essentials(chosen), room)
			if complete {
				more, _, _ := m.fitStatusParts(rest, room)
				shown = append(shown, more...)
			}
		}
	}

	tail := ""
	for _, s := range shown {
		tail += statusSep + s
	}
	if m.width > 0 {
		used := ansi.StringWidth(chosen.left + glyph + primary + tail + chosen.right)
		tail += strings.Repeat(" ", max(m.width-used, 0))
	}
	line := renderStatusRuns(c.tone, chosen.left, primary, tail+chosen.right)
	if m.width > 0 && ansi.StringWidth(line) > m.width {
		line = TruncateVisual(line, m.width)
	}
	return fitScreenLines(line, m.width)
}

// renderStatusRuns paints the bar as runs that each carry the reverse. Wrapping
// an already styled string would not work: the inner reset also ends the outer
// reverse, leaving the rest of the line without a background. With a glyph the
// color stays on its cell; a tone without one paints the message.
func renderStatusRuns(tone statusTone, left, primary, tail string) string {
	bar := styleStatusBar()
	glyph := tone.glyph()
	if glyph == "" {
		return bar.Render(left) + tone.style().Render(primary) + bar.Render(tail)
	}
	return bar.Render(left) + tone.style().Render(glyph) + bar.Render(" "+primary+tail)
}

// fitStatusParts takes parts in order until one does not fit in room, the columns
// left after the lead. It returns what fit and, when every part made it, the room
// still free.
func (m Model) fitStatusParts(parts []string, room int) ([]string, int, bool) {
	sepWidth := ansi.StringWidth(statusSep)
	var shown []string
	for i, s := range parts {
		room -= sepWidth
		if ansi.StringWidth(s) <= room {
			shown = append(shown, s)
			room -= ansi.StringWidth(s)
			continue
		}
		// Channel name outranks remaining parts: try the short "#login" form
		// before cutting or giving up.
		cut := s
		if ref := m.chatChannelRef(); ref != "" && strings.Contains(s, ref) {
			if ansi.StringWidth(ref) <= room {
				return append(shown, ref), 0, false
			}
			cut = ref
		}
		// Only the last part may be cut, and only when enough of it survives to
		// be read; a stub like "vo…" helps nobody.
		if i == len(parts)-1 && room >= minSecondaryCut {
			return append(shown, TruncateVisual(cut, room)), 0, false
		}
		return shown, 0, false
	}
	return shown, room, true
}

// minSecondaryCut is the narrowest room in which a truncated secondary part
// is still worth showing in the status bar.
const minSecondaryCut = 12

// statusSep joins the pieces of the state text.
const statusSep = " · "
