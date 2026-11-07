package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// Chat scroll API (ticket 06 / base for v1.4 copy):
//
//   SelectedChatMessage — message on the bottom-most visible row (the scroll
//                         highlight). While following live this is the newest
//                         message. Ticket 05 of v1.4 copies this value.
//   ChatFollowLive      — whether the viewport stays pinned to new arrivals.
//   ChatPendingNew      — how many messages arrived while scrolled away from
//                         the live bottom (drives the "N new ↓" notice).
//
// Scroll keys (chat focus, not composing): ↑/k one message up, ↓/j one toward
// live, PgUp one page up, Home oldest page, End/G resume live. Mouse is not
// captured. The compose input stays pinned to the panel footer; the buffer
// keeps its line/byte limits.

// SelectedChatMessage returns the chat message currently selected for actions
// such as copy. Selection is the message on the bottom-most visible row of the
// chat viewport: the newest message while following live, or the message under
// the scroll highlight when history is paused. ok is false when the buffer is
// empty.
func (m Model) SelectedChatMessage() (ChatMessage, bool) {
	if m.chatPanel.buf == nil {
		return ChatMessage{}, false
	}
	msgs := m.ChatMessages()
	if len(msgs) == 0 {
		return ChatMessage{}, false
	}
	idx := m.chatSelectedIndex(len(msgs))
	if idx < 0 || idx >= len(msgs) {
		return ChatMessage{}, false
	}
	return msgs[idx], true
}

// ChatFollowLive reports whether the chat viewport stays pinned to new messages.
func (m Model) ChatFollowLive() bool {
	return m.chatPanel.followLive
}

// ChatPendingNew returns how many messages arrived while the viewport was
// scrolled away from the live bottom. Zero while following live.
func (m Model) ChatPendingNew() int {
	if m.chatPanel.followLive {
		return 0
	}
	return m.chatPanel.pendingNew
}

func (m Model) chatSelectedIndex(n int) int {
	if n <= 0 {
		return -1
	}
	if m.chatPanel.followLive {
		return n - 1
	}
	idx := m.chatPanel.bottomIdx
	if idx < 0 {
		return 0
	}
	if idx >= n {
		return n - 1
	}
	return idx
}

func (m *Model) resetChatScroll() {
	m.chatPanel.followLive = true
	m.chatPanel.bottomIdx = 0
	m.chatPanel.pendingNew = 0
}

func (m *Model) chatFollowLiveNow() {
	m.chatPanel.followLive = true
	m.chatPanel.pendingNew = 0
	if m.chatPanel.buf != nil && m.chatPanel.buf.Len() > 0 {
		m.chatPanel.bottomIdx = m.chatPanel.buf.Len() - 1
	} else {
		m.chatPanel.bottomIdx = 0
	}
}

// noteChatAppend records a successful buffer append for scroll bookkeeping.
// beforeLen is buf.Len() immediately before Append; appended is whether Append
// returned true.
func (m *Model) noteChatAppend(beforeLen int, appended bool) {
	if m.chatPanel.buf == nil {
		return
	}
	after := m.chatPanel.buf.Len()
	if m.chatPanel.followLive {
		if after > 0 {
			m.chatPanel.bottomIdx = after - 1
		}
		return
	}
	if appended {
		m.chatPanel.pendingNew++
	}
	// Oldest messages may have been evicted to keep buffer limits; shift the
	// anchor so the same logical message stays at the bottom of the viewport.
	evicted := beforeLen - after
	if appended {
		evicted++
	}
	if evicted > 0 {
		m.chatPanel.bottomIdx -= evicted
		if m.chatPanel.bottomIdx < 0 {
			m.chatPanel.bottomIdx = 0
		}
	}
	if after > 0 && m.chatPanel.bottomIdx >= after {
		m.chatPanel.bottomIdx = after - 1
	}
}

func (m Model) chatPageSize() int {
	// Approximate the message-area row budget without relying on View mutations
	// (tea.Model.View receives a copy). Header ~2, footer ~2, status bar 1.
	height := m.height
	if m.videoBodyShown() {
		height -= m.videoRows(max(height-1, 0))
	}
	if height > 6 {
		return height - 6
	}
	return defaultChatPreviewLines
}

func (m Model) chatHomeBottomIdx(msgs []ChatMessage, budget, maxWidth int) int {
	if len(msgs) == 0 || budget <= 0 {
		return 0
	}
	rows := 0
	last := 0
	for i, msg := range msgs {
		// Ignore selection styling when estimating the home page; lead width
		// must stay stable so the first page fills the budget predictably.
		lead, content := m.formatChatMessageParts(msg, false)
		rows += len(wrapChatRow(lead, content, maxWidth))
		last = i
		if rows >= budget {
			break
		}
	}
	return last
}

func (m Model) scrollChatUp(by int) Model {
	if by < 1 {
		by = 1
	}
	if m.chatPanel.buf == nil {
		return m
	}
	n := m.chatPanel.buf.Len()
	if n == 0 {
		return m
	}
	if m.chatPanel.followLive {
		m.chatPanel.followLive = false
		m.chatPanel.bottomIdx = n - 1
		m.chatPanel.pendingNew = 0
	}
	m.chatPanel.bottomIdx -= by
	if m.chatPanel.bottomIdx < 0 {
		m.chatPanel.bottomIdx = 0
	}
	return m
}

func (m Model) scrollChatDown(by int) Model {
	if by < 1 {
		by = 1
	}
	if m.chatPanel.followLive || m.chatPanel.buf == nil {
		return m
	}
	n := m.chatPanel.buf.Len()
	if n == 0 {
		m.chatFollowLiveNow()
		return m
	}
	m.chatPanel.bottomIdx += by
	if m.chatPanel.bottomIdx >= n-1 {
		m.chatFollowLiveNow()
		return m
	}
	return m
}

func (m Model) scrollChatHome() Model {
	if m.chatPanel.buf == nil {
		return m
	}
	msgs := m.ChatMessages()
	if len(msgs) == 0 {
		return m
	}
	m.chatPanel.followLive = false
	_, chatW := m.panelWidths()
	budget := m.chatPageSize()
	m.chatPanel.bottomIdx = m.chatHomeBottomIdx(msgs, budget, chatW)
	return m
}

func (m Model) renderChatPendingNotice(maxWidth int) string {
	n := m.ChatPendingNew()
	if n <= 0 {
		return ""
	}
	text := fmt.Sprintf(m.t("chat.scroll.new"), n)
	line := TruncateVisual("  "+text, maxWidth)
	return lipgloss.NewStyle().Faint(true).Render(line)
}

// chatViewportEnd returns the exclusive end index into msgs for the scrolled
// viewport (messages after this index are hidden below the fold).
func (m Model) chatViewportEnd(msgCount int) int {
	if msgCount <= 0 {
		return 0
	}
	if m.chatPanel.followLive {
		return msgCount
	}
	end := m.chatPanel.bottomIdx + 1
	if end > msgCount {
		end = msgCount
	}
	if end < 0 {
		end = 0
	}
	return end
}
