package ui

// LayoutMode names the visible TUI panel arrangement. The values are internal
// identifiers; the screen shows the localized label (layoutModeLabel).
type LayoutMode string

const (
	LayoutSplit LayoutMode = "split" // list + chat
	LayoutChat  LayoutMode = "chat"  // chat only
	LayoutList  LayoutMode = "list"  // list only
	LayoutFocus LayoutMode = "focus" // both hidden (video focus)
)

// LayoutMode returns the current panel arrangement derived from visibility flags.
func (m Model) LayoutMode() LayoutMode {
	listOn := m.listVisible()
	switch {
	case listOn && m.showChat:
		return LayoutSplit
	case !listOn && m.showChat:
		return LayoutChat
	case listOn && !m.showChat:
		return LayoutList
	default:
		return LayoutFocus
	}
}

// layoutModeLabel is the localized name of a layout mode, the same word the
// help line (help.layout) uses. The LayoutMode values stay language-neutral
// identifiers; only the catalog label reaches the screen.
func (m Model) layoutModeLabel(mode LayoutMode) string {
	switch mode {
	case LayoutSplit:
		return m.t("layout.mode.split")
	case LayoutChat:
		return m.t("layout.mode.chat")
	case LayoutList:
		return m.t("layout.mode.list")
	default:
		return m.t("layout.mode.focus")
	}
}

// listVisible reports whether the list panel should occupy columns. With no chat
// the list is all there is to draw, so no flag can hide it and leave the screen
// empty.
func (m Model) listVisible() bool {
	return m.showList || !m.hasChatPanel()
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

// warnChatHiddenByWidth answers a chat key the split cannot serve, and reports
// whether it did. The on-screen notice alone reads as a key that did nothing.
func (m *Model) warnChatHiddenByWidth() bool {
	if !m.shouldShowNarrowChatNotice() {
		return false
	}
	m.warnStatus(m.t("chat.hidden_narrow_key"))
	return true
}

// keepListWithoutChat answers a layout key before any chat exists: every mode
// would draw the same list or nothing, so the panels stay and the bar says why.
func (m *Model) keepListWithoutChat() {
	m.showList, m.showChat = true, true
	m.warnStatus(m.t("layout.no_chat"))
	m.normalizeFocusAfterLayout()
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

// toggleChatPanel shows or hides the chat. In a terminal too narrow for the split,
// showing it means chat-only, the one mode that can draw it there.
func (m *Model) toggleChatPanel() {
	if m.chatHiddenByWidth() {
		m.showList, m.showChat = false, true
	} else {
		m.showChat = !m.showChat
	}
	m.normalizeFocusAfterLayout()
}

// normalizeFocusAfterLayout keeps focus and compose coherent with visible panels.
// Esc never changes layout; only the layout keys do.
func (m *Model) normalizeFocusAfterLayout() {
	listOn := m.listVisible()
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
