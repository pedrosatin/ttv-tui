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
	}
	if m.selectedCategory != nil && m.selectedCategory.Name != "" {
		catText := sanitizeChatText(m.selectedCategory.Name)
		if m.selectedChannel == nil && m.selectedCategory.Viewers != nil {
			catText += " (" + m.formatViewers(m.selectedCategory.Viewers) + ")"
		}
		parts = append(parts, catText)
	}
	return strings.Join(parts, " · ")
}

// statusParts splits the status text into the main message for the current
// state and secondary context, most important first. The main message carries
// what is happening and the next step; secondaries are what narrow terminals
// may drop.
func (m Model) statusParts() (string, []string) {
	var secondary []string
	trailing := ""
	withContext := func() {
		if ctx := m.activeContextSummary(); ctx != "" {
			secondary = append(secondary, ctx)
		}
	}
	primary := m.t("status.ready")
	switch {
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
	case m.statusNotice != "":
		primary = m.statusNotice
	case m.playerWindowNoticeShown():
		primary = m.t("player.window_notice")
		secondary = append(secondary, m.playerStatusText())
		withContext()
	case m.playerState == PlayerStateError:
		primary = m.playerFailureText()
		// The technical detail goes last: it is the first thing to drop.
		trailing = m.playerErrorDetail()
	case m.playerState != PlayerStateNone && m.playerState != PlayerStateEnded:
		primary = m.playerStatusBase()
		if vol := m.playerVolumeText(); vol != "" {
			secondary = append(secondary, vol)
		}
		withContext()
	case m.loading:
		switch m.Mode() {
		case ModeChannels:
			primary = m.t("channels.loading")
		case ModeSearch:
			primary = m.t("search.loading")
		default:
			primary = m.t("status.loading")
		}
		withContext()
	case m.playerState == PlayerStateEnded:
		primary = m.playerStatusText()
		withContext()
	case m.selectedChannel != nil:
		primary = fmt.Sprintf(m.t("channels.selected"), m.selectedChannel.DisplayName, m.selectedChannel.Login)
		if m.selectedChannel.Viewers != nil {
			primary += " " + m.formatChannelViewers(m.selectedChannel.Viewers)
		}
		if m.selectedCategory != nil && m.selectedCategory.Name != "" {
			secondary = append(secondary, m.selectedCategory.Name)
		}
	case m.Mode() == ModeSearch && m.searchQuery != "":
		primary = fmt.Sprintf(m.t("search.title_query"), m.searchQuery)
	case m.selectedCategory != nil:
		primary = fmt.Sprintf(m.t("categories.selected"), m.selectedCategory.Name)
		if m.selectedCategory.Viewers != nil {
			primary += " " + m.formatViewers(m.selectedCategory.Viewers)
		}
	}
	if m.err == nil {
		if chatStatus := m.chatStatusSummary(); chatStatus != "" && !strings.Contains(primary, chatStatus) {
			secondary = append(secondary, chatStatus)
		}
	}
	if trailing != "" {
		secondary = append(secondary, trailing)
	}
	return primary, secondary
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
// the app title, the language tag, the help hint. The main message for the
// current state and the quit hint are the last to go.
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

	stateText := primary
	if m.width <= 0 {
		for _, s := range secondary {
			stateText += sep + s
		}
	} else {
		avail := m.width - ansi.StringWidth(chosen.left) - ansi.StringWidth(chosen.right)
		if ansi.StringWidth(stateText) > avail {
			stateText = TruncateVisual(stateText, max(avail, 1))
		} else {
			for i, s := range secondary {
				room := avail - ansi.StringWidth(stateText) - ansi.StringWidth(sep)
				if ansi.StringWidth(s) <= room {
					stateText += sep + s
					continue
				}
				// Only the last secondary may be cut, and only when enough of
				// it survives to be read; a stub like "vo…" helps nobody.
				if i == len(secondary)-1 && room >= minSecondaryCut {
					stateText += sep + TruncateVisual(s, room)
				}
				break
			}
		}
	}
	line := chosen.left + stateText + chosen.right

	style := lipgloss.NewStyle().Reverse(true)
	if m.width > 0 {
		line = padOrTruncateVisual(line, m.width)
	}
	return style.Render(line)
}

// minSecondaryCut is the narrowest room in which a truncated secondary part
// is still worth showing in the status bar.
const minSecondaryCut = 12
