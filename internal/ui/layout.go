package ui

// LayoutMode names the visible TUI panel arrangement for the status bar.
// Values match the ticket tokens exactly.
type LayoutMode string

const (
	LayoutSplit LayoutMode = "split" // list + chat
	LayoutChat  LayoutMode = "chat"  // chat only
	LayoutList  LayoutMode = "list"  // list only
	LayoutFocus LayoutMode = "focus" // both hidden (video focus)
)

// LayoutMode returns the current panel arrangement derived from visibility flags.
func (m Model) LayoutMode() LayoutMode {
	switch {
	case m.showList && m.showChat:
		return LayoutSplit
	case !m.showList && m.showChat:
		return LayoutChat
	case m.showList && !m.showChat:
		return LayoutList
	default:
		return LayoutFocus
	}
}

// listVisible reports whether the list panel should occupy columns.
func (m Model) listVisible() bool {
	return m.showList
}

// chatColumnVisible reports whether the chat panel occupies a column (layout
// wants it, a chat session exists, and the terminal is wide enough).
func (m Model) chatColumnVisible() bool {
	return m.showChat && m.hasChatPanel() && !m.chatHiddenByWidth()
}

// shouldShowNarrowChatNotice is true only when width hides chat. Hiding chat
// via layout must not show the "<80 cols" warning.
func (m Model) shouldShowNarrowChatNotice() bool {
	return m.showChat && m.hasChatPanel() && m.chatHiddenByWidth()
}

func (m *Model) cycleLayout() {
	switch m.LayoutMode() {
	case LayoutSplit:
		m.showList, m.showChat = false, true
	case LayoutChat:
		m.showList, m.showChat = true, false
	case LayoutList:
		m.showList, m.showChat = false, false
	default:
		m.showList, m.showChat = true, true
	}
	m.normalizeFocusAfterLayout()
}

func (m *Model) toggleListPanel() {
	m.showList = !m.showList
	m.normalizeFocusAfterLayout()
}

func (m *Model) toggleChatPanel() {
	m.showChat = !m.showChat
	m.normalizeFocusAfterLayout()
}

// normalizeFocusAfterLayout keeps focus and compose coherent with visible panels.
// Esc never changes layout; only the layout keys do.
func (m *Model) normalizeFocusAfterLayout() {
	listOn := m.showList
	chatOn := m.chatColumnVisible()
	if !chatOn {
		m.composing = false
	}
	switch {
	case listOn && chatOn:
		// Keep the user's focus between the two panels.
	case chatOn:
		m.focus = FocusChat
	case listOn:
		m.focus = FocusList
	default:
		// Focus mode: leave underlying focus as-is; Focus() reports list.
		m.composing = false
	}
}
