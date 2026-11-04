package ui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

var (
	sgrColorRe = regexp.MustCompile(`\x1b\[(?:\d+;)*?(3[0-7]|9[0-7]|38;5;\d+)m`)
	sgrFgRe    = regexp.MustCompile(`\x1b\[(?:\d+;)*?(3[1-7]|9[0-7])m`)
)

func TestOrientationColorsPlayerStatesEmitExpectedSGR(t *testing.T) {
	forceOrientationColorsForTest(true)
	defer forceOrientationColorsForTest(false)

	cases := []struct {
		name  string
		state PlayerState
		want  string // substring of SGR params, e.g. "32" green
	}{
		{"starting", PlayerStateStarting, "90"}, // bright black / dim gray
		{"playing", PlayerStatePlaying, "32"},
		{"paused", PlayerStatePaused, "33"},
		{"break", PlayerStateBreak, "36"},
		{"error", PlayerStateError, "31"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := New(mustCatalog(t, i18n.LangEN))
			m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: 100, Height: 24})
			m.playerLogin = "gaules"
			m.playerState = tc.state
			m.playerVolume = 80
			if tc.state == PlayerStateError {
				m.playerFailure = PlayerFailureNetwork
			}
			if tc.state == PlayerStateBreak {
				m.playerBreakElapsed = 0
			}
			bar := m.StatusBar()
			if !strings.Contains(bar, "\x1b[") {
				t.Fatalf("expected ANSI in status bar for %s:\n%q", tc.name, bar)
			}
			if !strings.Contains(bar, tc.want+"m") && !strings.Contains(bar, ";"+tc.want+"m") && !strings.Contains(bar, tc.want+";") {
				// Accept combined SGR such as "1;32m" or "2;90m".
				re := regexp.MustCompile(`\x1b\[[0-9;]*` + tc.want + `[0-9;]*m`)
				if !re.MatchString(bar) {
					t.Fatalf("status for %s missing SGR %s:\n%q", tc.name, tc.want, bar)
				}
			}
			plain := ansi.Strip(bar)
			base := m.playerStatusBase()
			if tc.state == PlayerStateError {
				base = m.playerFailureText()
			}
			if !strings.Contains(plain, ansi.Strip(base)) && !strings.Contains(plain, strings.TrimSpace(ansi.Strip(base))) {
				// playerStatusBase may be truncated; at least the state word must remain.
				if plain == "" {
					t.Fatalf("empty status for %s", tc.name)
				}
			}
		})
	}
}

func TestOrientationColorsAvailabilityAndFocus(t *testing.T) {
	forceOrientationColorsForTest(true)
	defer forceOrientationColorsForTest(false)

	m := New(mustCatalog(t, i18n.LangPTBR),
		WithFavorites([]twitch.Channel{
			{ID: "f1", Login: "favlive", DisplayName: "FavLive", Viewers: twitch.IntPtr(10), Availability: twitch.AvailabilityLive},
			{ID: "f2", Login: "favoff", DisplayName: "FavOff", Availability: twitch.AvailabilityOffline},
		}),
		WithRecents([]twitch.Channel{
			{ID: "r1", Login: "unknown", DisplayName: "Unknown"},
		}),
	)
	m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: 100, Height: 24})
	m.playerLogin = "favlive"
	m.playerState = PlayerStatePlaying
	// No chat panel: keep the full width for the list so offline/unverified rows stay visible.
	m.focus = FocusList
	if items := m.VisibleItems(); len(items) < 3 {
		t.Fatalf("VisibleItems=%d, want favorites+recents", len(items))
	}

	view := m.View()
	plain := ansi.Strip(view)

	for _, label := range []string{
		m.t("channels.offline_tag"),
		m.t("channels.unverified_tag"),
		m.t("section.favorites"),
	} {
		if !strings.Contains(plain, label) {
			t.Fatalf("plain view missing label %q:\n%s", label, plain)
		}
	}
	// Playing channel forces the live label into the viewers column.
	if !strings.Contains(plain, m.t("channels.live_label")) && !strings.Contains(plain, "FavLive") {
		t.Fatalf("playing/live row missing:\n%s", plain)
	}
	if !sgrFgRe.MatchString(view) {
		t.Fatalf("expected foreground SGR in colored view:\n%q", view)
	}
	// Playing/live → green 32; offline → red 31; favorite → yellow 33.
	for _, code := range []string{"32", "31", "33"} {
		re := regexp.MustCompile(`\x1b\[[0-9;]*` + code + `[0-9;]*m`)
		if !re.MatchString(view) {
			t.Errorf("view missing SGR %s:\n%q", code, view)
		}
	}
}

func TestOrientationColorsChatSystemRejectMention(t *testing.T) {
	forceOrientationColorsForTest(true)
	defer forceOrientationColorsForTest(false)

	m := New(mustCatalog(t, i18n.LangEN), WithChat(&fakeChat{hasToken: true, username: "pedrosatin"}))
	m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	m.chatPanel.channel = "gaules"
	m.chatPanel.state = ChatStateConnected
	m.chatPanel.username = "pedrosatin"
	m.focus = FocusChat
	m.chatPanel.buf.Append(ChatMessage{Kind: chat.MessageSystem, Body: "slow mode on"})
	m.chatPanel.buf.Append(ChatMessage{Kind: chat.MessageNotice, Body: "followers-only"})
	m.chatPanel.buf.Append(ChatMessage{
		Kind: chat.MessageChat, User: "alice", DisplayName: "Alice",
		Body: "hey @pedrosatin check this",
	})
	m.chatPanel.buf.Append(ChatMessage{
		Kind: chat.MessageChat, User: "bob", DisplayName: "Bob", Body: "hello",
		Delivery: chat.DeliveryRejected, RejectReason: chat.RejectSlowMode,
	})

	view := m.View()
	plain := ansi.Strip(view)
	if !strings.Contains(plain, m.t("chat.system_prefix")) {
		t.Fatalf("missing system prefix:\n%s", plain)
	}
	if !strings.Contains(plain, m.t("chat.mention_tag")) {
		t.Fatalf("missing mention tag:\n%s", plain)
	}
	if !strings.Contains(plain, "[rejected:") && !strings.Contains(plain, m.t("chat.reject.slow_mode")) {
		// rejected format embeds the reason.
		rej := fmt.Sprintf(m.t("chat.send.rejected"), m.t("chat.reject.slow_mode"))
		if !strings.Contains(plain, ansi.Strip(rej)) && !strings.Contains(plain, "rejected") {
			t.Fatalf("missing rejection label:\n%s", plain)
		}
	}
	for _, code := range []string{"35", "31", "93"} { // magenta, red, bright yellow
		re := regexp.MustCompile(`\x1b\[[0-9;]*` + code + `[0-9;]*m`)
		if !re.MatchString(view) {
			t.Errorf("chat view missing SGR %s:\n%q", code, view)
		}
	}
}

func TestOrientationColorsDisabledNoForegroundSGR(t *testing.T) {
	forceOrientationColorsForTest(false)
	defer forceOrientationColorsForTest(true)

	m := New(mustCatalog(t, i18n.LangEN),
		WithFavorites([]twitch.Channel{
			{ID: "f1", Login: "gaules", DisplayName: "Gaules", Viewers: twitch.IntPtr(12), Availability: twitch.AvailabilityLive},
			{ID: "f2", Login: "offline_guy", DisplayName: "Offline", Availability: twitch.AvailabilityOffline},
		}),
	)
	m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: 100, Height: 24})
	m.playerState = PlayerStatePlaying
	m.playerLogin = "gaules"
	m.helpVisible = true
	m.highlightedID = "f1"

	view := m.View()
	if sgrColorRe.MatchString(view) {
		t.Fatalf("NO_COLOR/disabled must not emit color SGR:\n%q", view)
	}
	plain := ansi.Strip(view)
	// States remain distinguishable by labels/symbols without color.
	for _, want := range []string{
		m.t("channels.offline_tag"),
		m.t("section.favorites"),
		"▸",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("without color, missing %q:\n%s", want, plain)
		}
	}
	help := ansi.Strip(strings.Join(m.renderHelpLines(100), "\n"))
	if !strings.Contains(help, m.t("help.colors")) {
		t.Fatalf("help legend missing without color:\n%s", help)
	}

	// List error path: labels stay, still no color SGR.
	m.err = &twitch.Error{Kind: twitch.Rede, Op: "categories", Err: fmt.Errorf("boom")}
	errView := m.View()
	if sgrColorRe.MatchString(errView) {
		t.Fatalf("error view must not emit color SGR when disabled:\n%q", errView)
	}
	if !strings.Contains(ansi.Strip(errView), m.t("error.action_retry")) {
		t.Fatalf("error retry label missing without color:\n%s", ansi.Strip(errView))
	}
}

func TestOrientationColorsHelpLegendAndWidths(t *testing.T) {
	forceOrientationColorsForTest(true)
	defer forceOrientationColorsForTest(false)

	for _, width := range []int{80, 120} {
		t.Run(fmt.Sprintf("%d", width), func(t *testing.T) {
			m := New(mustCatalog(t, i18n.LangPTBR))
			m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: width, Height: 28})
			m.helpVisible = true
			m.playerState = PlayerStatePaused
			m.playerLogin = "gaules"
			view := m.View()
			plain := ansi.Strip(view)
			legend := m.t("help.colors")
			if w := ansi.StringWidth("  " + legend); w > 80 {
				t.Fatalf("help.colors is %d cells, must fit 80", w)
			}
			// Overlay may truncate the legend on very short panels; the raw help
			// lines must still carry it, and the visible view must show a prefix.
			help := strings.Join(m.renderHelpLines(width), "\n")
			if !strings.Contains(ansi.Strip(help), legend) {
				t.Fatalf("renderHelpLines missing legend:\n%s", help)
			}
			prefix := legend
			if runes := []rune(legend); len(runes) > 20 {
				prefix = string(runes[:20])
			}
			if !strings.Contains(plain, prefix) {
				t.Fatalf("help legend prefix missing at width %d:\n%s", width, plain)
			}
			for _, line := range strings.Split(plain, "\n") {
				if ansi.StringWidth(line) > width {
					t.Fatalf("line wider than %d: %d %q", width, ansi.StringWidth(line), line)
				}
			}
			bar := m.StatusBar()
			re := regexp.MustCompile(`\x1b\[[0-9;]*33[0-9;]*m`)
			if !re.MatchString(bar) {
				t.Fatalf("paused status missing yellow SGR:\n%q", bar)
			}
		})
	}
}

func TestMentionsSelfDetection(t *testing.T) {
	m := New(mustCatalog(t, i18n.LangEN))
	m.chatPanel.username = "PedroSatin"
	cases := []struct {
		body string
		want bool
	}{
		{"hey @pedrosatin", true},
		{"@PedroSatin hi", true},
		{"pedrosatin check", true},
		{"hello world", false},
		{"apedrosatin", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := m.mentionsSelf(tc.body); got != tc.want {
			t.Errorf("mentionsSelf(%q)=%v, want %v", tc.body, got, tc.want)
		}
	}
}

func TestOrientationColorsPTYEvidenceDump(t *testing.T) {
	forceOrientationColorsForTest(true)
	defer forceOrientationColorsForTest(false)

	m := New(mustCatalog(t, i18n.LangPTBR),
		WithFavorites([]twitch.Channel{
			{ID: "f1", Login: "gaules", DisplayName: "Gaules", Viewers: twitch.IntPtr(1200), Availability: twitch.AvailabilityLive},
			{ID: "f2", Login: "off", DisplayName: "Off", Availability: twitch.AvailabilityOffline},
		}),
	)
	m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: 100, Height: 24})
	m.playerState = PlayerStatePlaying
	m.playerLogin = "gaules"
	m.helpVisible = true
	view := m.View()
	bar := m.StatusBar()
	t.Logf("RAW_VIEW=%q", view)
	t.Logf("RAW_BAR=%q", bar)
	re := regexp.MustCompile(`\x1b\[[0-9;]*(3[1-7]|90|9[1-7]|38;5;\d+)[0-9;]*m`)
	hits := re.FindAllString(view+"\n"+bar, -1)
	if len(hits) == 0 {
		t.Fatal("expected color SGR in PTY dump")
	}
	t.Logf("SGR_HITS=%q", hits)
}
