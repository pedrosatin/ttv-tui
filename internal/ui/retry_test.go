package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

var keyR = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}}

// playerErrorModel returns a two-panel model in lang whose player failed on gaules.
func playerErrorModel(t *testing.T, lang string) (Model, *fakePlayback) {
	t.Helper()
	m, pb, _ := setupTwoPanelModel(t)
	m.catalog = mustCatalog(t, lang)
	updated, _ := m.Update(PlayerStatusMsg{
		Generation: 1,
		State:      PlayerStateError,
		Login:      "gaules",
		LastLine:   "error: network failure",
		Failure:    PlayerFailureNetwork,
	})
	return updated.(Model), pb
}

// pressRRestartsPlayer checks that the bar leads with the player failure, which
// names the r key in each language, and that r restarts the player.
func pressRRestartsPlayer(t *testing.T, m Model, pb *fakePlayback) {
	t.Helper()
	if bar := ansi.Strip(m.StatusBar()); !strings.Contains(bar, m.playerFailureText()) {
		t.Fatalf("status bar = %q, want the player failure %q", bar, m.playerFailureText())
	}
	before := len(pb.started)
	updated, cmd := m.Update(keyR)
	m = updated.(Model)
	if m.notices.status == m.t("layout.hidden_list_key") {
		t.Fatalf("r was taken as a hidden list key: notice=%q", m.notices.status)
	}
	if m.PlayerState() != PlayerStateStarting {
		t.Fatalf("player state after r = %q, want %q", m.PlayerState(), PlayerStateStarting)
	}
	drainCmd(t, m, cmd)
	if len(pb.started) != before+1 || pb.started[len(pb.started)-1] != "gaules" {
		t.Fatalf("playback not restarted: started=%v", pb.started)
	}
}

func TestRetryPlayerWithHiddenList(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		t.Run(lang, func(t *testing.T) {
			m, pb := playerErrorModel(t, lang)
			m.layout.showList = false
			m.normalizeFocusAfterLayout()
			if m.listVisible() {
				t.Fatal("list should be hidden")
			}
			pressRRestartsPlayer(t, m, pb)
		})
	}
}

// With list and player errors, the bar leads with the player; r must follow it.
func TestRetryPlayerAheadOfListError(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		t.Run(lang, func(t *testing.T) {
			m, pb := playerErrorModel(t, lang)
			m.list.err = &twitch.Error{Kind: twitch.Servidor, Op: "channels", Err: errors.New("server down")}
			if bar := ansi.Strip(m.StatusBar()); strings.Contains(bar, m.listErrorAction()) {
				t.Fatalf("status bar shows the list error under a player error: %q", bar)
			}
			pressRRestartsPlayer(t, m, pb)
		})
	}
}

// Without a player error, r on a hidden list keeps the hidden-list notice.
func TestRetryHiddenListWithoutPlayerError(t *testing.T) {
	m, pb, _ := setupTwoPanelModel(t)
	m.layout.showList = false
	m.normalizeFocusAfterLayout()
	before := len(pb.started)
	updated, _ := m.Update(keyR)
	m = updated.(Model)
	if m.notices.status != m.t("layout.hidden_list_key") {
		t.Fatalf("notice = %q, want %q", m.notices.status, m.t("layout.hidden_list_key"))
	}
	if len(pb.started) != before {
		t.Fatalf("r started playback: started=%v", pb.started)
	}
}

func TestRetryListWhenOnlyListHasError(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		t.Run(lang, func(t *testing.T) {
			m, _, _ := setupTwoPanelModel(t)
			m.catalog = mustCatalog(t, lang)
			m.list.err = &twitch.Error{Kind: twitch.Rede, Op: "channels", Err: errors.New("network down")}
			if bar := ansi.Strip(m.StatusBar()); !strings.Contains(bar, m.listErrorAction()) {
				t.Fatalf("status bar = %q, want the list error %q", bar, m.listErrorAction())
			}
			updated, cmd := m.Update(keyR)
			m = updated.(Model)
			if cmd == nil || m.list.err != nil {
				t.Fatalf("r did not retry the list: cmd=%v err=%v", cmd != nil, m.list.err)
			}
		})
	}
}

// Two quick r presses cancel the first request; its late result does not
// replace the newest one.
func TestRetryBurstKeepsNewestRequest(t *testing.T) {
	firstStarted := make(chan struct{})
	calls := 0
	src := &fakeSource{
		categoriesFn: func(ctx context.Context, _ twitch.PageRequest) (twitch.Page[twitch.Category], error) {
			calls++
			if calls == 1 {
				close(firstStarted)
				<-ctx.Done()
				return twitch.Page[twitch.Category]{}, ctx.Err()
			}
			if err := ctx.Err(); err != nil {
				return twitch.Page[twitch.Category]{}, err
			}
			return twitch.Page[twitch.Category]{Items: []twitch.Category{{ID: "cat-new", Name: "New"}}}, nil
		},
	}
	m := New(mustCatalog(t, i18n.LangEN), WithSource(src))
	m.width, m.height = 80, 24
	m.list.mode = ModeCategories

	updated, cmd1 := m.Update(keyR)
	m = updated.(Model)
	first := make(chan tea.Msg, 1)
	go func() { first <- cmd1() }()
	select {
	case <-firstStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("first request did not start")
	}

	updated, cmd2 := m.Update(keyR)
	m = updated.(Model)
	updated, _ = m.Update(cmd2())
	m = updated.(Model)

	var msg1 tea.Msg
	select {
	case msg1 = <-first:
	case <-time.After(2 * time.Second):
		t.Fatal("first request was not canceled")
	}
	updated, _ = m.Update(msg1)
	m = updated.(Model)

	if m.list.err != nil {
		t.Fatalf("list.err = %v, want nil", m.list.err)
	}
	if len(m.list.categories) != 1 || m.list.categories[0].ID != "cat-new" {
		t.Fatalf("categories = %v, want the newest result", m.list.categories)
	}
}
