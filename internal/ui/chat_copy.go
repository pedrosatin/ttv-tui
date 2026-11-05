package ui

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
	"github.com/pedrosatin/ttv-tui/internal/chat"
)

const (
	// osc52MaxBytes caps the plaintext copied to the clipboard. Chat bodies are
	// far smaller; the ceiling keeps a runaway payload from flooding the terminal
	// or a clipboard helper.
	osc52MaxBytes = 100 * 1024
	// copyNoticeTTL is how long the copy status notice stays visible.
	copyNoticeTTL = 2 * time.Second
	// copyPreviewRunes is the max runes shown after the em dash in the notice.
	copyPreviewRunes = 40

	clipboardMethodTmux   = "tmux"
	clipboardMethodWlCopy = "wl-copy"
	clipboardMethodXclip  = "xclip"
	clipboardMethodOSC52  = "OSC 52"

	// clipboardHelperTimeout bounds clipboard helpers so a stuck helper cannot
	// freeze the yank path; the next clipboard path is then tried.
	clipboardHelperTimeout = 3 * time.Second
)

// osc52Out receives OSC 52 sequences. Stderr shares the TTY with the alt-screen
// renderer on stdout without racing Bubble Tea's frame writes. Tests replace it.
var osc52Out io.Writer = os.Stderr

// clipboardLookPath / clipboardGetenv are the stdlib hooks used by
// writeClipboard. Tests may replace them; production keeps LookPath and Getenv.
var (
	clipboardLookPath = exec.LookPath
	clipboardGetenv   = os.Getenv
)

// osc52Prefix / osc52BEL are built from bytes so the i18n loose-string linter
// does not treat the OSC 52 control sequence as user-facing copy.
var (
	osc52Prefix = string([]byte{0x1b, ']', '5', '2', ';', 'c', ';'})
	osc52BEL    = string([]byte{0x07})
)

// copyNoticeClearMsg dismisses a copy notice after copyNoticeTTL when gen still
// matches the notice that scheduled the tick.
type copyNoticeClearMsg struct {
	gen uint64
}

// copyResultMsg is the outcome of writeClipboard for one yank (gen matches the
// copyNoticeGen that scheduled the attempt).
type copyResultMsg struct {
	gen       uint64
	method    string
	err       error
	chatMsg   ChatMessage
	plaintext string
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

// CopyNotice returns the transient copy status text, if any.
func (m Model) CopyNotice() string {
	return m.copyNotice
}

// writeClipboard tries tmux first, then local display helpers outside SSH,
// then OSC 52. Helper exit zero confirms a copy; OSC 52 only confirms sending.
// Failed helpers advance to the next path.
func writeClipboard(plaintext string) (string, error) {
	if clipboardGetenv("TMUX") != "" {
		if _, err := clipboardLookPath(clipboardMethodTmux); err == nil {
			if err := runStdinClipboard(clipboardMethodTmux, []string{"load-buffer", "-w", "-"}, plaintext); err == nil {
				return clipboardMethodTmux, nil
			}
		}
	}
	if clipboardGetenv("SSH_TTY") == "" && clipboardGetenv("SSH_CONNECTION") == "" {
		if method := writeDisplayClipboard(plaintext); method != "" {
			return method, nil
		}
	}
	// A redirected stderr cannot deliver OSC 52 to the terminal. Non-file
	// writers are injected by tests.
	if f, ok := osc52Out.(*os.File); ok && !term.IsTerminal(f.Fd()) {
		return "", os.ErrInvalid
	}
	if _, err := io.WriteString(osc52Out, osc52Sequence(plaintext)); err != nil {
		return "", err
	}
	return clipboardMethodOSC52, nil
}

func writeDisplayClipboard(plaintext string) string {
	if clipboardGetenv("WAYLAND_DISPLAY") != "" {
		if _, err := clipboardLookPath(clipboardMethodWlCopy); err == nil {
			if err := runStdinClipboard(clipboardMethodWlCopy, nil, plaintext); err == nil {
				return clipboardMethodWlCopy
			}
		}
	}
	if clipboardGetenv("DISPLAY") != "" {
		if _, err := clipboardLookPath(clipboardMethodXclip); err == nil {
			if err := runStdinClipboard(clipboardMethodXclip, []string{"-selection", "clipboard"}, plaintext); err == nil {
				return clipboardMethodXclip
			}
		}
	}
	return ""
}

func runStdinClipboard(name string, args []string, plaintext string) error {
	ctx, cancel := context.WithTimeout(context.Background(), clipboardHelperTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(plaintext)
	// Leave Stdout/Stderr nil so os/exec attaches /dev/null as *os.File.
	// io.Discard would use a pipe; wl-copy/xclip fork and the child keeps the
	// write end open, so Wait would hang forever on the copy goroutine.
	return cmd.Run()
}

// copyChatMessage copies the selected chat message (or the latest while
// following live) and shows a short-lived status notice naming the path used.
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
	chatMsg := msg
	return m, func() tea.Msg {
		method, err := writeClipboard(plaintext)
		return copyResultMsg{
			gen:       gen,
			method:    method,
			err:       err,
			chatMsg:   chatMsg,
			plaintext: plaintext,
		}
	}
}

func (m Model) applyCopyResult(msg copyResultMsg) (Model, tea.Cmd) {
	if msg.gen != m.copyNoticeGen {
		return m, nil
	}
	m.copyNoticeUntil = m.now().Add(copyNoticeTTL)
	if msg.err != nil {
		m.copyNotice = m.t("chat.copy.failed")
	} else {
		m.copyNotice = m.formatCopyNotice(msg.chatMsg, msg.plaintext, msg.method)
	}
	gen := msg.gen
	return m, tea.Tick(copyNoticeTTL, func(time.Time) tea.Msg {
		return copyNoticeClearMsg{gen: gen}
	})
}

func (m Model) formatCopyNotice(msg ChatMessage, plaintext, method string) string {
	preview := truncateRunes(plaintext, copyPreviewRunes)
	if method == clipboardMethodOSC52 {
		return fmt.Sprintf(m.t("chat.copy.sent_terminal"), preview)
	}
	name := strings.TrimSpace(sanitizeChatText(msg.DisplayName))
	if name == "" {
		name = strings.TrimSpace(sanitizeChatText(msg.User))
	}
	switch msg.Kind {
	case chat.MessageSystem, chat.MessageNotice:
		return fmt.Sprintf(m.t("chat.copy.done_text_via"), method, preview)
	}
	if name == "" {
		return fmt.Sprintf(m.t("chat.copy.done_text_via"), method, preview)
	}
	bodyPreview := truncateRunes(strings.TrimSpace(sanitizeChatText(msg.Body)), copyPreviewRunes)
	if bodyPreview == "" {
		return fmt.Sprintf(m.t("chat.copy.done_text_via"), method, name)
	}
	return fmt.Sprintf(m.t("chat.copy.done_via"), method, name, bodyPreview)
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
