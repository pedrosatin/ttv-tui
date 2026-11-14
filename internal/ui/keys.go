package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
)

// expandBatchedKeyMsg splits a KeyMsg that carries several runes into one
// KeyMsg per rune, in order. Bubble Tea can deliver a burst such as "jjj" as a
// single KeyRunes message; without splitting, the registry never sees "j" and
// the movement is dropped. Paste events stay as one message so pasted text
// never fires navigation shortcuts rune-by-rune. Single-rune and non-rune
// keys are returned unchanged.
func expandBatchedKeyMsg(msg tea.KeyMsg) []tea.KeyMsg {
	if msg.Type != tea.KeyRunes || msg.Paste || len(msg.Runes) <= 1 {
		return []tea.KeyMsg{msg}
	}
	out := make([]tea.KeyMsg, len(msg.Runes))
	for i, r := range msg.Runes {
		out[i] = tea.KeyMsg{
			Type:  tea.KeyRunes,
			Runes: []rune{r},
			Alt:   msg.Alt,
		}
	}
	return out
}

func (m Model) handleKeyMsgBatch(keys []tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	cur := m
	for _, k := range keys {
		next, cmd := cur.handleKeyMsg(k)
		cur = next.(Model)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		if cur.quitting {
			break
		}
	}
	return cur, tea.Batch(cmds...)
}

func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if keys := expandBatchedKeyMsg(msg); len(keys) > 1 {
		return m.handleKeyMsgBatch(keys)
	}

	m.clearKeyNotices()
	if next, cmd, ok := m.handleAppKey(msg); ok {
		return next, cmd
	}

	// Help is modal: past this point no key reaches the list, chat, filter or
	// search behind it. Only the app-level Ctrl+Q and Ctrl+C above outrank it.
	if m.helpOpen() {
		return m.updateHelpKey(msg)
	}

	// No campo de texto, o que se digita e as teclas de edição vão ao campo
	// antes dos atalhos: Ctrl+H, Ctrl+W, = e _ não mexem no layout nem no volume.
	if m.typingField() && isFieldEditKey(msg) {
		next, cmd, _ := m.handleModeKey(msg)
		return next, cmd
	}
	if next, cmd, ok := m.handleGlobalKey(msg); ok {
		return next, cmd
	}
	if next, cmd, ok := m.handleLayoutKey(msg); ok {
		return next, cmd
	}
	if next, cmd, ok := m.handleModeKey(msg); ok {
		return next, cmd
	}
	if next, cmd, ok := m.handleCommandKey(msg); ok {
		return next, cmd
	}
	if next, cmd, ok := m.handlePlayerKey(msg); ok {
		return next, cmd
	}
	if next, cmd, ok := m.handleMoveKey(msg); ok {
		return next, cmd
	}
	return m, nil
}

// hiddenListKey rejects list actions while preserving shared chat shortcuts
// and text entry in a visible chat. Global controls are handled independently.
func (m Model) hiddenListKey(msg tea.KeyMsg) bool {
	if m.listVisible() || m.list.prefs.menuOpen || (m.chat.composing && m.chatColumnVisible()) {
		return false
	}
	switch m.keys.actionFor(modeNormal, msg) {
	case actFilter, actSearch, actFavorite, actChannelFilter, actSwitchPlatform:
		return true
	case actRetry:
		return !m.canRetryPlayer()
	case actUp, actDown, actPageUp, actPageDown, actHome, actEnd, actSelect, actBack, actCopyMessage:
		return !m.chatColumnVisible()
	}
	return false
}

// typingField diz se o foco está num campo de texto à vista: o prompt de busca,
// o filtro / ou a mensagem do chat.
func (m Model) typingField() bool {
	if m.listVisible() && (m.list.search.active || m.list.filtering) {
		return true
	}
	return m.chat.composing && m.Focus() == FocusChat && m.chatColumnVisible()
}

// switchPanelFocus toggles list ↔ chat. It leaves composition without clearing
// the draft. When layout hides either panel, Tab is a no-op; when width does, it
// says so.
func (m Model) switchPanelFocus() Model {
	if !m.listVisible() || !m.chatColumnVisible() {
		m.warnChatHiddenByWidth()
		return m
	}
	if m.layout.focus == FocusChat {
		m.setFocus(FocusList)
	} else {
		m.setFocus(FocusChat)
	}
	return m
}

// toggleHelp opens or closes the help, always at the top.
func (m *Model) toggleHelp() {
	m.layout.helpVisible = !m.layout.helpVisible
	m.layout.helpScroll = 0
}

// updateHelpKey consumes every key while the help is open: it closes, scrolls,
// or does nothing. Nothing here may touch the state behind the help.
func (m Model) updateHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	total := len(m.renderHelpLines(m.helpTextWidth()))
	page := max(m.helpPageRows(total)-1, 1)
	offset := m.helpOffset(total)
	switch m.keys.actionFor(modeHelp, msg) {
	case actHelp, actHelpAnywhere, actBack, actCloseHelp:
		m.toggleHelp()
		return m, nil
	case actUp:
		offset--
	case actDown:
		offset++
	case actPageUp:
		offset -= page
	case actPageDown:
		offset += page
	case actHome:
		offset = 0
	case actEnd:
		offset = total
	}
	m.layout.helpScroll = offset
	return m, nil
}

// enterCompose focuses the chat panel and opens the message input when sending
// is available. The existing draft, if any, stays in the input.
func (m Model) enterCompose() (tea.Model, tea.Cmd) {
	if m.chat.panel.unsupported || !m.chat.panel.caps.CanSend {
		return m, nil
	}
	if m.warnChatHiddenByWidth() || !m.layout.showChat || m.chatHiddenByWidth() {
		return m, nil
	}
	if !m.chat.hasToken() && (m.chat.session != nil || m.chat.panel.channel != "") {
		m.chat.panel.sendWarning = m.t("chat.warn.no_token")
		return m, nil
	}
	m.setFocus(FocusChat)
	m.chat.composing = true
	return m, nil
}

func (p playbackState) controllable() bool {
	switch p.state {
	case PlayerStatePlaying, PlayerStatePaused, PlayerStateBreak, PlayerStateStarting:
		return true
	default:
		return false
	}
}

// adjustVolume answers a volume key. At a limit the key would change nothing, so
// the bar says so. The command still goes out: the volume here can lag one in
// flight, and the player clamps on its own.
func (m Model) adjustVolume(delta int) (tea.Model, tea.Cmd) {
	if m.playback == nil || !m.player.controllable() {
		return m, nil
	}
	switch {
	case delta > 0 && m.player.volume >= 100:
		m.warnStatus(m.t("player.vol_max"))
	case delta < 0 && m.player.volume <= 0:
		m.warnStatus(m.t("player.vol_min"))
	}
	return m, m.playback.AdjustVolume(delta)
}

func (m Model) updateComposingKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if draft, ok := editText(m.chat.composeDraft, msg); ok {
		m.chat.composeDraft = draft
		m.clearResolvedComposeWarning()
		return m, nil
	}
	switch m.keys.actionFor(modeCompose, msg) {
	case actBack:
		// Esc leaves composition and stays on the chat panel; draft is kept.
		m.chat.composing = false
		return m, nil

	case actSelect:
		return m.submitComposeMessage()
	}
	return m, nil
}

// clearResolvedComposeWarning drops a warning about the draft once the edit fixed
// it, so the footer stops describing a problem the draft no longer has.
func (m *Model) clearResolvedComposeWarning() {
	switch m.chat.panel.sendWarning {
	case "":
	case m.t("chat.warn.too_long"):
		if !m.chat.draftOverLimit() {
			m.chat.panel.sendWarning = ""
		}
	case m.t("chat.warn.command_blocked"):
		if !catalog.IsBlockedCommand(m.chat.composeDraft) {
			m.chat.panel.sendWarning = ""
		}
	}
}
