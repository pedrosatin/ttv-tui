package ui

import (
	"context"
	"errors"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

func setupTwoPanelModel(t *testing.T) (Model, *fakePlayback, *fakeChat) {
	t.Helper()
	catEN, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New(en): %v", err)
	}

	title1 := "Major CS2 Final — Playoff Stage"
	title2 := "Speedrunning Resident Evil 4 Remake"
	src := &fakeSource{
		categoriesFn: func(ctx context.Context, page catalog.PageRequest) (catalog.Page[catalog.Category], error) {
			return catalog.Page[catalog.Category]{
				Items: []catalog.Category{
					{ID: "cat-cs", Name: "Counter-Strike", Viewers: catalog.IntPtr(250000)},
					{ID: "cat-jc", Name: "Just Chatting", Viewers: catalog.IntPtr(410000)},
				},
			}, nil
		},
		channelsFn: func(ctx context.Context, categoryID string, page catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
			return catalog.Page[catalog.Channel]{
				Items: []catalog.Channel{
					{ID: "ch-gau", Login: "gaules", DisplayName: "Gaules", Viewers: catalog.IntPtr(98500), Title: &title1},
					{ID: "ch-alan", Login: "alanzoka", DisplayName: "alanzoka", Viewers: catalog.IntPtr(42000), Title: &title2},
				},
			}, nil
		},
	}

	pb := &fakePlayback{}
	fc := &fakeChat{
		hasToken: true,
		username: "pedrosatin",
	}

	m := New(catEN, WithSource(src), WithPlayback(pb), WithChat(fc))
	updated, _ := m.Update(m.Init()())
	m = updated.(Model)

	// Enter Counter-Strike category -> loads channels.
	updated, chCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	updated, _ = m.Update(chCmd())
	m = updated.(Model)

	// Select Gaules channel -> starts playback and chat.
	updated, selCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	updated, batchCmd := m.Update(selCmd())
	m = updated.(Model)
	if batchCmd != nil {
		updated, _ = m.Update(batchCmd())
		m = updated.(Model)
	}

	// Mark player playing at vol 85 and chat connected with messages.
	updated, _ = m.Update(PlayerStatusMsg{
		Generation: 1,
		State:      PlayerStatePlaying,
		Login:      "gaules",
		Volume:     85,
	})
	m = updated.(Model)

	updated, _ = m.Update(ChatEventMsg{
		Generation: 1,
		Channel:    "gaules",
		State:      ChatStateConnected,
		Messages: []ChatMessage{
			{Generation: 1, Kind: chat.MessageChat, ID: "1", User: "tribo1", DisplayName: "TriboOne", Body: "VAMO TRIBO!"},
			{Generation: 1, Kind: chat.MessageChat, ID: "2", User: "tribo2", DisplayName: "TriboTwo", Body: "Que jogada absurda"},
		},
	})
	m = updated.(Model)

	return m, pb, fc
}

func TestLayoutWidths_120_100_80_79_AndRestoreFocusAndDraft(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)

	// Switch focus to chat, enter compose mode, type a draft, then Esc out of compose (still focused on chat).
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.Focus() != FocusChat {
		t.Fatalf("after Tab, Focus() = %q, want %q", m.Focus(), FocusChat)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = updated.(Model)
	for _, r := range "rascunho salvo" {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(Model)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.Composing() || m.Focus() != FocusChat || m.ComposeDraft() != "rascunho salvo" {
		t.Fatalf("expected FocusChat with draft preserved, got Composing=%v Focus=%q Draft=%q", m.Composing(), m.Focus(), m.ComposeDraft())
	}

	cases := []struct {
		width          int
		wantSideBySide bool
		wantNotice     bool
		minChatW       int
		maxChatW       int
	}{
		{width: 120, wantSideBySide: true, wantNotice: false, minChatW: 65, maxChatW: 75},
		{width: 100, wantSideBySide: true, wantNotice: false, minChatW: 55, maxChatW: 65},
		{width: 80, wantSideBySide: true, wantNotice: false, minChatW: 34, maxChatW: 40},
		{width: 79, wantSideBySide: false, wantNotice: true, minChatW: 0, maxChatW: 0},
	}

	var chatW100, chatW80 int
	for _, tc := range cases {
		updated, _ = m.Update(tea.WindowSizeMsg{Width: tc.width, Height: 14})
		m = updated.(Model)

		listW, chatW := m.panelWidths()
		if tc.width == 100 {
			chatW100 = chatW
		}
		if tc.width == 80 {
			chatW80 = chatW
		}

		if tc.wantSideBySide {
			if chatW < tc.minChatW || chatW > tc.maxChatW {
				t.Fatalf("width=%d: chatW=%d out of expected range [%d, %d] (listW=%d)", tc.width, chatW, tc.minChatW, tc.maxChatW, listW)
			}
		} else if chatW != 0 {
			t.Fatalf("width=%d: expected chatW=0 when hidden, got %d", tc.width, chatW)
		}

		plain := ansi.Strip(m.View())
		lines := strings.Split(plain, "\n")
		if len(lines) != 14 {
			t.Fatalf("width=%d: expected 14 lines for Height=14, got %d:\n%s", tc.width, len(lines), plain)
		}

		for i, line := range lines {
			w := ansi.StringWidth(line)
			if w > tc.width {
				t.Fatalf("width=%d line %d exceeded terminal width (%d > %d): %q", tc.width, i, w, tc.width, line)
			}
		}

		hasSep := strings.Contains(plain, "│")
		if hasSep != tc.wantSideBySide {
			t.Fatalf("width=%d: separator presence=%v, want %v:\n%s", tc.width, hasSep, tc.wantSideBySide, plain)
		}

		hasChatHeader := strings.Contains(plain, "Chat — #gaules")
		if hasChatHeader != tc.wantSideBySide {
			t.Fatalf("width=%d: chat header presence=%v, want %v:\n%s", tc.width, hasChatHeader, tc.wantSideBySide, plain)
		}

		hasNotice := strings.Contains(plain, "Chat hidden (<80 cols)")
		if hasNotice != tc.wantNotice {
			t.Fatalf("width=%d: narrow notice presence=%v, want %v:\n%s", tc.width, hasNotice, tc.wantNotice, plain)
		}
	}

	if chatW80 >= chatW100 {
		t.Fatalf("expected chat panel width at 80 cols (%d) to be smaller than at 100 cols (%d)", chatW80, chatW100)
	}

	// At 79 columns, chat is hidden and list remains operable (j moves highlight to ch-alan).
	if m.Focus() != FocusList {
		t.Fatalf("at 79 cols, effective Focus() = %q, want %q", m.Focus(), FocusList)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(Model)
	if m.HighlightedID() != "ch-alan" {
		t.Fatalf("at 79 cols, expected list navigation with 'j' to move highlight to ch-alan, got %q", m.HighlightedID())
	}

	// When terminal widens back to 120 columns, chat reappears with focus and draft intact!
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 14})
	m = updated.(Model)
	if m.Focus() != FocusChat {
		t.Fatalf("after widening back to 120 cols, Focus() = %q, want %q", m.Focus(), FocusChat)
	}
	if m.ComposeDraft() != "rascunho salvo" {
		t.Fatalf("after widening back to 120 cols, ComposeDraft() = %q, want %q", m.ComposeDraft(), "rascunho salvo")
	}
	plain120 := ansi.Strip(m.View())
	if !strings.Contains(plain120, "▸ Chat — #gaules [focus]") {
		t.Fatalf("expected focused chat header after widening to 120 cols:\n%s", plain120)
	}
	if !strings.Contains(plain120, "rascunho salvo") {
		t.Fatalf("expected draft visible in chat input after widening to 120 cols:\n%s", plain120)
	}

	// Status bar at 120 cols must show channel, category, viewers, player state
	// (with volume), and which panel is focused in the chrome next to help/quit.
	statusLine := strings.Split(plain120, "\n")[13]
	for _, want := range []string{
		"98.5k",
		"Counter-Strike",
		"Playing gaules",
		"vol 85",
		"Focus: chat",
	} {
		if !strings.Contains(statusLine, want) {
			t.Fatalf("status bar at 120 cols missing %q:\n%s", want, statusLine)
		}
	}
}

func TestFocusTabAndEscPreserveSelectionAndDraft(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 14})
	m = updated.(Model)

	// Move highlight in list to second channel ("ch-alan").
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(Model)
	if m.HighlightedID() != "ch-alan" {
		t.Fatalf("HighlightedID = %q, want ch-alan", m.HighlightedID())
	}

	// Tab switches focus from list to chat.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.Focus() != FocusChat {
		t.Fatalf("Focus = %q, want %q", m.Focus(), FocusChat)
	}
	plainChatFocus := ansi.Strip(m.View())
	if !strings.Contains(plainChatFocus, "▸ Chat — #gaules [focus]") {
		t.Fatalf("expected visible focus on chat header:\n%s", plainChatFocus)
	}

	// While focused on chat (even outside compose), j/k do not move the list selection.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(Model)
	if m.HighlightedID() != "ch-alan" {
		t.Fatalf("list selection changed while chat focused: got %q, want ch-alan", m.HighlightedID())
	}

	// Press 'i' to compose, type draft, press Esc -> leaves compose mode (still in chat), second Esc -> returns to list.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = updated.(Model)
	for _, r := range "oi chat" {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(Model)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.Composing() || m.Focus() != FocusChat {
		t.Fatalf("after first Esc from compose: Composing=%v Focus=%q", m.Composing(), m.Focus())
	}

	// Esc inside chat (not composing) returns focus to the list while keeping draft and list selection.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.Focus() != FocusList {
		t.Fatalf("after Esc in chat panel, Focus = %q, want %q", m.Focus(), FocusList)
	}
	if m.HighlightedID() != "ch-alan" {
		t.Fatalf("HighlightedID = %q, want ch-alan", m.HighlightedID())
	}
	if m.ComposeDraft() != "oi chat" {
		t.Fatalf("ComposeDraft = %q, want %q", m.ComposeDraft(), "oi chat")
	}

	plainListFocus := ansi.Strip(m.View())
	if !strings.Contains(plainListFocus, "▸ Live Channels — Counter-Strike [focus]") {
		t.Fatalf("expected visible focus on list header:\n%s", plainListFocus)
	}
}

func TestVisualTruncationUnicodeCJKAndEmojiNeverOverflowWidth(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)

	longEmojiCJKTitle := "🔥🎮 日本語の配信タイトル — 超長いストリームタイトルでレイアウトが崩れないことを確認するテスト 🚀✨🎉"
	m.list.channels[0].Title = &longEmojiCJKTitle

	updated, _ := m.Update(ChatEventMsg{
		Generation: 1,
		Channel:    "gaules",
		State:      ChatStateConnected,
		Messages: []ChatMessage{
			{
				Generation:  1,
				Kind:        chat.MessageChat,
				ID:          "u1",
				User:        "unicodefan",
				DisplayName: "日本語ユーザー🎌",
				Body:        "🔥🔥🔥 こんにちは世界！これは非常に長い絵文字と漢字のメッセージです 🚀🚀🚀 1234567890",
			},
		},
	})
	m = updated.(Model)

	for _, width := range []int{120, 100, 80, 79} {
		updated, _ = m.Update(tea.WindowSizeMsg{Width: width, Height: 12})
		m = updated.(Model)

		plain := ansi.Strip(m.View())
		lines := strings.Split(plain, "\n")
		if len(lines) != 12 {
			t.Fatalf("width=%d: got %d lines, want 12", width, len(lines))
		}
		for i, line := range lines {
			visualW := ansi.StringWidth(line)
			if visualW > width {
				t.Fatalf("width=%d line %d overflowed visual width (%d > %d): %q", width, i, visualW, width, line)
			}
		}
		if !strings.Contains(plain, "…") && !strings.Contains(plain, "...") {
			t.Fatalf("width=%d: expected ellipsis from visual truncation:\n%s", width, plain)
		}
	}
}

// TestCJKChannelNameDoesNotInsertBlankListRow: a CJK display name plus the row
// em dash used to overflow by one cell on East Asian terminals (ambiguous width),
// wrapping a spurious blank line under the name. fitListRow keeps one logical row.
func TestCJKChannelNameDoesNotInsertBlankListRow(t *testing.T) {
	catEN, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	title := "配信タイトル — とても長いタイトルで折り返しを誘発する"
	m := New(catEN)
	m.list.mode = ModeChannels
	m.list.channels = []catalog.Channel{
		{ID: "cjk-1", Login: "nihongo", DisplayName: "日本語配信者", Viewers: catalog.IntPtr(1234), Title: &title, Availability: catalog.AvailabilityLive},
		{ID: "cjk-2", Login: "nextchan", DisplayName: "次のチャンネル", Viewers: catalog.IntPtr(56), Title: catalog.StringPtr("short"), Availability: catalog.AvailabilityLive},
		{ID: "lat-3", Login: "latin", DisplayName: "LatinOnly", Viewers: catalog.IntPtr(99), Title: catalog.StringPtr("ascii title"), Availability: catalog.AvailabilityLive},
	}
	m.list.highlightedID = "cjk-1"

	for _, width := range []int{80, 120} {
		updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 14})
		m = updated.(Model)
		plain := ansi.Strip(m.View())
		lines := strings.Split(plain, "\n")
		if len(lines) != 14 {
			t.Fatalf("width=%d: got %d lines, want 14:\n%s", width, len(lines), plain)
		}
		for i, line := range lines {
			if ansi.StringWidth(line) > width {
				t.Fatalf("width=%d line %d overflow (%d): %q", width, i, ansi.StringWidth(line), line)
			}
		}

		// List body: header then channel rows without an empty line between them.
		var listRows []string
		for _, line := range lines[:len(lines)-1] {
			cell := strings.TrimRight(line, " ")
			if left, _, ok := strings.Cut(line, panelSeparator); ok {
				cell = strings.TrimRight(left, " ")
			}
			listRows = append(listRows, cell)
		}
		idxNihongo, idxNext := -1, -1
		for i, row := range listRows {
			if strings.Contains(row, "nihongo") {
				idxNihongo = i
			}
			if strings.Contains(row, "nextchan") {
				idxNext = i
			}
		}
		if idxNihongo < 0 || idxNext < 0 {
			t.Fatalf("width=%d: missing CJK channel rows:\n%s", width, plain)
		}
		if idxNext != idxNihongo+1 {
			t.Fatalf("width=%d: expected next channel immediately after CJK row (got %d then %d):\n%s",
				width, idxNihongo, idxNext, strings.Join(listRows, "\n"))
		}
		if strings.Contains(listRows[idxNihongo], "—") {
			t.Fatalf("width=%d: CJK row still has ambiguous em dash: %q", width, listRows[idxNihongo])
		}
		if !strings.Contains(listRows[idxNihongo], "日本語配信者") {
			t.Fatalf("width=%d: CJK display name missing: %q", width, listRows[idxNihongo])
		}
	}

	// Unit: fitListRow replaces em dash and stays within maxWidth for CJK.
	row := "▸   1.2k  日本語配信者 (nihongo) — " + title
	for _, max := range []int{20, 31, 40, 55, 80} {
		got := fitListRow(row, max)
		if hasWideRune(got) && strings.Contains(got, "—") {
			t.Fatalf("fitListRow(%d) kept em dash: %q", max, got)
		}
		if w := ansi.StringWidth(got); w > max {
			t.Fatalf("fitListRow(%d) width %d: %q", max, w, got)
		}
	}
	latin := "▸   1.2k  LatinOnly (latin) — ascii title"
	if got := fitListRow(latin, 80); !strings.Contains(got, "—") {
		t.Fatalf("Latin row should keep em dash: %q", got)
	}
}

func TestIndependentStates_CtrlRPlayerErrorAndChatError(t *testing.T) {
	m, pb, fc := setupTwoPanelModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 14})
	m = updated.(Model)

	// 1. Ctrl+R returns to categories without stopping video or disconnecting chat.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = updated.(Model)
	if cmd != nil {
		updated, _ = m.Update(cmd())
		m = updated.(Model)
	}
	if m.Mode() != ModeCategories {
		t.Fatalf("Mode = %q, want %q", m.Mode(), ModeCategories)
	}
	if pb.stopped != 0 {
		t.Fatalf("Ctrl+R must not stop video; pb.stopped = %d", pb.stopped)
	}
	if fc.stopped != 0 {
		t.Fatalf("Ctrl+R must not stop chat; fc.stopped = %d", fc.stopped)
	}
	if m.PlayerState() != PlayerStatePlaying || m.ChatState() != ChatStateConnected {
		t.Fatalf("states after Ctrl+R: Player=%q Chat=%q", m.PlayerState(), m.ChatState())
	}

	// 2. Player error does not close or disconnect the chat.
	updated, _ = m.Update(PlayerStatusMsg{
		Generation: 1,
		State:      PlayerStateError,
		Login:      "gaules",
		LastLine:   "streamlink exited with code 1",
	})
	m = updated.(Model)
	if m.PlayerState() != PlayerStateError {
		t.Fatalf("PlayerState = %q, want error", m.PlayerState())
	}
	if m.ChatState() != ChatStateConnected || fc.stopped != 0 {
		t.Fatalf("player error must not affect chat: ChatState=%q fc.stopped=%d", m.ChatState(), fc.stopped)
	}
	plain := ansi.Strip(m.View())
	if !strings.Contains(plain, "Player failed: press r to retry") || !strings.Contains(plain, "Connected to #gaules") {
		t.Fatalf("expected both player error and connected chat in view:\n%s", plain)
	}

	// Restore player to playing, then trigger a chat error -> must not stop the player.
	updated, _ = m.Update(PlayerStatusMsg{
		Generation: 1,
		State:      PlayerStatePlaying,
		Login:      "gaules",
		Volume:     80,
	})
	m = updated.(Model)

	updated, _ = m.Update(ChatEventMsg{
		Generation: 1,
		Channel:    "gaules",
		State:      ChatStateError,
		ErrText:    "TLS handshake timeout",
	})
	m = updated.(Model)
	if m.ChatState() != ChatStateError {
		t.Fatalf("ChatState = %q, want error", m.ChatState())
	}
	if m.PlayerState() != PlayerStatePlaying || pb.stopped != 0 {
		t.Fatalf("chat error must not stop player: PlayerState=%q pb.stopped=%d", m.PlayerState(), pb.stopped)
	}
	plain = ansi.Strip(m.View())
	if !strings.Contains(plain, "Playing gaules · vol 80") || !strings.Contains(plain, "Chat error: TLS handshake timeout") {
		t.Fatalf("expected playing video and chat error in view:\n%s", plain)
	}

	// 4. Navigation error in list does not stop player or chat.
	m.list.err = &twitch.Error{Kind: twitch.Rede, Op: "categories", Err: errors.New("offline")}
	plain = ansi.Strip(m.View())
	if !strings.Contains(plain, "Network error") || !strings.Contains(plain, "Chat — #gaules") {
		t.Fatalf("expected list network error alongside active chat panel:\n%s", plain)
	}
}

func TestHelpOverlayAccessibleAtAnyTime(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 16})
	m = updated.(Model)

	// Toggle help with '?'.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = updated.(Model)
	if !m.HelpVisible() {
		t.Fatal("expected HelpVisible() = true after pressing '?'")
	}
	plain := ansi.Strip(m.View())
	if !strings.Contains(plain, "Keyboard Shortcuts") || !strings.Contains(plain, "Switch between the list and the chat") {
		t.Fatalf("expected help overlay in View():\n%s", plain)
	}

	// Close help with Esc.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.HelpVisible() {
		t.Fatal("expected HelpVisible() = false after pressing Esc")
	}

	// Enter compose mode and press F1 -> help is accessible even while composing!
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = updated.(Model)
	if !m.Composing() {
		t.Fatal("expected Composing() = true")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyF1})
	m = updated.(Model)
	if !m.HelpVisible() {
		t.Fatal("expected HelpVisible() = true after pressing F1 in compose mode")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyF1})
	m = updated.(Model)
	if m.HelpVisible() {
		t.Fatal("expected HelpVisible() = false after pressing F1 again")
	}
}
