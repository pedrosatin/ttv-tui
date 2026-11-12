package ui

import (
	"fmt"
	"strings"
)

// statusLead is the message that opens the bar and what came with it.
type statusLead struct {
	primary string
	tone    statusTone
	detail  string
	// context goes to the extras, in the order found.
	context []string
}

// statusParts decides what the bar says for the current state. The main message
// carries what is happening and the next step.
//
// Soft states (ready / category selected) promote a channel-bearing chat status
// to primary so 80 columns keep the name whole. When the lead already names the
// channel (playing/selected), context outranks the duplicate chat line;
// otherwise chat outranks context so the name is not cut behind category/viewers.
func (m Model) statusParts() statusContent {
	lead := m.statusLead()
	c := statusContent{tone: lead.tone, detail: lead.detail}
	primary := lead.primary
	c.keep = m.statusKeep(primary, c.tone)

	chatStatus := m.statusChatLine(primary)
	channelInPrimary := m.chat.primaryNamesChannel(primary)

	// Soft states yield the lead to a channel-bearing chat status so the name
	// is not truncated behind category/ready context on narrow terminals.
	if chatStatus != "" && m.chat.statusHasChannel() && m.softStatusPrimary() && !channelInPrimary {
		c.extra = append(c.extra, primary)
		primary = chatStatus
		chatStatus = ""
		channelInPrimary = true
	}

	c.extra = appendStatusContext(c.extra, chatStatus, channelInPrimary, lead.context)
	c.extra = m.appendPlayerInfo(c.extra)
	c.primary = primary
	return c
}

// statusLead picks the lead by priority: what blocks the screen, one-shot
// notices, the player and loading, then what is selected.
func (m Model) statusLead() statusLead {
	if lead, ok := m.blockingStatusLead(); ok {
		return lead
	}
	if lead, ok := m.noticeStatusLead(); ok {
		return lead
	}
	if lead, ok := m.activityStatusLead(); ok {
		return lead
	}
	return m.selectionStatusLead()
}

// blockingStatusLead covers errors and the states waiting for typed input.
func (m Model) blockingStatusLead() (statusLead, bool) {
	switch {
	case m.player.state == PlayerStateError:
		lead := statusLead{primary: m.playerFailureText(), tone: toneError, detail: m.playerErrorDetail()}
		if m.player.downloadFailed {
			if hint := m.manualInstallHint(); hint != "" {
				lead.context = append(lead.context, hint)
			}
		}
		return lead, true
	case m.notices.ctrlC != "":
		// Ctrl+C hint outranks filter/search prompts so every mode shows it.
		return statusLead{primary: m.notices.ctrlC}, true
	case m.list.err != nil:
		return statusLead{primary: m.listErrorAction(), tone: toneError, detail: m.listErrorDetail()}, true
	case m.list.prefs.menuOpen && m.notices.statusWarns:
		// A choice made in the menu may fail to save; the warning stays over
		// the menu hint, or nobody would see it before the next key clears it.
		return statusLead{primary: m.notices.status, tone: toneWarning}, true
	case m.list.prefs.menuOpen:
		return statusLead{primary: m.t("channel_filter.status_open")}, true
	case m.list.search.active && m.listVisible():
		return statusLead{primary: fmt.Sprintf(m.t("search.prompt"), m.list.search.input)}, true
	case m.list.filtering && m.listVisible():
		if m.filterHasNoMatches() {
			return statusLead{primary: fmt.Sprintf(m.t("filter.search_hint"), strings.TrimSpace(m.list.filter))}, true
		}
		return statusLead{primary: fmt.Sprintf(m.t("filter.prompt"), m.list.filter)}, true
	}
	return statusLead{}, false
}

// noticeStatusLead covers the one-shot notices, each with its own tone.
func (m Model) noticeStatusLead() (statusLead, bool) {
	lead := statusLead{}
	switch {
	case m.notices.auth != "":
		lead.primary = m.notices.auth
		lead.tone = toneWarning
	case m.notices.corrupt != "":
		lead.primary = m.notices.corrupt
		lead.tone = toneWarning
	case m.chat.copyNotice != "":
		lead.primary = m.chat.copyNotice
		if lead.primary == m.t("chat.copy.failed") {
			lead.tone = toneWarning
		}
	case m.notices.status != "":
		lead.primary = m.notices.status
		if m.notices.statusWarns {
			lead.tone = toneWarning
		}
	default:
		return lead, false
	}
	return lead, true
}

// activityStatusLead covers the player and a list being loaded. Each brings the
// active context along.
func (m Model) activityStatusLead() (statusLead, bool) {
	lead := statusLead{}
	switch {
	case m.player.windowNoticeShown():
		lead.primary = m.t("player.window_notice")
		lead.tone = playerTone(m.player.state)
		lead.context = append(lead.context, m.playerStatusBase())
	case m.player.state != PlayerStateNone && m.player.state != PlayerStateEnded:
		lead.primary = m.playerStatusBase()
		lead.tone = playerTone(m.player.state)
	case m.list.loading:
		lead.primary = m.statusLoadingText()
	case m.player.state == PlayerStateEnded:
		lead.primary = m.playerStatusText()
		lead.tone = toneEnded
	default:
		return lead, false
	}
	if ctx := m.activeContextSummary(); ctx != "" {
		lead.context = append(lead.context, ctx)
	}
	return lead, true
}

// statusLoadingText is the lead while the list of the current mode loads.
func (m Model) statusLoadingText() string {
	switch m.Mode() {
	case ModeChannels:
		return m.t("channels.loading")
	case ModeSearch:
		return m.t("search.loading")
	default:
		return m.t("status.loading")
	}
}

// selectionStatusLead covers the resting states: empty list, selected channel,
// search results, selected category and, with none of them, ready.
func (m Model) selectionStatusLead() statusLead {
	lead := statusLead{primary: m.t("status.ready")}
	switch {
	case !m.list.loading && len(m.VisibleItems()) == 0:
		lead.primary = m.t("list.empty_action")
	case m.list.selectedChannel != nil:
		lead.primary = fmt.Sprintf(m.t("channels.selected"), m.list.selectedChannel.DisplayName, m.list.selectedChannel.Login)
		if m.list.selectedChannel.Viewers != nil {
			lead.primary += " " + m.formatChannelViewers(m.list.selectedChannel.Viewers)
		}
		if cat := channelCategoryName(m.list.selectedChannel); cat != "" {
			lead.context = append(lead.context, cat)
		}
	case m.Mode() == ModeSearch && m.list.search.query != "":
		lead.primary = fmt.Sprintf(m.t("search.title_query"), m.list.search.query)
	case m.list.selectedCategory != nil:
		lead.primary = fmt.Sprintf(m.t("categories.selected"), m.list.selectedCategory.Name)
		if m.list.selectedCategory.Viewers != nil {
			lead.primary += " " + m.formatViewers(m.list.selectedCategory.Viewers)
		}
	}
	return lead
}

// statusKeep lists what stays on the bar under any lead: the volume and the
// active filter. The mpv window can change the volume with no other place to
// show it. Only an error outranks the volume, because its detail needs the
// columns.
func (m Model) statusKeep(primary string, tone statusTone) []string {
	volume := m.playerVolumeText()
	if tone == toneError {
		volume = ""
	}
	var keep []string
	for _, s := range []string{volume, m.list.prefs.summary()} {
		if s != "" && !strings.Contains(primary, s) {
			keep = append(keep, s)
		}
	}
	return keep
}

// statusChatLine is the chat status when it adds to the lead. A list error
// leaves it out.
func (m Model) statusChatLine(primary string) string {
	if m.list.err != nil {
		return ""
	}
	if s := m.chatStatusSummary(); s != "" && !strings.Contains(primary, s) {
		return s
	}
	return ""
}

// appendStatusContext orders chat status and context. When the lead already
// names the channel (playing/selected), context is more informative than a
// duplicate "Conectado em #canal". Otherwise the channel-bearing chat status
// outranks context so the name is not cut behind category/viewers.
func appendStatusContext(extra []string, chatStatus string, channelInPrimary bool, context []string) []string {
	if chatStatus == "" {
		return append(extra, context...)
	}
	if channelInPrimary {
		return append(append(extra, context...), chatStatus)
	}
	return append(append(extra, chatStatus), context...)
}

// appendPlayerInfo adds the player's information line. A streamlink log line
// ("[plugins.twitch][info] ...") says nothing to the viewer.
func (m Model) appendPlayerInfo(extra []string) []string {
	rawLog := m.hosted.host != nil && strings.HasPrefix(m.player.infoLine, "[")
	if m.player.state != PlayerStateError && m.player.infoLine != "" && !rawLog {
		extra = append(extra, fmt.Sprintf(m.t("player.info"), m.player.infoLine))
	}
	return extra
}
