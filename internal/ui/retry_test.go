package ui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

// TestRetryWithHiddenListAndPlayerError tests Hypothesis 1 & Criterion 3:
// When list is hidden and player is in error, pressing 'r' must retry the player,
// not be rejected with layout.hidden_list_key, in both en and pt-BR.
func TestRetryWithHiddenListAndPlayerError(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		t.Run(lang, func(t *testing.T) {
			m, pb, _ := setupTwoPanelModel(t)
			m.catalog = mustCatalog(t, lang)
			updated, _ := m.Update(PlayerStatusMsg{
				Generation: 1,
				State:      PlayerStateError,
				Login:      "gaules",
				LastLine:   "error: network failure",
				Failure:    PlayerFailureNetwork,
			})
			m = updated.(Model)

			// Hide list.
			m.layout.showList = false
			m.normalizeFocusAfterLayout()
			if m.listVisible() {
				t.Fatal("list should be hidden")
			}

			// Status bar must announce player retry with the available key in both languages.
			bar := ansi.Strip(m.StatusBar())
			if !strings.Contains(bar, m.playerFailureText()) {
				t.Fatalf("status bar lacks player failure text: %q", bar)
			}
			switch lang {
			case i18n.LangEN:
				if !strings.Contains(bar, "press r to retry") && !strings.Contains(bar, "r retry") {
					t.Fatalf("en bar missing retry key hint: %q", bar)
				}
			case i18n.LangPTBR:
				if !strings.Contains(bar, "r para tentar de novo") && !strings.Contains(bar, "r repetir") {
					t.Fatalf("pt-BR bar missing retry key hint: %q", bar)
				}
			}

			// Press 'r'.
			beforeStarts := len(pb.started)
			updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
			m = updated.(Model)

			if m.notices.status == m.t("layout.hidden_list_key") {
				t.Fatalf("r was incorrectly intercepted as hidden list key: notice=%q", m.notices.status)
			}
			if m.PlayerState() != PlayerStateStarting {
				t.Fatalf("player state after r = %q, want %q", m.PlayerState(), PlayerStateStarting)
			}
			if cmd == nil {
				t.Fatal("r returned nil command, expected playback.Start")
			}
			m = drainCmd(t, m, cmd)
			if len(pb.started) != beforeStarts+1 || pb.started[len(pb.started)-1] != "gaules" {
				t.Fatalf("playback not restarted: started=%v", pb.started)
			}
		})
	}
}

// TestRetrySimultaneousErrorsPrioritizesPlayer tests Hypothesis 2 & Criterion 3:
// When both player and list are in error, the status bar advertises player retry,
// and pressing 'r' must retry the player, matching the advertised action in both en and pt-BR.
func TestRetrySimultaneousErrorsPrioritizesPlayer(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		t.Run(lang, func(t *testing.T) {
			m, pb, _ := setupTwoPanelModel(t)
			m.catalog = mustCatalog(t, lang)

			// Both player and list error.
			updated, _ := m.Update(PlayerStatusMsg{
				Generation: 1,
				State:      PlayerStateError,
				Login:      "gaules",
				LastLine:   "error: network failure",
				Failure:    PlayerFailureNetwork,
			})
			m = updated.(Model)
			m.list.err = &twitch.Error{Kind: twitch.Servidor, Op: "channels", Err: errors.New("server down")}

			// Verify status bar announces player error and not list error.
			bar := ansi.Strip(m.StatusBar())
			if !strings.Contains(bar, m.playerFailureText()) {
				t.Fatalf("status bar should announce player error, got: %q", bar)
			}
			if strings.Contains(bar, m.listErrorAction()) {
				t.Fatalf("status bar should not show list error when player error leads: %q", bar)
			}
			switch lang {
			case i18n.LangEN:
				if !strings.Contains(bar, "press r to retry") && !strings.Contains(bar, "r retry") {
					t.Fatalf("en bar missing retry key hint: %q", bar)
				}
			case i18n.LangPTBR:
				if !strings.Contains(bar, "r para tentar de novo") && !strings.Contains(bar, "r repetir") {
					t.Fatalf("pt-BR bar missing retry key hint: %q", bar)
				}
			}

			// Press 'r'.
			beforeStarts := len(pb.started)
			updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
			m = updated.(Model)

			if m.PlayerState() != PlayerStateStarting {
				t.Fatalf("player state after r = %q, want %q (should have retried player)", m.PlayerState(), PlayerStateStarting)
			}
			if cmd == nil {
				t.Fatal("expected retry command for player")
			}
			m = drainCmd(t, m, cmd)
			if len(pb.started) != beforeStarts+1 || pb.started[len(pb.started)-1] != "gaules" {
				t.Fatalf("playback not restarted: started=%v", pb.started)
			}
		})
	}
}

// TestRetryListWhenOnlyListHasError tests that when player has no error,
// 'r' retries the list and status bar shows the list retry key in both languages.
func TestRetryListWhenOnlyListHasError(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		t.Run(lang, func(t *testing.T) {
			m, _, _ := setupTwoPanelModel(t)
			m.catalog = mustCatalog(t, lang)
			m.list.err = &twitch.Error{Kind: twitch.Rede, Op: "channels", Err: errors.New("network down")}

			bar := ansi.Strip(m.StatusBar())
			if !strings.Contains(bar, m.listErrorAction()) {
				t.Fatalf("status bar should announce list error, got: %q", bar)
			}
			switch lang {
			case i18n.LangEN:
				if !strings.Contains(bar, "press r to retry") {
					t.Fatalf("en bar missing retry key hint: %q", bar)
				}
			case i18n.LangPTBR:
				if !strings.Contains(bar, "r para tentar de novo") {
					t.Fatalf("pt-BR bar missing retry key hint: %q", bar)
				}
			}

			updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
			m = updated.(Model)
			if cmd == nil {
				t.Fatal("expected retry command for list")
			}
			if m.list.err != nil {
				t.Fatalf("list.err should be cleared on retry, got: %v", m.list.err)
			}
		})
	}
}

// TestRetryBurstDoesNotCancelNewestRequest tests ticket 04 extra:
// Rapid consecutive 'r' presses must cancel the older in-flight request,
// while preserving the newest request so it completes successfully.
func TestRetryBurstDoesNotCancelNewestRequest(t *testing.T) {
	var mu sync.Mutex
	contexts := make([]context.Context, 0, 2)
	req1Started := make(chan struct{})
	req2Started := make(chan struct{})
	req1Block := make(chan struct{})
	var req2ActiveErr error

	src := &fakeSource{
		categoriesFn: func(ctx context.Context, page twitch.PageRequest) (twitch.Page[twitch.Category], error) {
			mu.Lock()
			count := len(contexts)
			contexts = append(contexts, ctx)
			mu.Unlock()

			if count == 0 {
				close(req1Started)
				select {
				case <-req1Block:
				case <-ctx.Done():
				}
				return twitch.Page[twitch.Category]{}, ctx.Err()
			}
			req2ActiveErr = ctx.Err()
			close(req2Started)
			return twitch.Page[twitch.Category]{
				Items: []twitch.Category{
					{ID: "cat-new", Name: "New Category"},
				},
			}, nil
		},
	}

	catEN, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n: %v", err)
	}

	m := New(catEN, WithSource(src))
	m.width, m.height = 80, 24
	m.list.mode = ModeCategories

	// First 'r'
	updated, cmd1 := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updated.(Model)
	if cmd1 == nil {
		t.Fatal("cmd1 is nil")
	}

	// Start cmd1 in background
	go func() {
		_ = cmd1()
	}()

	select {
	case <-req1Started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for req1 to start")
	}

	// Second 'r' in rapid succession
	updated, cmd2 := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updated.(Model)
	if cmd2 == nil {
		t.Fatal("cmd2 is nil")
	}

	var msg2 tea.Msg
	done2 := make(chan struct{})
	go func() {
		msg2 = cmd2()
		close(done2)
	}()

	select {
	case <-req2Started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for req2 to start")
	}

	select {
	case <-done2:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for req2 to finish")
	}

	close(req1Block)

	mu.Lock()
	ctx1 := contexts[0]
	mu.Unlock()

	// Verify req1 context was canceled by the second request
	if ctx1.Err() == nil {
		t.Fatal("expected req1 context to be canceled by burst, but ctx1.Err() == nil")
	}

	// Verify req2 was active during execution
	if req2ActiveErr != nil {
		t.Fatalf("expected req2 context to remain active, but ctx.Err() = %v", req2ActiveErr)
	}

	// Apply msg2 to model
	loaded, ok := msg2.(CategoriesLoadedMsg)
	if !ok {
		t.Fatalf("msg2 is %T, want CategoriesLoadedMsg", msg2)
	}
	if loaded.Err != nil {
		t.Fatalf("loaded.Err = %v, want nil", loaded.Err)
	}

	updated, _ = m.Update(msg2)
	m = updated.(Model)
	if m.list.err != nil {
		t.Fatalf("list.err after cmd2 = %v, want nil", m.list.err)
	}
	if len(m.list.categories) != 1 || m.list.categories[0].ID != "cat-new" {
		t.Fatalf("expected newest categories to be loaded, got %v", m.list.categories)
	}
}
