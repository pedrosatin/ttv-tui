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
	channelInPrimary := m.primaryNamesChatChannel(primary)

	// Soft states yield the lead to a channel-bearing chat status so the name
	// is not truncated behind category/ready context on narrow terminals.
	if chatStatus != "" && m.chatStatusHasChannel() && m.softStatusPrimary() && !channelInPrimary {
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
	case m.playerState == PlayerStateError:
		return statusLead{primary: m.playerFailureText(), tone: toneError, detail: m.playerErrorDetail()}, true
	case m.ctrlCNotice != "":
		// Ctrl+C hint outranks filter/search prompts so every mode shows it.
		return statusLead{primary: m.ctrlCNotice}, true
	case m.err != nil:
		return statusLead{primary: m.listErrorAction(), tone: toneError, detail: m.listErrorDetail()}, true
	case m.channelFilterMenu:
		return statusLead{primary: m.t("channel_filter.status_open")}, true
	case m.searching && m.listVisible():
		return statusLead{primary: fmt.Sprintf(m.t("search.prompt"), m.searchInput)}, true
	case m.filtering && m.listVisible():
		if m.filterHasNoMatches() {
			return statusLead{primary: fmt.Sprintf(m.t("filter.search_hint"), strings.TrimSpace(m.filter))}, true
		}
		return statusLead{primary: fmt.Sprintf(m.t("filter.prompt"), m.filter)}, true
	}
	return statusLead{}, false
}

// noticeStatusLead covers the one-shot notices, each with its own tone.
func (m Model) noticeStatusLead() (statusLead, bool) {
	lead := statusLead{}
	switch {
	case m.authNotice != "":
		lead.primary = m.authNotice
		lead.tone = toneWarning
	case m.corruptNotice != "":
		lead.primary = m.corruptNotice
		lead.tone = toneWarning
	case m.copyNotice != "":
		lead.primary = m.copyNotice
		if lead.primary == m.t("chat.copy.failed") {
			lead.tone = toneWarning
		}
	case m.statusNotice != "":
		lead.primary = m.statusNotice
		if m.statusNoticeWarns {
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
	case m.playerWindowNoticeShown():
		lead.primary = m.t("player.window_notice")
		lead.tone = playerTone(m.playerState)
		lead.context = append(lead.context, m.playerStatusBase())
	case m.playerState != PlayerStateNone && m.playerState != PlayerStateEnded:
		lead.primary = m.playerStatusBase()
		lead.tone = playerTone(m.playerState)
	case m.loading:
		lead.primary = m.statusLoadingText()
	case m.playerState == PlayerStateEnded:
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
	case !m.loading && len(m.VisibleItems()) == 0:
		lead.primary = m.t("list.empty_action")
	case m.selectedChannel != nil:
		lead.primary = fmt.Sprintf(m.t("channels.selected"), m.selectedChannel.DisplayName, m.selectedChannel.Login)
		if m.selectedChannel.Viewers != nil {
			lead.primary += " " + m.formatChannelViewers(m.selectedChannel.Viewers)
		}
		if cat := channelCategoryName(m.selectedChannel); cat != "" {
			lead.context = append(lead.context, cat)
		}
	case m.Mode() == ModeSearch && m.searchQuery != "":
		lead.primary = fmt.Sprintf(m.t("search.title_query"), m.searchQuery)
	case m.selectedCategory != nil:
		lead.primary = fmt.Sprintf(m.t("categories.selected"), m.selectedCategory.Name)
		if m.selectedCategory.Viewers != nil {
			lead.primary += " " + m.formatViewers(m.selectedCategory.Viewers)
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
	for _, s := range []string{volume, m.activeChannelFilterSummary()} {
		if s != "" && !strings.Contains(primary, s) {
			keep = append(keep, s)
		}
	}
	return keep
}

// statusChatLine is the chat status when it adds to the lead. A list error
// leaves it out.
func (m Model) statusChatLine(primary string) string {
	if m.err != nil {
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
	rawLog := m.videoHost != nil && strings.HasPrefix(m.playerInfoLine, "[")
	if m.playerState != PlayerStateError && m.playerInfoLine != "" && !rawLog {
		extra = append(extra, fmt.Sprintf(m.t("player.info"), m.playerInfoLine))
	}
	return extra
}
