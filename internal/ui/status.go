package ui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
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
		return fmt.Sprintf(m.t("error.fail.detail"), sanitizeChatText(te.Err.Error()))
	}
	if msg := strings.TrimSpace(sanitizeChatText(m.err.Error())); msg != "" {
		return fmt.Sprintf(m.t("error.fail.detail"), msg)
	}
	return ""
}

func (m Model) activeContextSummary() string {
	var parts []string
	if m.selectedChannel != nil {
		playerHasLogin := m.playerLogin != "" && strings.EqualFold(m.playerLogin, m.selectedChannel.Login)
		name := sanitizeChatText(m.selectedChannel.DisplayName)
		login := sanitizeChatText(m.selectedChannel.Login)
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
		catText := sanitizeChatText(m.selectedCategory.Name)
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
	return sanitizeChatText(strings.TrimSpace(*ch.CategoryName))
}

// statusParts splits the status text into the main message for the current
// state and secondary context, most important first. The main message carries
// what is happening and the next step; secondaries are what narrow terminals
// may drop.
//
// Drop order among secondaries: trailing technical detail first, then the
// least useful of context vs chat. Soft states (ready / category selected /
// playback ended) promote a channel-bearing chat status to primary so 80
// columns keep the name whole. When the lead already names the channel
// (playing/selected), context outranks the duplicate chat line; otherwise
// chat outranks context so the name is not cut behind category/viewers.
func (m Model) statusParts() (string, []string) {
	var secondary []string
	var context []string
	trailing := ""
	addContext := func() {
		if ctx := m.activeContextSummary(); ctx != "" {
			context = append(context, ctx)
		}
	}
	primary := m.t("status.ready")
	switch {
	case m.ctrlCNotice != "":
		// Ctrl+C hint outranks filter/search prompts so every mode shows it.
		primary = m.ctrlCNotice
	case m.err != nil:
		primary = m.listErrorAction()
		// The technical detail goes last: it is the first thing to drop.
		trailing = m.listErrorDetail()
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
	case m.corruptNotice != "":
		primary = m.corruptNotice
	case m.copyNotice != "":
		primary = m.copyNotice
	case m.statusNotice != "":
		primary = m.statusNotice
	case m.playerWindowNoticeShown():
		primary = m.t("player.window_notice")
		secondary = append(secondary, m.playerStatusText())
		addContext()
	case m.playerState == PlayerStateError:
		primary = m.playerFailureText()
		// The technical detail goes last: it is the first thing to drop.
		trailing = m.playerErrorDetail()
	case m.playerState != PlayerStateNone && m.playerState != PlayerStateEnded:
		primary = m.playerStatusBase()
		if vol := m.playerVolumeText(); vol != "" {
			secondary = append(secondary, vol)
		}
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
		secondary = append([]string{primary}, secondary...)
		primary = chatStatus
		chatStatus = ""
		channelInPrimary = true
	}

	// When the lead already names the channel (playing/selected), context is
	// more informative than a duplicate "Conectado em #canal". Otherwise the
	// channel-bearing chat status outranks context so the name is not cut
	// behind category/viewers.
	if chatStatus != "" && channelInPrimary {
		secondary = append(secondary, context...)
		secondary = append(secondary, chatStatus)
	} else {
		if chatStatus != "" {
			secondary = append(secondary, chatStatus)
		}
		secondary = append(secondary, context...)
	}
	if trailing != "" {
		secondary = append(secondary, trailing)
	}
	return primary, secondary
}

// softStatusPrimary reports states whose lead message may yield to a connected
// chat channel. Player actions, errors, search/filter and one-shot notices keep
// the lead (ticket 07 / orientation).
func (m Model) softStatusPrimary() bool {
	if m.err != nil || m.searching || m.filtering || m.loading {
		return false
	}
	if m.authNotice != "" || m.corruptNotice != "" || m.copyNotice != "" || m.statusNotice != "" || m.ctrlCNotice != "" {
		return false
	}
	if m.playerWindowNoticeShown() || m.playerState == PlayerStateError {
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

// focusStatusText names the focused panel when the chat column is visible.
func (m Model) focusStatusText() string {
	if !m.hasChatPanel() || m.chatHiddenByWidth() {
		return ""
	}
	if m.Focus() == FocusChat {
		return m.t("status.focus_chat")
	}
	return m.t("status.focus_list")
}

// StatusBar renders the status bar containing the active language, channel/category/viewers,
// player state (with volume), chat state, and key hints.
//
// At narrow widths the bar gives things up in this order: secondary context,
// the app title, the language tag, the help hint. A channel-bearing chat
// status outranks context: the full connected string is preferred, then the
// short "#login" ref, and only as a last resort the name is cut with a trailing
// ellipsis. The main message for the current state and the quit hint are the
// last to go.
func (m Model) StatusBar() string {
	lang := i18n.LangEN
	if m.catalog != nil {
		lang = m.catalog.Lang()
	}
	title := m.t("app.title")
	helpHint := m.t("status.help_hint")
	quitHint := m.t("status.quit_hint")
	// Focus lives in the chrome (with help/quit), not in the state message, so
	// Tab always has a stable place to announce the active panel. Skip it while
	// an error detail needs the columns.
	focusHint := ""
	if m.err == nil {
		focusHint = m.focusStatusText()
	}
	rightHints := fmt.Sprintf(" | %s · %s ", helpHint, quitHint)
	if focusHint != "" {
		rightHints = fmt.Sprintf(" | %s · %s · %s ", focusHint, helpHint, quitHint)
	}

	primary, secondary := m.statusParts()
	sep := " · "

	layouts := []struct{ left, right string }{
		{fmt.Sprintf(" %s | [%s] ", title, lang), rightHints},
		{fmt.Sprintf(" [%s] ", lang), rightHints},
		{" ", rightHints},
		{" ", " | " + quitHint + " "},
	}
	chosen := layouts[0]
	if m.width > 0 {
		chosen = layouts[len(layouts)-1]
		for _, l := range layouts {
			if ansi.StringWidth(l.left+primary+l.right) <= m.width {
				chosen = l
				break
			}
		}
	}

	// Color the lead before measuring/joining so SGR does not eat column budget.
	styledPrimary := m.styleStatusPrimary(primary)
	stateText := styledPrimary
	plainState := primary
	if m.width <= 0 {
		for _, s := range secondary {
			stateText += sep + s
			plainState += sep + s
		}
	} else {
		avail := m.width - ansi.StringWidth(chosen.left) - ansi.StringWidth(chosen.right)
		if ansi.StringWidth(plainState) > avail {
			plainState = TruncateVisual(primary, max(avail, 1))
			stateText = m.styleStatusPrimary(plainState)
		} else {
			for i, s := range secondary {
				room := avail - ansi.StringWidth(plainState) - ansi.StringWidth(sep)
				if ansi.StringWidth(s) <= room {
					stateText += sep + s
					plainState += sep + s
					continue
				}
				// Channel name outranks remaining secondaries: try the short
				// "#login" form before cutting or giving up.
				if ref := m.chatChannelRef(); ref != "" && strings.Contains(s, ref) {
					if ansi.StringWidth(ref) <= room {
						stateText += sep + ref
						plainState += sep + ref
						break
					}
					if i == len(secondary)-1 && room >= minSecondaryCut {
						cut := TruncateVisual(ref, room)
						stateText += sep + cut
						plainState += sep + cut
					}
					break
				}
				// Only the last secondary may be cut, and only when enough of
				// it survives to be read; a stub like "vo…" helps nobody.
				if i == len(secondary)-1 && room >= minSecondaryCut {
					cut := TruncateVisual(s, room)
					stateText += sep + cut
					plainState += sep + cut
				}
				break
			}
		}
	}
	line := chosen.left + stateText + chosen.right

	style := lipgloss.NewStyle().Reverse(true)
	if m.width > 0 {
		// Pad using the plain visual width so colored SGR does not inflate the bar.
		plainLine := chosen.left + plainState + chosen.right
		pad := m.width - ansi.StringWidth(plainLine)
		if pad > 0 {
			line += strings.Repeat(" ", pad)
		} else if pad < 0 {
			line = padOrTruncateVisual(line, m.width)
		}
	}
	return style.Render(line)
}

// minSecondaryCut is the narrowest room in which a truncated secondary part
// is still worth showing in the status bar.
const minSecondaryCut = 12
