package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/platform"
	"github.com/pedrosatin/ttv-tui/internal/player"
	"github.com/pedrosatin/ttv-tui/internal/provider"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
	"github.com/pedrosatin/ttv-tui/internal/ui"
)

func TestMapPlayerEventCarriesWindowVolumeAndKeysWarning(t *testing.T) {
	msg := mapPlayerEvent(player.Event{
		Generation:          3,
		State:               player.StatePlaying,
		Login:               "gaules",
		Volume:              35,
		InputConfFailed:     true,
		ContainmentDegraded: true,
	})
	if msg.State != ui.PlayerStatePlaying || msg.Volume != 35 || msg.Generation != 3 {
		t.Fatalf("msg = %+v", msg)
	}
	if !msg.ContainmentDegraded {
		t.Fatal("ContainmentDegraded lost in mapping")
	}
	if !msg.InputConfFailed {
		t.Fatal("InputConfFailed lost in the mapping")
	}
}

func TestLogInputConfErr(t *testing.T) {
	for lang, want := range map[string]string{
		i18n.LangEN:   "the mpv window ran without keys",
		i18n.LangPTBR: "a janela do mpv ficou sem teclas",
	} {
		t.Run(lang, func(t *testing.T) {
			cat, err := i18n.New(lang)
			if err != nil {
				t.Fatalf("i18n.New: %v", err)
			}
			mgr := player.New(player.Options{
				RuntimeDir: t.TempDir(),
				StopWait:   100 * time.Millisecond,
				Command: func(player.Target, string) *exec.Cmd {
					return exec.Command(os.Args[0], "-test.run=^$")
				},
				WriteInputConf: func(string) error { return errors.New("disk full") },
			})
			defer mgr.Close()

			var log bytes.Buffer
			logInputConfErr(&log, mgr, cat)
			if log.Len() != 0 {
				t.Fatalf("logged before any failure: %q", log.String())
			}

			if _, err := mgr.Start(twitchTarget("gaules")); err != nil {
				t.Fatalf("Start: %v", err)
			}
			mgr.Stop()
			logInputConfErr(&log, mgr, cat)
			got := log.String()
			if !strings.Contains(got, want) || !strings.Contains(got, "disk full") || !strings.HasSuffix(got, "\n") {
				t.Fatalf("log = %q", got)
			}
		})
	}
}

func TestLogPlayerNotes(t *testing.T) {
	for lang, want := range map[string]string{
		i18n.LangEN:   "ttv-tui: player warning: player: process group 42 survived SIGKILL\n",
		i18n.LangPTBR: "ttv-tui: aviso do player: player: process group 42 survived SIGKILL\n",
	} {
		cat, err := i18n.New(lang)
		if err != nil {
			t.Fatalf("i18n.New: %v", err)
		}
		var log bytes.Buffer
		logPlayerNotes(&log, []string{"player: process group 42 survived SIGKILL"}, cat)
		if log.String() != want {
			t.Fatalf("%s: log = %q want %q", lang, log.String(), want)
		}
	}
}

func TestMapPlayerTerminalEvent(t *testing.T) {
	for _, reason := range []player.EndReason{player.EndReasonStopped, player.EndReasonMPVDead, player.EndReasonSocketEOF, player.EndReasonStreamlinkExit, ""} {
		for _, line := range []string{"", "streamlink: not found"} {
			msg := mapPlayerEvent(player.Event{Generation: 7, State: player.StateEnded, Reason: reason, Login: "xaryu", LastLine: line})
			if reason == player.EndReasonStopped {
				if msg.State != ui.PlayerStateEnded {
					t.Fatalf("stop became failure: %+v", msg)
				}
			} else {
				want := ui.PlayerFailureUnknown
				if line != "" {
					want = ui.PlayerFailureStreamlinkMissing
				}
				if msg.State != ui.PlayerStateError || msg.Failure != want || msg.LastLine != line || msg.Generation != 7 || msg.Login != "xaryu" {
					t.Fatalf("%s: %+v", reason, msg)
				}
			}
		}
	}
}

func TestTwitchTargetEmptyLoginIsRefusedByThePlayer(t *testing.T) {
	if got := twitchTarget(""); got.URL != "" {
		t.Fatalf("twitchTarget(\"\").URL = %q, want empty", got.URL)
	}
	mgr := player.New(player.Options{})
	t.Cleanup(mgr.Close)
	if _, err := mgr.Start(twitchTarget("")); err == nil {
		t.Fatal("Start with an empty login must fail, not open the Twitch home page")
	}
}

func TestPlaybackBridge_EnsureStreamlink_Configured(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "streamlink")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	mgr := player.New(player.Options{})
	t.Cleanup(mgr.Close)

	bridge := newPlaybackBridge(mgr, bin, t.TempDir())
	if err := bridge.ensureStreamlink(context.Background()); err != nil {
		t.Fatalf("ensureStreamlink: %v", err)
	}
	if mgr.StreamlinkPath() != bin {
		t.Fatalf("mgr streamlink path = %q, want %q", mgr.StreamlinkPath(), bin)
	}
}

func TestPlaybackBridge_EnsureStreamlink_ExplicitMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-streamlink")
	mgr := player.New(player.Options{})
	t.Cleanup(mgr.Close)

	bridge := newPlaybackBridge(mgr, missing, t.TempDir())
	err := bridge.ensureStreamlink(context.Background())
	if err == nil {
		t.Fatal("expected error for non-existent configured streamlink")
	}
}

func TestEnsureStreamlinkSkipsCustomPlayerCommand(t *testing.T) {
	mgr := player.New(player.Options{Command: func(player.Target, string) *exec.Cmd {
		return exec.Command("true")
	}})
	t.Cleanup(mgr.Close)
	missing := filepath.Join(t.TempDir(), "no-such-streamlink")
	bridge := newPlaybackBridge(mgr, missing, t.TempDir())
	if err := bridge.ensureStreamlink(context.Background()); err != nil {
		t.Fatalf("ensureStreamlink with a custom player command = %v, want nil (no lookup, no download)", err)
	}
}

// twitchTarget delegates to the Twitch provider for backward compatibility with tests.
func twitchTarget(login string) player.Target {
	prov := twitch.NewProvider(nil)
	st, _ := prov.StreamTarget(catalog.Channel{Login: login, Platform: platform.Twitch})
	return player.Target{
		URL:       st.URL,
		ExtraArgs: st.ExtraArgs,
		Label:     st.Label,
	}
}

func TestPlaybackBridgeStartTargetTable(t *testing.T) {
	tests := []struct {
		name      string
		target    ui.PlayTarget
		wantURL   string
		wantLabel string
		wantErr   bool
	}{
		{
			name:      "Twitch por login",
			target:    ui.PlayTarget{Label: "gaules"},
			wantURL:   "https://www.twitch.tv/gaules",
			wantLabel: "gaules",
		},
		{
			name:      "Twitch com URL",
			target:    ui.PlayTarget{URL: "https://www.twitch.tv/gaules", Label: "gaules", Platform: platform.Twitch},
			wantURL:   "https://www.twitch.tv/gaules",
			wantLabel: "gaules",
		},
		{
			name:      "Kick colado (rotulo qualificado)",
			target:    ui.PlayTarget{URL: "https://kick.com/xqc", Label: "kick:xqc", Platform: platform.Kick},
			wantURL:   "https://kick.com/xqc",
			wantLabel: "kick:xqc",
		},
		{
			name:      "YouTube colado (rotulo qualificado)",
			target:    ui.PlayTarget{URL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ", Label: "youtube:dQw4w9WgXcQ", Platform: platform.YouTube},
			wantURL:   "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
			wantLabel: "youtube:dQw4w9WgXcQ",
		},
		{
			name:      "host generico (facebook.com:<id>)",
			target:    ui.PlayTarget{URL: "https://facebook.com/watch/live/?v=123", Label: "facebook.com:123", Platform: "facebook.com"},
			wantURL:   "https://facebook.com/watch/live/?v=123",
			wantLabel: "facebook.com:123",
		},
		{
			name:    "canal fora da Twitch sem URL (erro)",
			target:  ui.PlayTarget{Label: "kick:xqc", Platform: platform.Kick},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var startedTarget player.Target
			mgr := player.New(player.Options{
				RuntimeDir: t.TempDir(),
				StopWait:   100 * time.Millisecond,
				Command: func(target player.Target, socket string) *exec.Cmd {
					startedTarget = target
					return exec.Command(os.Args[0], "-test.run=^$")
				},
			})
			defer mgr.Close()

			bridge := newPlaybackBridge(mgr)
			bridge.setRegistry(provider.NewRegistry(twitch.NewProvider(nil), provider.NewURLProvider()))

			cmd := bridge.StartTarget(tt.target, 1)
			if cmd == nil {
				t.Fatal("StartTarget returned nil cmd")
			}
			rawMsg := cmd()
			msg, ok := rawMsg.(ui.PlayerStatusMsg)
			if !ok {
				t.Fatalf("msg is %T, want ui.PlayerStatusMsg", rawMsg)
			}

			if tt.wantErr {
				if msg.State != ui.PlayerStateError {
					t.Fatalf("msg.State = %v, want PlayerStateError", msg.State)
				}
				if msg.LastLine == "" {
					t.Fatal("msg.LastLine is empty, want error description")
				}
				return
			}

			if msg.State == ui.PlayerStateError {
				t.Fatalf("unexpected error: %s", msg.LastLine)
			}
			if startedTarget.URL != tt.wantURL {
				t.Errorf("startedTarget.URL = %q, want %q", startedTarget.URL, tt.wantURL)
			}
			if startedTarget.Label != tt.wantLabel {
				t.Errorf("startedTarget.Label = %q, want %q", startedTarget.Label, tt.wantLabel)
			}
		})
	}
}
