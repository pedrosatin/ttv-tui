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

// helpContainsLegend reports whether the (possibly wrapped) help text still
// carries every field of the legend phrase.
func helpContainsLegend(help, legend string) bool {
	compact := strings.Join(strings.Fields(help), " ")
	want := strings.Join(strings.Fields(legend), " ")
	return strings.Contains(compact, want)
}

func TestOrientationColorsPlayerStatesEmitExpectedSGR(t *testing.T) {
	forceOrientationColorsForTest(true)
	defer forceOrientationColorsForTest(false)

	cases := []struct {
		name  string
		state PlayerState
		want  string // substring of SGR params, e.g. "32" green
	}{
		{"starting", PlayerStateStarting, "37"}, // white + bold (ticket 14)
		{"playing", PlayerStatePlaying, "32"},
		{"paused", PlayerStatePaused, "33"},
		{"break", PlayerStateBreak, "36"},
		{"error", PlayerStateError, "31"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := New(mustCatalog(t, i18n.LangEN))
			m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: 100, Height: 24})
			m.player.login = "gaules"
			m.player.state = tc.state
			m.player.volume = 80
			if tc.state == PlayerStateError {
				m.player.failure = PlayerFailureNetwork
			}
			if tc.state == PlayerStateBreak {
				m.player.breakElapsed = 0
			}
			bar := m.StatusBar()
			if !strings.Contains(bar, "\x1b[") {
				t.Fatalf("expected ANSI in status bar for %s:\n%q", tc.name, bar)
			}
			re := regexp.MustCompile(`\x1b\[[0-9;]*` + tc.want + `[0-9;]*m`)
			if !re.MatchString(bar) {
				t.Fatalf("status for %s missing SGR %s:\n%q", tc.name, tc.want, bar)
			}
			if tc.name == "starting" {
				if strings.Contains(bar, "\x1b[2;") || strings.Contains(bar, "\x1b[2m") {
					t.Fatalf("starting must not use dim (SGR 2):\n%q", bar)
				}
				boldRe := regexp.MustCompile(`\x1b\[[0-9;]*1[0-9;]*m`)
				if !boldRe.MatchString(bar) {
					t.Fatalf("starting must use bold (SGR 1):\n%q", bar)
				}
				if regexp.MustCompile(`\x1b\[[0-9;]*90[0-9;]*m`).MatchString(bar) {
					t.Fatalf("starting must not use bright black (SGR 90):\n%q", bar)
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
	m.player.login = "favlive"
	m.player.state = PlayerStatePlaying
	m.layout.focus = FocusList
	m.list.highlightedID = "f1"
	if items := m.VisibleItems(); len(items) < 3 {
		t.Fatalf("VisibleItems=%d, want favorites+recents", len(items))
	}

	view := m.View()
	plain := ansi.Strip(view)

	for _, label := range []string{
		m.t("channels.offline_tag"),
		m.t("channels.unverified_tag"),
		m.t("section.favorites"),
		"●",
		"★",
		"▸",
	} {
		if !strings.Contains(plain, label) {
			t.Fatalf("plain view missing label %q:\n%s", label, plain)
		}
	}
	if !sgrFgRe.MatchString(view) {
		t.Fatalf("expected foreground SGR in colored view:\n%q", view)
	}
	// Live marker/viewers → green 32; offline tag → red 31; favorite ★ → yellow 33; selection uses full-row reverse + bold.
	for _, code := range []string{"32", "31", "33", "7"} {
		re := regexp.MustCompile(`\x1b\[[0-9;]*` + code + `[0-9;]*m`)
		if !re.MatchString(view) {
			t.Errorf("view missing SGR %s:\n%q", code, view)
		}
	}
	if !regexp.MustCompile(`\x1b\[[0-9;]*1[0-9;]*m`).MatchString(view) {
		t.Errorf("focus/highlight must use bold (SGR 1):\n%q", view)
	}
	if regexp.MustCompile(`\x1b\[[0-9;]*96[0-9;]*m`).MatchString(view) {
		t.Errorf("focus must not use bright cyan (SGR 96):\n%q", view)
	}
	// Favorite must not paint the whole row yellow: identity stays uncolored.
	if strings.Contains(view, "\x1b[33mFavLive") || strings.Contains(view, "\x1b[33m★ FavLive") {
		t.Fatalf("favorite must not color the whole row yellow:\n%q", view)
	}
}

func TestOrientationColorsChatSystemRejectMention(t *testing.T) {
	forceOrientationColorsForTest(true)
	defer forceOrientationColorsForTest(false)

	m := New(mustCatalog(t, i18n.LangEN), WithChat(&fakeChat{hasToken: true, username: "pedrosatin"}))
	m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	m.chat.panel.channel = "gaules"
	m.chat.panel.state = ChatStateConnected
	m.chat.panel.username = "pedrosatin"
	m.layout.focus = FocusChat
	m.chat.panel.buf.Append(ChatMessage{Kind: chat.MessageSystem, Body: "slow mode on"})
	m.chat.panel.buf.Append(ChatMessage{Kind: chat.MessageSystem, MsgID: systemMsgIDModeration, Body: "spammer timed out"})
	m.chat.panel.buf.Append(ChatMessage{Kind: chat.MessageNotice, Body: "followers-only"})
	m.chat.panel.buf.Append(ChatMessage{
		Kind: chat.MessageChat, User: "alice", DisplayName: "Alice", Color: "#FF0000",
		Body: "hey @pedrosatin check this",
	})
	m.chat.panel.buf.Append(ChatMessage{
		Kind: chat.MessageChat, User: "bob", DisplayName: "Bob", Body: "hello", Color: "#00FF00",
		Delivery: chat.DeliveryRejected, RejectReason: chat.RejectSlowMode,
	})

	view := m.View()
	plain := ansi.Strip(view)
	if !strings.Contains(plain, m.t("chat.system_prefix")) {
		t.Fatalf("missing system prefix:\n%s", plain)
	}
	if !strings.Contains(plain, m.t("chat.mod_prefix")) {
		t.Fatalf("missing mod prefix:\n%s", plain)
	}
	if !strings.Contains(plain, m.t("chat.mention_tag")) {
		t.Fatalf("missing mention tag:\n%s", plain)
	}
	if !strings.Contains(plain, "[rejected:") && !strings.Contains(plain, m.t("chat.reject.slow_mode")) {
		rej := fmt.Sprintf(m.t("chat.send.rejected"), m.t("chat.reject.slow_mode"))
		if !strings.Contains(plain, ansi.Strip(rej)) && !strings.Contains(plain, "rejected") {
			t.Fatalf("missing rejection label:\n%s", plain)
		}
	}
	// Magenta system, red reject, underline mention (4), nick blues/whites only.
	for _, code := range []string{"35", "31"} {
		re := regexp.MustCompile(`\x1b\[[0-9;]*` + code + `[0-9;]*m`)
		if !re.MatchString(view) {
			t.Errorf("chat view missing SGR %s:\n%q", code, view)
		}
	}
	if !strings.Contains(view, "\x1b[4m") && !strings.Contains(view, ";4m") && !strings.Contains(view, "4;") {
		t.Fatalf("mention must use underline:\n%q", view)
	}
	// Nicks must not reuse semantic red/green from Twitch hex.
	if strings.Contains(view, "\x1b[38;2;") || strings.Contains(view, "\x1b[38;5;") {
		t.Fatalf("nicks must not use truecolor/256 from Twitch hex:\n%q", view)
	}
}

func TestOrientationColorsNickPaletteOnly(t *testing.T) {
	forceOrientationColorsForTest(true)
	defer forceOrientationColorsForTest(false)

	for _, name := range []string{"Alice", "Bob", "carol", "xQc", "gaules", "PedRo"} {
		styled := styleNick(name, "#FF0000")
		plain := ansi.Strip(styled)
		if plain != name {
			t.Fatalf("styleNick(%q) plain=%q", name, plain)
		}
		reBad := regexp.MustCompile(`\x1b\[[0-9;]*(3[12356]|9[12356])[0-9;]*m`)
		if reBad.MatchString(styled) {
			t.Fatalf("nick %q used semantic SGR: %q", name, styled)
		}
		reOK := regexp.MustCompile(`\x1b\[[0-9;]*(34|94|37|97)[0-9;]*m`)
		if !reOK.MatchString(styled) {
			t.Fatalf("nick %q missing blue/white SGR: %q", name, styled)
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
	m.player.state = PlayerStatePlaying
	m.player.login = "gaules"
	m.layout.helpVisible = true
	m.list.highlightedID = "f1"

	view := m.View()
	if sgrColorRe.MatchString(view) {
		t.Fatalf("NO_COLOR/disabled must not emit color SGR:\n%q", view)
	}
	plain := ansi.Strip(view)
	for _, want := range []string{
		m.t("channels.offline_tag"),
		m.t("section.favorites"),
		"▸",
		"★",
		"●",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("without color, missing %q:\n%s", want, plain)
		}
	}
	help := ansi.Strip(strings.Join(m.renderHelpLines(100), "\n"))
	if !helpContainsLegend(help, m.t("help.colors")) {
		t.Fatalf("help legend missing without color:\n%s", help)
	}

	m.list.err = &twitch.Error{Kind: twitch.Rede, Op: "categories", Err: fmt.Errorf("boom")}
	errView := m.View()
	if sgrColorRe.MatchString(errView) {
		t.Fatalf("error view must not emit color SGR when disabled:\n%q", errView)
	}
	if !strings.Contains(ansi.Strip(errView), m.t("error.action_retry")) {
		t.Fatalf("error retry label missing without color:\n%s", ansi.Strip(errView))
	}
}

func TestOrientationColorsCLICOLORZeroDisablesEvenWithForce(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR", "0")
	t.Setenv("CLICOLOR_FORCE", "1")
	t.Setenv("TERM", "xterm-256color")
	resetOrientationColorsForTest()
	defer forceOrientationColorsForTest(true)

	if colorsEnabled() {
		t.Fatal("CLICOLOR=0 must disable colors even when CLICOLOR_FORCE=1")
	}

	m := New(mustCatalog(t, i18n.LangEN),
		WithFavorites([]twitch.Channel{
			{ID: "f1", Login: "gaules", DisplayName: "Gaules", Viewers: twitch.IntPtr(12), Availability: twitch.AvailabilityLive},
		}),
	)
	m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: 100, Height: 24})
	m.player.state = PlayerStatePlaying
	m.player.login = "gaules"
	view := m.View()
	if sgrColorRe.MatchString(view) {
		t.Fatalf("CLICOLOR=0 must not emit color SGR:\n%q", view)
	}
}

func TestOrientationColorsTERMDumbAndNOCOLOR(t *testing.T) {
	for _, env := range []struct {
		name string
		set  map[string]string
	}{
		{"NO_COLOR", map[string]string{"NO_COLOR": "1", "CLICOLOR": "1", "CLICOLOR_FORCE": "", "TERM": "xterm-256color"}},
		{"TERM_dumb", map[string]string{"NO_COLOR": "", "CLICOLOR": "1", "CLICOLOR_FORCE": "", "TERM": "dumb"}},
	} {
		t.Run(env.name, func(t *testing.T) {
			for k, v := range env.set {
				t.Setenv(k, v)
			}
			resetOrientationColorsForTest()
			if colorsEnabled() {
				t.Fatalf("%s must disable colors", env.name)
			}
		})
	}
	forceOrientationColorsForTest(true)
}

func TestOrientationColorsHelpLegendAndWidths(t *testing.T) {
	forceOrientationColorsForTest(true)
	defer forceOrientationColorsForTest(false)

	for _, width := range []int{80, 120} {
		t.Run(fmt.Sprintf("%d", width), func(t *testing.T) {
			m := New(mustCatalog(t, i18n.LangPTBR))
			m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: width, Height: 28})
			m.layout.helpVisible = true
			m.player.state = PlayerStatePaused
			m.player.login = "gaules"
			view := m.View()
			plain := ansi.Strip(view)
			legend := m.t("help.colors")
			help := strings.Join(m.renderHelpLines(width), "\n")
			helpPlain := ansi.Strip(help)
			if !helpContainsLegend(helpPlain, legend) {
				t.Fatalf("renderHelpLines missing full legend:\n%s", helpPlain)
			}
			for _, line := range strings.Split(helpPlain, "\n") {
				if w := ansi.StringWidth(line); width > 0 && w > width {
					t.Fatalf("legend line wider than %d: %d %q", width, w, line)
				}
			}
			prefix := "Cores:"
			if width > 0 && !strings.Contains(legend, "Cores:") {
				prefix = "Colors:"
			}
			if !strings.Contains(plain, prefix) && !strings.Contains(helpPlain, prefix) {
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
			if !strings.Contains(help, "\x1b[") {
				t.Fatalf("colored help legend missing SGR:\n%q", help)
			}
		})
	}
}

func TestAvailabilityTagsNeverTruncatedAt80(t *testing.T) {
	forceOrientationColorsForTest(false)
	defer forceOrientationColorsForTest(true)

	for _, lang := range []string{i18n.LangPTBR, i18n.LangEN} {
		cat := mustCatalog(t, lang)
		m := New(cat,
			WithFavorites([]twitch.Channel{
				{ID: "f-1", Login: "gaules", DisplayName: "GaulesNomeBemLongoParaForcar", Availability: twitch.AvailabilityUnknown},
			}),
			WithRecents([]twitch.Channel{
				{ID: "r-1", Login: "offline_guy", DisplayName: "OfflineNomeBemLongoParaForcar", Availability: twitch.AvailabilityOffline},
				{ID: "r-2", Login: "liveguy", DisplayName: "LiveNomeBemLongo", Viewers: nil, Availability: twitch.AvailabilityLive},
			}),
		)
		m.player.login = "liveguy"
		m.player.state = PlayerStatePlaying
		m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: 80, Height: 24})
		plain := ansi.Strip(m.View())
		for _, tag := range []string{
			cat.T("channels.offline_tag"),
			cat.T("channels.unverified_tag"),
			cat.T("channels.live_compact"),
		} {
			if !strings.Contains(plain, tag) {
				t.Fatalf("%s: missing intact tag %q at 80 cols:\n%s", lang, tag, plain)
			}
			if strings.Contains(plain, tag+"…") || strings.Contains(plain, strings.TrimSuffix(tag, "]")+"…") {
				t.Fatalf("%s: tag %q was truncated:\n%s", lang, tag, plain)
			}
		}
		for i, line := range strings.Split(plain, "\n") {
			if w := ansi.StringWidth(line); w > 80 {
				t.Fatalf("%s line %d width %d > 80: %q", lang, i, w, line)
			}
		}
	}
}

func TestMentionsSelfDetection(t *testing.T) {
	m := New(mustCatalog(t, i18n.LangEN))
	m.chat.panel.username = "PedroSatin"
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
		if got := m.chat.mentionsSelf(tc.body); got != tc.want {
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
	m.player.state = PlayerStatePlaying
	m.player.login = "gaules"
	m.layout.helpVisible = true
	m.list.highlightedID = "f1"
	view := m.View()
	bar := m.StatusBar()
	t.Logf("RAW_VIEW=%q", view)
	t.Logf("RAW_BAR=%q", bar)
	plain := ansi.Strip(view)
	t.Logf("PLAIN_VIEW=%s", plain)
	re := regexp.MustCompile(`\x1b\[[0-9;]*(3[1-7]|90|9[1-7]|38;5;\d+)[0-9;]*m`)
	hits := re.FindAllString(view+"\n"+bar, -1)
	if len(hits) == 0 {
		t.Fatal("expected color SGR in PTY dump")
	}
	t.Logf("SGR_HITS=%q", hits)

	forceOrientationColorsForTest(false)
	resetOrientationColorsForTest()
	t.Setenv("NO_COLOR", "1")
	configureOrientationColors()
	m2 := New(mustCatalog(t, i18n.LangPTBR),
		WithFavorites([]twitch.Channel{
			{ID: "f1", Login: "gaules", DisplayName: "Gaules", Viewers: twitch.IntPtr(1200), Availability: twitch.AvailabilityLive},
		}),
	)
	m2, _ = pressKeyMsg(m2, tea.WindowSizeMsg{Width: 100, Height: 24})
	m2.player.state = PlayerStatePlaying
	m2.player.login = "gaules"
	m2.layout.helpVisible = true
	noColorView := m2.View()
	t.Logf("NO_COLOR_VIEW=%q", noColorView)
	if sgrColorRe.MatchString(noColorView) {
		t.Fatalf("NO_COLOR PTY dump still has color SGR:\n%q", noColorView)
	}
}
