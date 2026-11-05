package ui

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

func decodeOSC52Payload(seq string) (string, error) {
	if !strings.HasPrefix(seq, osc52Prefix) {
		return "", errOSC52Shape
	}
	rest := strings.TrimPrefix(seq, osc52Prefix)
	rest = strings.TrimSuffix(rest, osc52BEL)
	rest = strings.TrimSuffix(rest, string([]byte{0x1b, '\\'}))
	raw, err := base64.StdEncoding.DecodeString(rest)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

var errOSC52Shape = errString("osc52 shape")

type errString string

func (e errString) Error() string { return string(e) }

type errWriter struct{ err error }

func (w errWriter) Write([]byte) (int, error) { return 0, w.err }

// forceOSCClipboardEnv clears display env and isolates PATH so writeClipboard
// skips helpers and cannot resolve real wl-copy/xclip binaries.
func forceOSCClipboardEnv(t *testing.T) {
	t.Helper()
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", "")
	t.Setenv("PATH", t.TempDir())
}

// flushCopy runs the copy cmd, feeds copyResultMsg back into Update, and
// ignores the clear tick (which would sleep for copyNoticeTTL).
func flushCopy(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if c == nil {
				continue
			}
			inner := runCmdWithTimeout(t, c)
			if inner == nil {
				continue
			}
			updated, next := m.Update(inner)
			m = updated.(Model)
			_ = next
		}
		return m
	}
	updated, next := m.Update(msg)
	m = updated.(Model)
	_ = next
	return m
}

func runCmdWithTimeout(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	done := make(chan tea.Msg, 1)
	go func() {
		done <- cmd()
	}()
	select {
	case msg := <-done:
		return msg
	case <-time.After(30 * time.Millisecond):
		// tea.Tick blocks until TTL; leave it.
		return nil
	}
}

func writeExecScript(t *testing.T, dir, name, body string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake helpers are #!/bin/sh scripts")
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// pathWithFakeBins puts dir first so fake wl-copy/xclip win, while keeping
// /usr/bin:/bin so shell scripts can still resolve cat and friends.
func pathWithFakeBins(dir string) string {
	return dir + string(os.PathListSeparator) + "/usr/bin:/bin"
}

func TestFormatChatCopyText_AuthorAndBody(t *testing.T) {
	got := formatChatCopyText(ChatMessage{
		Kind:        chat.MessageChat,
		DisplayName: "Gaules",
		User:        "gaules",
		Body:        "salve tribo",
	})
	if got != "Gaules: salve tribo" {
		t.Fatalf("got %q, want %q", got, "Gaules: salve tribo")
	}
}

func TestFormatChatCopyText_SystemIsBodyOnly(t *testing.T) {
	got := formatChatCopyText(ChatMessage{
		Kind: chat.MessageSystem,
		Body: "Chat cleared by a moderator",
	})
	if got != "Chat cleared by a moderator" {
		t.Fatalf("system copy = %q", got)
	}
	got = formatChatCopyText(ChatMessage{
		Kind: chat.MessageNotice,
		Body: "This room is now in slow mode",
	})
	if got != "This room is now in slow mode" {
		t.Fatalf("notice copy = %q", got)
	}
}

func TestOSC52PayloadDecodesToAuthorText(t *testing.T) {
	plain := formatChatCopyText(ChatMessage{
		Kind:        chat.MessageChat,
		DisplayName: "autor",
		Body:        "texto",
	})
	if plain != "autor: texto" {
		t.Fatalf("plaintext = %q", plain)
	}
	seq := osc52Sequence(plain)
	if !strings.HasPrefix(seq, osc52Prefix) || !strings.HasSuffix(seq, osc52BEL) {
		t.Fatalf("bad OSC shape: %q", seq)
	}
	got, err := decodeOSC52Payload(seq)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got != "autor: texto" {
		t.Fatalf("decoded = %q, want autor: texto", got)
	}
}

func TestCopyDoesNotIncludeLayoutJunk(t *testing.T) {
	m := scrollFixture(t, i18n.LangEN, 5)
	msg, ok := m.SelectedChatMessage()
	if !ok {
		t.Fatal("expected selection")
	}
	plain := formatChatCopyText(msg)
	for _, junk := range []string{"│", "▌", "  ", "\n", "\t"} {
		if junk == "  " {
			// author: text has no double-space padding from the list column
			if strings.Contains(plain, "  ") {
				t.Fatalf("payload has layout padding: %q", plain)
			}
			continue
		}
		if strings.Contains(plain, junk) {
			t.Fatalf("payload %q contains layout junk %q", plain, junk)
		}
	}
	view := m.View()
	if !strings.Contains(view, panelSeparator) && !strings.Contains(ansi.Strip(view), "│") {
		// Side-by-side layout should show the separator in the full view; the
		// copy payload must not.
		t.Log("view may be single-panel in fixture; payload check still stands")
	}
	if strings.Contains(plain, "│") || strings.Contains(plain, "▌") {
		t.Fatalf("payload leaked selection/layout markers: %q", plain)
	}
	if plain != "User: msg 4" {
		t.Fatalf("payload = %q, want User: msg 4", plain)
	}
}

func TestCopyY_LiveCopiesLastMessageAndWritesOSC(t *testing.T) {
	forceOSCClipboardEnv(t)
	var buf bytes.Buffer
	prev := osc52Out
	osc52Out = &buf
	t.Cleanup(func() { osc52Out = prev })

	m := scrollFixture(t, i18n.LangEN, 3)
	if !m.ChatFollowLive() {
		t.Fatal("expected live follow")
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected copy cmd")
	}
	m = flushCopy(t, m, cmd)

	got, err := decodeOSC52Payload(buf.String())
	if err != nil {
		t.Fatalf("decode OSC from stderr writer: %v (raw=%q)", err, buf.String())
	}
	if got != "User: msg 2" {
		t.Fatalf("copied %q, want last live message User: msg 2", got)
	}
	wantNotice := "Copied via OSC 52: User — msg 2"
	if m.CopyNotice() != wantNotice {
		t.Fatalf("notice = %q, want %q", m.CopyNotice(), wantNotice)
	}
	if !strings.Contains(ansi.Strip(m.View()), wantNotice) {
		t.Fatalf("view missing copy notice:\n%s", ansi.Strip(m.View()))
	}
}

func TestCopyY_ScrolledCopiesSelection(t *testing.T) {
	forceOSCClipboardEnv(t)
	var buf bytes.Buffer
	prev := osc52Out
	osc52Out = &buf
	t.Cleanup(func() { osc52Out = prev })

	m := scrollFixture(t, i18n.LangEN, 10)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(Model)
	sel, ok := m.SelectedChatMessage()
	if !ok || sel.Body != "msg 7" {
		t.Fatalf("selection = %+v", sel)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(Model)
	m = flushCopy(t, m, cmd)
	got, err := decodeOSC52Payload(buf.String())
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got != "User: msg 7" {
		t.Fatalf("copied %q, want selected User: msg 7", got)
	}
	if !strings.Contains(m.CopyNotice(), "msg 7") || !strings.Contains(m.CopyNotice(), "OSC 52") {
		t.Fatalf("notice = %q", m.CopyNotice())
	}
}

func TestCopyY_SystemMessage(t *testing.T) {
	forceOSCClipboardEnv(t)
	var buf bytes.Buffer
	prev := osc52Out
	osc52Out = &buf
	t.Cleanup(func() { osc52Out = prev })

	m := scrollFixture(t, i18n.LangEN, 0)
	updated, _ := m.Update(ChatEventMsg{
		Generation: 1,
		Channel:    "gaules",
		Messages: []ChatMessage{{
			Generation: 1,
			Kind:       chat.MessageSystem,
			ID:         "sys1",
			Body:       "gaules timed out for 10s",
		}},
	})
	m = updated.(Model)
	m.focus = FocusChat

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(Model)
	m = flushCopy(t, m, cmd)
	got, err := decodeOSC52Payload(buf.String())
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got != "gaules timed out for 10s" {
		t.Fatalf("system payload = %q", got)
	}
	if !strings.HasPrefix(m.CopyNotice(), "Copied via OSC 52: ") {
		t.Fatalf("notice = %q", m.CopyNotice())
	}
	if strings.Contains(got, "[mod]") || strings.Contains(got, "▌") {
		t.Fatalf("system payload has UI chrome: %q", got)
	}
}

func TestCopyNoticeExpiresWithInjectedClock(t *testing.T) {
	forceOSCClipboardEnv(t)
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	now := base
	m := scrollFixture(t, i18n.LangPTBR, 2)
	m.nowFunc = func() time.Time { return now }

	prev := osc52Out
	osc52Out = ioDiscard{}
	t.Cleanup(func() { osc52Out = prev })

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(Model)
	m = flushCopy(t, m, cmd)
	if m.CopyNotice() == "" {
		t.Fatal("expected copy notice")
	}
	want := "Copiado via OSC 52: User — msg 1"
	if m.CopyNotice() != want {
		t.Fatalf("pt-BR notice = %q, want %q", m.CopyNotice(), want)
	}
	gen := m.copyNoticeGen

	// Clear tick before TTL must keep the notice when the clock is frozen.
	updated, _ = m.Update(copyNoticeClearMsg{gen: gen})
	m = updated.(Model)
	if m.CopyNotice() == "" {
		t.Fatal("notice cleared before injected clock reached TTL")
	}

	now = base.Add(copyNoticeTTL)
	updated, _ = m.Update(copyNoticeClearMsg{gen: gen})
	m = updated.(Model)
	if m.CopyNotice() != "" {
		t.Fatalf("notice still visible after TTL: %q", m.CopyNotice())
	}
}

func TestCopyY_IgnoredOnListFocus(t *testing.T) {
	forceOSCClipboardEnv(t)
	var buf bytes.Buffer
	prev := osc52Out
	osc52Out = &buf
	t.Cleanup(func() { osc52Out = prev })

	m := scrollFixture(t, i18n.LangEN, 2)
	m.focus = FocusList
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(Model)
	if cmd != nil {
		m = flushCopy(t, m, cmd)
	}
	if buf.Len() != 0 {
		t.Fatalf("list focus must not copy, wrote %q", buf.String())
	}
	if m.CopyNotice() != "" {
		t.Fatalf("unexpected notice %q", m.CopyNotice())
	}
}

func TestCopyI18nBothLanguages(t *testing.T) {
	forceOSCClipboardEnv(t)
	cases := []struct {
		lang string
		want string
	}{
		{i18n.LangEN, "Copied via OSC 52: User — msg 0"},
		{i18n.LangPTBR, "Copiado via OSC 52: User — msg 0"},
	}
	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			prev := osc52Out
			osc52Out = ioDiscard{}
			t.Cleanup(func() { osc52Out = prev })

			m := scrollFixture(t, tc.lang, 1)
			updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
			m = updated.(Model)
			m = flushCopy(t, m, cmd)
			if m.CopyNotice() != tc.want {
				t.Fatalf("notice = %q, want %q", m.CopyNotice(), tc.want)
			}
			if m.t("chat.copy.done_via") == "chat.copy.done_via" || m.t("chat.copy.done_text_via") == "chat.copy.done_text_via" {
				t.Fatal("missing i18n keys")
			}
			if m.t("chat.copy.failed") == "chat.copy.failed" {
				t.Fatal("missing chat.copy.failed")
			}
		})
	}
}

func TestCopyNoticeVisibleForPTYCapture(t *testing.T) {
	forceOSCClipboardEnv(t)
	prev := osc52Out
	osc52Out = ioDiscard{}
	t.Cleanup(func() { osc52Out = prev })

	m := scrollFixture(t, i18n.LangPTBR, 1)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(Model)
	m = flushCopy(t, m, cmd)
	view := ansi.Strip(m.View())
	t.Logf("PTY capture (ansi-stripped View after y):\n%s", view)
	if !strings.Contains(view, "Copiado via OSC 52: User — msg 0") {
		t.Fatalf("missing copy notice in view")
	}
}

func TestCopyEmptyBufferNoop(t *testing.T) {
	forceOSCClipboardEnv(t)
	var buf bytes.Buffer
	prev := osc52Out
	osc52Out = &buf
	t.Cleanup(func() { osc52Out = prev })

	m := scrollFixture(t, i18n.LangEN, 0)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(Model)
	if cmd != nil {
		m = flushCopy(t, m, cmd)
	}
	if buf.Len() != 0 || m.CopyNotice() != "" {
		t.Fatalf("empty buffer must not copy: buf=%q notice=%q", buf.String(), m.CopyNotice())
	}
}

func TestCopyUsesWlCopyWhenWayland(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "copied.txt")
	writeExecScript(t, dir, "wl-copy", "#!/bin/sh\n/bin/cat > \""+out+"\"\n")
	t.Setenv("PATH", pathWithFakeBins(dir))
	t.Setenv("WAYLAND_DISPLAY", "wayland-1")
	t.Setenv("DISPLAY", "")

	var buf bytes.Buffer
	prev := osc52Out
	osc52Out = &buf
	t.Cleanup(func() { osc52Out = prev })

	m := scrollFixture(t, i18n.LangEN, 2)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(Model)
	m = flushCopy(t, m, cmd)

	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("wl-copy did not write output: %v", err)
	}
	if got := string(raw); got != "User: msg 1" {
		t.Fatalf("wl-copy payload = %q, want User: msg 1", got)
	}
	if buf.Len() != 0 {
		t.Fatalf("OSC 52 must not run when wl-copy works; got %q", buf.String())
	}
	if !strings.Contains(m.CopyNotice(), "wl-copy") {
		t.Fatalf("status should name wl-copy: %q", m.CopyNotice())
	}
	if !strings.Contains(m.CopyNotice(), "User") {
		t.Fatalf("status missing preview: %q", m.CopyNotice())
	}
}

func TestCopyUsesXclipWhenNoWayland(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "copied.txt")
	writeExecScript(t, dir, "xclip", "#!/bin/sh\n/bin/cat > \""+out+"\"\n")
	// A wl-copy that must not be consulted (no WAYLAND_DISPLAY).
	writeExecScript(t, dir, "wl-copy", "#!/bin/sh\necho wl-copy-should-not-run >&2\nexit 1\n")
	t.Setenv("PATH", pathWithFakeBins(dir))
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", ":0")

	var buf bytes.Buffer
	prev := osc52Out
	osc52Out = &buf
	t.Cleanup(func() { osc52Out = prev })

	m := scrollFixture(t, i18n.LangEN, 2)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(Model)
	m = flushCopy(t, m, cmd)

	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("xclip did not write output: %v", err)
	}
	if got := string(raw); got != "User: msg 1" {
		t.Fatalf("xclip payload = %q, want User: msg 1", got)
	}
	if buf.Len() != 0 {
		t.Fatalf("OSC 52 must not run when xclip works; got %q", buf.String())
	}
	if !strings.Contains(m.CopyNotice(), "xclip") {
		t.Fatalf("status should name xclip: %q", m.CopyNotice())
	}
}

func TestCopyFallsBackToOSC52WhenHelpersAbsent(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("WAYLAND_DISPLAY", "wayland-1")
	t.Setenv("DISPLAY", ":0")

	var buf bytes.Buffer
	prev := osc52Out
	osc52Out = &buf
	t.Cleanup(func() { osc52Out = prev })

	m := scrollFixture(t, i18n.LangEN, 1)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(Model)
	m = flushCopy(t, m, cmd)

	got, err := decodeOSC52Payload(buf.String())
	if err != nil {
		t.Fatalf("decode OSC: %v (raw=%q)", err, buf.String())
	}
	if got != "User: msg 0" {
		t.Fatalf("OSC payload = %q", got)
	}
	if !strings.Contains(m.CopyNotice(), "OSC 52") {
		t.Fatalf("status should name OSC 52: %q", m.CopyNotice())
	}
}

func TestCopyWlCopyFailureFallsBackToOSC52(t *testing.T) {
	dir := t.TempDir()
	writeExecScript(t, dir, "wl-copy", "#!/bin/sh\nexit 1\n")
	t.Setenv("PATH", pathWithFakeBins(dir))
	t.Setenv("WAYLAND_DISPLAY", "wayland-1")
	t.Setenv("DISPLAY", "")

	var buf bytes.Buffer
	prev := osc52Out
	osc52Out = &buf
	t.Cleanup(func() { osc52Out = prev })

	m := scrollFixture(t, i18n.LangEN, 1)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(Model)
	m = flushCopy(t, m, cmd)

	got, err := decodeOSC52Payload(buf.String())
	if err != nil {
		t.Fatalf("expected OSC fallback, decode: %v (raw=%q)", err, buf.String())
	}
	if got != "User: msg 0" {
		t.Fatalf("OSC payload = %q", got)
	}
	if !strings.Contains(m.CopyNotice(), "OSC 52") {
		t.Fatalf("status should report OSC 52 after wl-copy failure: %q", m.CopyNotice())
	}
	if strings.Contains(m.CopyNotice(), "wl-copy") {
		t.Fatalf("status must name the path that worked, not wl-copy: %q", m.CopyNotice())
	}
}

func TestCopyAllPathsFailShowsError(t *testing.T) {
	dir := t.TempDir()
	writeExecScript(t, dir, "wl-copy", "#!/bin/sh\nexit 1\n")
	writeExecScript(t, dir, "xclip", "#!/bin/sh\nexit 1\n")
	t.Setenv("PATH", pathWithFakeBins(dir))
	t.Setenv("WAYLAND_DISPLAY", "wayland-1")
	t.Setenv("DISPLAY", ":0")

	prev := osc52Out
	osc52Out = errWriter{err: errors.New("osc write failed")}
	t.Cleanup(func() { osc52Out = prev })

	m := scrollFixture(t, i18n.LangEN, 1)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(Model)
	m = flushCopy(t, m, cmd)

	want := m.t("chat.copy.failed")
	if m.CopyNotice() != want {
		t.Fatalf("notice = %q, want failure %q", m.CopyNotice(), want)
	}
	if strings.HasPrefix(m.CopyNotice(), "Copied") {
		t.Fatalf("must not pretend success: %q", m.CopyNotice())
	}
}

func TestWriteClipboard_PersistsViaRealHelperSmoke(t *testing.T) {
	if os.Getenv("TTV_REAL_CLIPBOARD") != "1" {
		t.Skip("set TTV_REAL_CLIPBOARD=1 to run real clipboard helper smoke")
	}
	// Optional machine smoke: real wl-copy then wl-paste. Skips when the
	// desktop clipboard tools are unavailable.
	if testing.Short() {
		t.Skip("short")
	}
	wayland := os.Getenv("WAYLAND_DISPLAY")
	if wayland == "" {
		t.Skip("WAYLAND_DISPLAY unset")
	}
	wlCopy, err := exec.LookPath("wl-copy")
	if err != nil {
		t.Skip("wl-copy not on PATH")
	}
	wlPaste, err := exec.LookPath("wl-paste")
	if err != nil {
		t.Skip("wl-paste not on PATH")
	}

	t.Setenv("WAYLAND_DISPLAY", wayland)
	t.Setenv("DISPLAY", "")
	// Prefer the resolved directory so a prior test's fake PATH cannot hide the
	// real helper after Cleanup ordering quirks.
	t.Setenv("PATH", filepath.Dir(wlCopy)+string(os.PathListSeparator)+"/usr/bin:/bin")

	payload := "ttv-tui ticket08 clipboard probe"
	type result struct {
		method string
		err    error
	}
	done := make(chan result, 1)
	go func() {
		method, werr := writeClipboard(payload)
		done <- result{method: method, err: werr}
	}()
	var got result
	select {
	case got = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("writeClipboard timed out using real wl-copy")
	}
	if got.err != nil {
		t.Fatalf("writeClipboard: %v", got.err)
	}
	if got.method != clipboardMethodWlCopy {
		t.Fatalf("method = %q, want wl-copy on this Wayland host", got.method)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, wlPaste, "-n").Output()
	if err != nil {
		t.Fatalf("wl-paste: %v", err)
	}
	if text := string(out); text != payload {
		t.Fatalf("wl-paste = %q, want %q", text, payload)
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
