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
// live, PgUp/PgDn one page of drawn rows up or down, Home oldest page, End/G
// resume live. Mouse is not captured. The compose input stays pinned to the
// panel footer; the buffer keeps its line/byte limits.

// SelectedChatMessage returns the chat message currently selected for actions
// such as copy. Selection is the message on the bottom-most visible row of the
// chat viewport: the newest message while following live, or the message under
// the scroll highlight when history is paused. ok is false when the buffer is
// empty.
func (m Model) SelectedChatMessage() (ChatMessage, bool) {
	if m.chat.panel.buf == nil {
		return ChatMessage{}, false
	}
	msgs := m.ChatMessages()
	if len(msgs) == 0 {
		return ChatMessage{}, false
	}
	idx := m.chat.selectedIndex(len(msgs))
	if idx < 0 || idx >= len(msgs) {
		return ChatMessage{}, false
	}
	return msgs[idx], true
}

// ChatFollowLive reports whether the chat viewport stays pinned to new messages.
func (m Model) ChatFollowLive() bool {
	return m.chat.panel.followLive
}

// ChatPendingNew returns how many messages arrived while the viewport was
// scrolled away from the live bottom. Zero while following live.
func (m Model) ChatPendingNew() int {
	if m.chat.panel.followLive {
		return 0
	}
	return m.chat.panel.pendingNew
}

func (c chatState) selectedIndex(n int) int {
	if n <= 0 {
		return -1
	}
	if c.panel.followLive {
		return n - 1
	}
	idx := c.panel.bottomIdx
	if idx < 0 {
		return 0
	}
	if idx >= n {
		return n - 1
	}
	return idx
}

func (m *Model) resetChatScroll() {
	m.chat.panel.followLive = true
	m.chat.panel.bottomIdx = 0
	m.chat.panel.pendingNew = 0
}

func (m *Model) chatFollowLiveNow() {
	m.chat.panel.followLive = true
	m.chat.panel.pendingNew = 0
	if m.chat.panel.buf != nil && m.chat.panel.buf.Len() > 0 {
		m.chat.panel.bottomIdx = m.chat.panel.buf.Len() - 1
	} else {
		m.chat.panel.bottomIdx = 0
	}
}

// noteChatAppend records a successful buffer append for scroll bookkeeping.
// beforeLen is buf.Len() immediately before Append; appended is whether Append
// returned true.
func (m *Model) noteChatAppend(beforeLen int, appended bool) {
	if m.chat.panel.buf == nil {
		return
	}
	after := m.chat.panel.buf.Len()
	if m.chat.panel.followLive {
		if after > 0 {
			m.chat.panel.bottomIdx = after - 1
		}
		return
	}
	if appended {
		m.chat.panel.pendingNew++
	}
	// Oldest messages may have been evicted to keep buffer limits; shift the
	// anchor so the same logical message stays at the bottom of the viewport.
	evicted := beforeLen - after
	if appended {
		evicted++
	}
	if evicted > 0 {
		m.chat.panel.bottomIdx -= evicted
		if m.chat.panel.bottomIdx < 0 {
			m.chat.panel.bottomIdx = 0
		}
	}
	if after > 0 && m.chat.panel.bottomIdx >= after {
		m.chat.panel.bottomIdx = after - 1
	}
}

// chatViewport returns the rows and the width View draws the chat panel with
// at the current terminal size: the overlay's height with the host, the whole
// body otherwise, and the unsized preview without a terminal size.
func (m Model) chatViewport() (rows, width int) {
	if m.width <= 0 && m.height <= 0 {
		return defaultChatPreviewLines, 0
	}
	available := m.height - 1 - m.footerRows()
	if m.hosted.host != nil && m.width > 0 && m.height > 1 {
		_, chatW, _ := m.hostedWidths()
		return m.chatPanelRows(available), chatW
	}
	if m.height <= 0 {
		available = 20
	}
	_, chatW := m.panelWidths()
	return available, chatW
}

// chatPageSize is how many message rows the chat panel draws now: the panel's
// rows minus the header, the footer and the notices, as renderChatLines lays
// them out. Never below one.
func (m Model) chatPageSize() int {
	budget, _ := m.chatPage()
	return max(budget, 1)
}

// chatPage returns the message rows the chat panel draws now and the width it
// wraps them to.
func (m Model) chatPage() (budget, width int) {
	rows, width := m.chatViewport()
	return m.chatLayout(rows, width).msgBudget, width
}

// chatFillDown returns the newest message index such that msgs[from..index]
// fit together in budget rows, so a page ending there shows all of them whole.
// It is at least from, even for a message taller than the page.
func (m Model) chatFillDown(msgs []ChatMessage, from, budget, maxWidth int) int {
	if from >= len(msgs) {
		return len(msgs) - 1
	}
	rows, last := 0, from
	for i := from; i < len(msgs); i++ {
		// The highlight lead is never wider than the plain one, so measuring
		// without it can only leave a spare row, never hide a message.
		rows += len(m.chatMessageWrapped(msgs[i], false, maxWidth))
		if rows > budget && i > from {
			break
		}
		last = i
	}
	return last
}

// pageChatUp moves one page toward older messages: the message just above the
// oldest one on screen becomes the bottom one, so no message is skipped. A
// message cut at the top becomes the bottom message on the next page; a
// message taller than the page still shows only its last rows. From the
// oldest page it stays on the oldest page, paused.
func (m Model) pageChatUp() Model {
	msgs := m.ChatMessages()
	if len(msgs) == 0 {
		return m
	}
	budget, width := m.chatPage()
	bottom := m.chat.selectedIndex(len(msgs))
	top, cut := m.chatViewportTop(msgs[:bottom+1], budget, width, bottom)
	next := top - 1
	if cut {
		next = top
	}
	if next >= bottom {
		next = bottom - 1
	}
	if next < 0 {
		return m.scrollChatHome()
	}
	return m.scrollChatUp(bottom - next)
}

// pageChatDown moves one page toward live: the page starts at the message just
// below the bottom one and holds as many whole messages as fit. Reaching the
// newest message resumes live.
func (m Model) pageChatDown() Model {
	if m.chat.panel.followLive {
		return m
	}
	msgs := m.ChatMessages()
	if len(msgs) == 0 {
		return m.scrollChatDown(1)
	}
	bottom := m.chat.selectedIndex(len(msgs))
	budget, width := m.chatPage()
	next := m.chatFillDown(msgs, bottom+1, budget, width)
	return m.scrollChatDown(next - bottom)
}

func (m Model) scrollChatUp(by int) Model {
	if by < 1 {
		by = 1
	}
	if m.chat.panel.buf == nil {
		return m
	}
	n := m.chat.panel.buf.Len()
	if n == 0 {
		return m
	}
	if m.chat.panel.followLive {
		m.chat.panel.followLive = false
		m.chat.panel.bottomIdx = n - 1
		m.chat.panel.pendingNew = 0
	}
	m.chat.panel.bottomIdx -= by
	if m.chat.panel.bottomIdx < 0 {
		m.chat.panel.bottomIdx = 0
	}
	return m
}

func (m Model) scrollChatDown(by int) Model {
	if by < 1 {
		by = 1
	}
	if m.chat.panel.followLive || m.chat.panel.buf == nil {
		return m
	}
	n := m.chat.panel.buf.Len()
	if n == 0 {
		m.chatFollowLiveNow()
		return m
	}
	m.chat.panel.bottomIdx += by
	if m.chat.panel.bottomIdx >= n-1 {
		m.chatFollowLiveNow()
		return m
	}
	return m
}

func (m Model) scrollChatHome() Model {
	if m.chat.panel.buf == nil {
		return m
	}
	msgs := m.ChatMessages()
	if len(msgs) == 0 {
		return m
	}
	m.chat.panel.followLive = false
	budget, width := m.chatPage()
	m.chat.panel.bottomIdx = m.chatFillDown(msgs, 0, budget, width)
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

// viewportEnd returns the exclusive end index into msgs for the scrolled
// viewport (messages after this index are hidden below the fold).
func (c chatState) viewportEnd(msgCount int) int {
	if msgCount <= 0 {
		return 0
	}
	if c.panel.followLive {
		return msgCount
	}
	end := c.panel.bottomIdx + 1
	if end > msgCount {
		end = msgCount
	}
	if end < 0 {
		end = 0
	}
	return end
}
