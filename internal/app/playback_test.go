package app

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/player"
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
