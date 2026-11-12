package app

import (
	"context"
	"fmt"
	"io"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/player"
	"github.com/pedrosatin/ttv-tui/internal/ui"
)

// playbackBridge adapts player.Manager to ui.Playback without the UI importing player.
// Events are forwarded with tea.Program.Send from a single goroutine started in Run.
type playbackBridge struct {
	mgr *player.Manager
}

func newPlaybackBridge(mgr *player.Manager) *playbackBridge {
	return &playbackBridge{mgr: mgr}
}

// twitchTarget builds the playback Target for a Twitch channel login. An empty
// login yields an empty Target, which the player refuses: the bare Twitch home
// page is not a channel.
func twitchTarget(login string) player.Target {
	if login == "" {
		return player.Target{}
	}
	return player.Target{
		URL:   fmt.Sprintf("https://www.twitch.tv/%s", login),
		Label: login,
	}
}

func (b *playbackBridge) Start(login string, selection uint64) tea.Cmd {
	return b.StartTarget(ui.PlayTarget{Label: login}, selection)
}

func (b *playbackBridge) StartTarget(target ui.PlayTarget, selection uint64) tea.Cmd {
	mgr := b.mgr
	var pTarget player.Target
	if target.URL != "" {
		label := target.Label
		if label == "" {
			label = target.URL
		}
		pTarget = player.Target{
			URL:   target.URL,
			Label: label,
		}
	} else {
		pTarget = twitchTarget(target.Label)
	}
	identity := pTarget.Identity()
	return func() tea.Msg {
		// Cmds run in goroutines that may not keep the order of the selections.
		gen, err := mgr.StartInOrder(pTarget, selection)
		if err != nil {
			return ui.PlayerStatusMsg{
				Generation: gen,
				State:      ui.PlayerStateError,
				Login:      identity,
				LastLine:   err.Error(),
				Failure:    ui.PlayerFailure(player.FailureOf(err)),
				Selection:  selection,
			}
		}
		// Handshake for the UI; later states arrive via the Events→Send pump.
		return ui.PlayerStatusMsg{
			Generation: gen,
			State:      ui.PlayerStateStarting,
			Login:      identity,
			Volume:     mgr.Volume(),
			Selection:  selection,
		}
	}
}

// Stop reads the generation when it is asked for, not when its Cmd runs: a
// Start asked for after it may reach the player first, and stays up.
func (b *playbackBridge) Stop() tea.Cmd {
	mgr := b.mgr
	gen := mgr.Generation()
	return func() tea.Msg {
		vol := mgr.Volume()
		if err := mgr.StopGeneration(gen); err != nil {
			return ui.PlayerStatusMsg{
				Generation: gen,
				State:      ui.PlayerStateError,
				LastLine:   err.Error(),
				Failure:    ui.PlayerFailure(player.FailureOf(err)),
				Volume:     vol,
			}
		}
		return ui.PlayerStatusMsg{
			Generation: gen,
			State:      ui.PlayerStateEnded,
			Volume:     vol,
		}
	}
}

func (b *playbackBridge) TogglePause() tea.Cmd {
	mgr := b.mgr
	return func() tea.Msg {
		_ = mgr.TogglePause()
		return nil
	}
}

func (b *playbackBridge) AdjustVolume(delta int) tea.Cmd {
	mgr := b.mgr
	return func() tea.Msg {
		_ = mgr.AdjustVolume(delta)
		return nil
	}
}

func (b *playbackBridge) Listen() tea.Cmd {
	// Events are pumped with Program.Send; nothing to poll here.
	return nil
}

func mapPlayerEvent(ev player.Event) ui.PlayerStatusMsg {
	// Only an explicit Stop is neutral. Process/IPC exits may arrive without
	// a classified error, especially when mpv cannot open a video window.
	if ev.State == player.StateEnded && ev.Reason != player.EndReasonStopped {
		ev.State = player.StateError
		if ev.Failure == player.FailureNone {
			ev.Failure = player.ClassifyLine(ev.LastLine)
			if ev.Failure == player.FailureNone {
				ev.Failure = player.FailureUnknown
			}
		}
	}
	return ui.PlayerStatusMsg{
		Generation:   ev.Generation,
		State:        ui.PlayerState(ev.State),
		Login:        ev.Login,
		LastLine:     ev.LastLine,
		BreakElapsed: ev.BreakElapsed,
		Volume:       ev.Volume,
		Failure:      ui.PlayerFailure(ev.Failure),

		InputConfFailed:     ev.InputConfFailed,
		ContainmentDegraded: ev.ContainmentDegraded,
	}
}

// logInputConfErr records why the mpv window ran without keys. It runs after the
// TUI exits; the status bar already warned during playback.
func logInputConfErr(log io.Writer, mgr *player.Manager, cat *i18n.Catalog) {
	if err := mgr.InputConfErr(); err != nil {
		_, _ = fmt.Fprintf(log, cat.T("app.warn_input_conf")+"\n", err)
	}
}

// logPlayerNotes records what the player sessions noted while the TUI owned the
// terminal.
func logPlayerNotes(log io.Writer, notes []string, cat *i18n.Catalog) {
	for _, n := range notes {
		_, _ = fmt.Fprintf(log, cat.T("app.warn_player")+"\n", n)
	}
}

// forwardPlayerEvents sends Manager events into the running tea program.
// It returns when ctx is cancelled or the Events channel is closed.
func forwardPlayerEvents(ctx context.Context, p *tea.Program, mgr *player.Manager) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-mgr.Events():
			if !ok {
				return
			}
			msg := mapPlayerEvent(ev)
			// Avoid blocking forever if the program has already exited.
			ch := make(chan struct{})
			go func() {
				p.Send(msg)
				close(ch)
			}()
			select {
			case <-ch:
			case <-ctx.Done():
				return
			}
		}
	}
}
