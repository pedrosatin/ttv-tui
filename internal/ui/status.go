package ui

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

func (m Model) errorMessage(err error) string {
	switch twitch.KindOf(err) {
	case twitch.Rede:
		return m.t("error.network")
	case twitch.RateLimit:
		return m.t("error.rate_limit")
	case twitch.Servidor:
		return m.t("error.server")
	case twitch.Contrato:
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
	m.err = nil
	if m.playerState != PlayerStateError {
		return
	}
	m.playerState = PlayerStateNone
	m.playerFailure = PlayerFailureNone
}

func (m Model) listErrorAction() string {
	switch twitch.KindOf(m.err) {
	case twitch.Rede:
		return m.t("error.list.network")
	case twitch.Contrato:
		return m.t("error.list.contract")
	case twitch.RateLimit:
		return m.t("error.list.rate_limit")
	case twitch.Servidor:
		return m.t("error.list.server")
	default:
		return m.t("error.list.unknown")
	}
}

func (m Model) listErrorDetail() string {
	if m.err == nil {
		return ""
	}
	var te *twitch.Error
	if errors.As(m.err, &te) && te != nil && te.Err != nil {
		return fmt.Sprintf(m.t("error.fail.detail"), sanitizeScreenText(te.Err.Error()))
	}
	if msg := strings.TrimSpace(sanitizeScreenText(m.err.Error())); msg != "" {
		return fmt.Sprintf(m.t("error.fail.detail"), msg)
	}
	return ""
}

func (m Model) activeContextSummary() string {
	var parts []string
	if m.selectedChannel != nil {
		playerHasLogin := m.playerLogin != "" && strings.EqualFold(m.playerLogin, m.selectedChannel.Login)
		name := sanitizeScreenText(m.selectedChannel.DisplayName)
		login := sanitizeScreenText(m.selectedChannel.Login)
		chText := ""
		if !playerHasLogin {
			chText = login
			if name != "" && !strings.EqualFold(name, login) {
				chText = fmt.Sprintf("%s (%s)", name, login)
			} else if name != "" {
				chText = name
			}
		}
		if m.selectedChannel.Viewers != nil {
			vText := m.formatChannelViewers(m.selectedChannel.Viewers)
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
		if cat := channelCategoryName(m.selectedChannel); cat != "" {
			parts = append(parts, cat)
		}
		return strings.Join(parts, " · ")
	}
	if m.selectedCategory != nil && m.selectedCategory.Name != "" {
		catText := sanitizeScreenText(m.selectedCategory.Name)
		if m.selectedCategory.Viewers != nil {
			catText += " (" + m.formatViewers(m.selectedCategory.Viewers) + ")"
		}
		parts = append(parts, catText)
	}
	return strings.Join(parts, " · ")
}

// channelCategoryName returns the sanitized category label carried by ch, or
// empty when unknown so the status bar can omit it.
func channelCategoryName(ch *twitch.Channel) string {
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
}

// statusParts decides what the bar says for the current state. The main message
// carries what is happening and the next step.
//
// Soft states (ready / category selected) promote a channel-bearing chat status
// to primary so 80 columns keep the name whole. When the lead already names the
// channel (playing/selected), context outranks the duplicate chat line;
// otherwise chat outranks context so the name is not cut behind category/viewers.
func (m Model) statusParts() statusContent {
	var c statusContent
	var context []string
	addContext := func() {
		if ctx := m.activeContextSummary(); ctx != "" {
			context = append(context, ctx)
		}
	}
	primary := m.t("status.ready")
	switch {
	case m.playerState == PlayerStateError:
		primary = m.playerFailureText()
		c.tone = toneError
		c.detail = m.playerErrorDetail()
	case m.ctrlCNotice != "":
		// Ctrl+C hint outranks filter/search prompts so every mode shows it.
		primary = m.ctrlCNotice
	case m.err != nil:
		primary = m.listErrorAction()
		c.tone = toneError
		c.detail = m.listErrorDetail()
	case m.channelFilterMenu:
		primary = m.t("channel_filter.status_open")
	case m.searching:
		primary = fmt.Sprintf(m.t("search.prompt"), m.searchInput)
	case m.filtering:
		if m.filterHasNoMatches() {
			primary = fmt.Sprintf(m.t("filter.search_hint"), strings.TrimSpace(m.filter))
		} else {
			primary = fmt.Sprintf(m.t("filter.prompt"), m.filter)
		}
	case m.authNotice != "":
		primary = m.authNotice
		c.tone = toneWarning
	case m.corruptNotice != "":
		primary = m.corruptNotice
		c.tone = toneWarning
	case m.copyNotice != "":
		primary = m.copyNotice
		if primary == m.t("chat.copy.failed") {
			c.tone = toneWarning
		}
	case m.statusNotice != "":
		primary = m.statusNotice
		if m.statusNoticeWarns {
			c.tone = toneWarning
		}
	case m.playerWindowNoticeShown():
		primary = m.t("player.window_notice")
		c.tone = playerTone(m.playerState)
		context = append(context, m.playerStatusBase())
		addContext()
	case m.playerState != PlayerStateNone && m.playerState != PlayerStateEnded:
		primary = m.playerStatusBase()
		c.tone = playerTone(m.playerState)
		addContext()
	case m.loading:
		switch m.Mode() {
		case ModeChannels:
			primary = m.t("channels.loading")
		case ModeSearch:
			primary = m.t("search.loading")
		default:
			primary = m.t("status.loading")
		}
		addContext()
	case m.playerState == PlayerStateEnded:
		primary = m.playerStatusText()
		c.tone = toneEnded
		addContext()
	case m.selectedChannel != nil:
		primary = fmt.Sprintf(m.t("channels.selected"), m.selectedChannel.DisplayName, m.selectedChannel.Login)
		if m.selectedChannel.Viewers != nil {
			primary += " " + m.formatChannelViewers(m.selectedChannel.Viewers)
		}
		if cat := channelCategoryName(m.selectedChannel); cat != "" {
			context = append(context, cat)
		}
	case m.Mode() == ModeSearch && m.searchQuery != "":
		primary = fmt.Sprintf(m.t("search.title_query"), m.searchQuery)
	case m.selectedCategory != nil:
		primary = fmt.Sprintf(m.t("categories.selected"), m.selectedCategory.Name)
		if m.selectedCategory.Viewers != nil {
			primary += " " + m.formatViewers(m.selectedCategory.Viewers)
		}
	}

	// The volume and the active filter stay on the bar under any lead: the mpv
	// window can change the volume with no other place to show it. Only an error
	// outranks the volume, because its detail needs the columns.
	volume := m.playerVolumeText()
	if c.tone == toneError {
		volume = ""
	}
	for _, s := range []string{volume, m.activeChannelFilterSummary()} {
		if s != "" && !strings.Contains(primary, s) {
			c.keep = append(c.keep, s)
		}
	}

	chatStatus := ""
	if m.err == nil {
		if s := m.chatStatusSummary(); s != "" && !strings.Contains(primary, s) {
			chatStatus = s
		}
	}

	channelInPrimary := m.primaryNamesChatChannel(primary)

	// Soft states yield the lead to a channel-bearing chat status so the name
	// is not truncated behind category/ready context on narrow terminals.
	if chatStatus != "" && m.chatStatusHasChannel() && m.softStatusPrimary() && !channelInPrimary {
		c.extra = append(c.extra, primary)
		primary = chatStatus
		chatStatus = ""
		channelInPrimary = true
	}

	// When the lead already names the channel (playing/selected), context is
	// more informative than a duplicate "Conectado em #canal". Otherwise the
	// channel-bearing chat status outranks context so the name is not cut
	// behind category/viewers.
	if chatStatus != "" && channelInPrimary {
		c.extra = append(c.extra, context...)
		c.extra = append(c.extra, chatStatus)
	} else {
		if chatStatus != "" {
			c.extra = append(c.extra, chatStatus)
		}
		c.extra = append(c.extra, context...)
	}
	c.primary = primary
	return c
}

// softStatusPrimary reports states whose lead message may yield to a connected
// chat channel. Player actions, errors, search/filter and one-shot notices keep
// the lead (ticket 07 / orientation).
func (m Model) softStatusPrimary() bool {
	if m.err != nil || m.searching || m.filtering || m.channelFilterMenu || m.loading {
		return false
	}
	if m.authNotice != "" || m.corruptNotice != "" || m.copyNotice != "" || m.statusNotice != "" || m.ctrlCNotice != "" {
		return false
	}
	if m.playerWindowNoticeShown() || m.playerState == PlayerStateError || m.playerState == PlayerStateEnded {
		return false
	}
	if m.playerState != PlayerStateNone && m.playerState != PlayerStateEnded {
		return false
	}
	return true
}

// chatStatusHasChannel reports whether the chat summary includes a room name.
func (m Model) chatStatusHasChannel() bool {
	switch m.chatPanel.state {
	case ChatStateConnecting, ChatStateConnected, ChatStateReconnecting:
		return m.chatPanel.channel != ""
	default:
		return false
	}
}

// primaryNamesChatChannel reports whether primary already cites the chat room
// (playing/selected), so promoting the chat status would only duplicate it.
func (m Model) primaryNamesChatChannel(primary string) bool {
	ch := strings.TrimSpace(m.chatPanel.channel)
	if ch == "" {
		return false
	}
	return strings.Contains(strings.ToLower(primary), strings.ToLower(ch))
}

// chatChannelRef is the short "#login" form used when the full chat status
// does not fit but the channel name still can.
func (m Model) chatChannelRef() string {
	ch := strings.TrimSpace(m.chatPanel.channel)
	if ch == "" || !m.chatStatusHasChannel() {
		return ""
	}
	return fmt.Sprintf(m.t("status.channel_ref"), ch)
}

// focusStatusText names the focused panel when both panels are visible.
func (m Model) focusStatusText() string {
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
	if m.err != nil || m.channelFilterMenu || m.searching || m.filtering {
		return false
	}
	if m.playerState == PlayerStateError {
		return false
	}
	if m.ctrlCNotice != "" || m.authNotice != "" || m.corruptNotice != "" || m.copyNotice != "" || m.statusNotice != "" {
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
	if focus := m.focusStatusText(); focus != "" && m.err == nil {
		focusHints = fmt.Sprintf(" | %s · %s · %s ", focus, help, quit)
	}
	return []statusChrome{
		{fmt.Sprintf(" %s | [%s] ", m.t("app.title"), lang), focusHints, true},
		{fmt.Sprintf(" [%s] ", lang), focusHints, true},
		{" ", focusHints, true},
		{" ", focusHints, false},
		{" ", hints, false},
		{" ", " | " + quit + " ", false},
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
	c := m.statusParts()
	c.primary = normalizeScreenText(c.primary)
	c.detail = normalizeScreenText(c.detail)
	for i := range c.keep {
		c.keep[i] = normalizeScreenText(c.keep[i])
	}
	for i := range c.extra {
		c.extra[i] = normalizeScreenText(c.extra[i])
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
