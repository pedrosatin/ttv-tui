package ui

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/chat"
)

const (
	// osc52MaxBytes caps the plaintext copied via OSC 52. Chat bodies are far
	// smaller; the ceiling keeps a runaway payload from flooding the terminal.
	osc52MaxBytes = 100 * 1024
	// copyNoticeTTL is how long the "Copied: …" status stays visible.
	copyNoticeTTL = 2 * time.Second
	// copyPreviewRunes is the max runes shown after the em dash in the notice.
	copyPreviewRunes = 40
)

// osc52Out receives OSC 52 sequences. Stderr shares the TTY with the alt-screen
// renderer on stdout without racing Bubble Tea's frame writes. Tests replace it.
var osc52Out io.Writer = os.Stderr

// copyNoticeClearMsg dismisses a copy notice after copyNoticeTTL when gen still
// matches the notice that scheduled the tick.
type copyNoticeClearMsg struct {
	gen uint64
}

// formatChatCopyText returns the clipboard payload for msg: "author: text" for
// chat lines, or the body alone for system/notice lines. Layout markers from
// the panel (selection lead, │, padding) are never included.
func formatChatCopyText(msg ChatMessage) string {
	body := strings.TrimSpace(sanitizeChatText(msg.Body))
	switch msg.Kind {
	case chat.MessageSystem, chat.MessageNotice:
		return truncateUTF8Bytes(body, osc52MaxBytes)
	}
	name := strings.TrimSpace(sanitizeChatText(msg.DisplayName))
	if name == "" {
		name = strings.TrimSpace(sanitizeChatText(msg.User))
	}
	if name == "" {
		return truncateUTF8Bytes(body, osc52MaxBytes)
	}
	if body == "" {
		return truncateUTF8Bytes(name, osc52MaxBytes)
	}
	return truncateUTF8Bytes(name+": "+body, osc52MaxBytes)
}

// osc52Prefix / osc52BEL are built from bytes so the i18n loose-string linter
// does not treat the OSC 52 control sequence as user-facing copy.
var (
	osc52Prefix = string([]byte{0x1b, ']', '5', '2', ';', 'c', ';'})
	osc52BEL    = string([]byte{0x07})
)

// osc52Sequence builds the OSC 52 set-clipboard escape for plaintext.
// Form: ESC ] 52 ; c ; <base64> BEL
func osc52Sequence(plaintext string) string {
	enc := base64.StdEncoding.EncodeToString([]byte(plaintext))
	return osc52Prefix + enc + osc52BEL
}

func truncateUTF8Bytes(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	trunc := s[:maxBytes]
	for len(trunc) > 0 && !utf8.ValidString(trunc) {
		trunc = trunc[:len(trunc)-1]
	}
	return trunc
}

func truncateRunes(s string, max int) string {
	if max <= 0 || s == "" {
		return ""
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n >= max {
			break
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}

func (m Model) now() time.Time {
	if m.nowFunc != nil {
		return m.nowFunc()
	}
	return time.Now()
}

// CopyNotice returns the transient "Copied: …" status text, if any.
func (m Model) CopyNotice() string {
	return m.copyNotice
}

// copyChatMessage copies the selected chat message (or the latest while
// following live) via OSC 52 and shows a short-lived status notice.
func (m Model) copyChatMessage() (tea.Model, tea.Cmd) {
	msg, ok := m.SelectedChatMessage()
	if !ok {
		return m, nil
	}
	plaintext := formatChatCopyText(msg)
	if plaintext == "" {
		return m, nil
	}

	m.copyNoticeGen++
	gen := m.copyNoticeGen
	m.copyNotice = m.formatCopyNotice(msg, plaintext)
	m.copyNoticeUntil = m.now().Add(copyNoticeTTL)

	seq := osc52Sequence(plaintext)
	writeCmd := func() tea.Msg {
		_, _ = io.WriteString(osc52Out, seq)
		return nil
	}
	clearCmd := tea.Tick(copyNoticeTTL, func(time.Time) tea.Msg {
		return copyNoticeClearMsg{gen: gen}
	})
	return m, tea.Batch(writeCmd, clearCmd)
}

func (m Model) formatCopyNotice(msg ChatMessage, plaintext string) string {
	preview := truncateRunes(plaintext, copyPreviewRunes)
	name := strings.TrimSpace(sanitizeChatText(msg.DisplayName))
	if name == "" {
		name = strings.TrimSpace(sanitizeChatText(msg.User))
	}
	switch msg.Kind {
	case chat.MessageSystem, chat.MessageNotice:
		return fmt.Sprintf(m.t("chat.copy.done_text"), preview)
	}
	if name == "" {
		return fmt.Sprintf(m.t("chat.copy.done_text"), preview)
	}
	bodyPreview := truncateRunes(strings.TrimSpace(sanitizeChatText(msg.Body)), copyPreviewRunes)
	if bodyPreview == "" {
		return fmt.Sprintf(m.t("chat.copy.done_text"), name)
	}
	return fmt.Sprintf(m.t("chat.copy.done"), name, bodyPreview)
}

func (m Model) clearCopyNoticeIfExpired(gen uint64) Model {
	if gen != m.copyNoticeGen {
		return m
	}
	// Injectable clock: a clear tick that arrives early (or a test advancing
	// time) only dismisses once now reaches copyNoticeUntil.
	if !m.copyNoticeUntil.IsZero() && m.now().Before(m.copyNoticeUntil) {
		return m
	}
	m.copyNotice = ""
	m.copyNoticeUntil = time.Time{}
	return m
}
