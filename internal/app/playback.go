package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/components"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/platform"
	"github.com/pedrosatin/ttv-tui/internal/player"
	"github.com/pedrosatin/ttv-tui/internal/provider"
	"github.com/pedrosatin/ttv-tui/internal/termtext"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
	"github.com/pedrosatin/ttv-tui/internal/ui"
)

// playbackBridge adapts player.Manager to ui.Playback without the UI importing player.
// Events are forwarded with tea.Program.Send from a single goroutine started in Run.
type playbackBridge struct {
	mgr           *player.Manager
	registry      *provider.Registry
	cfgStreamlink string
	dataDir       string
	sender        func(tea.Msg)
	down          *components.Downloader
	ctx           context.Context

	mu          sync.Mutex
	resolved    components.Result
	hasResolved bool
	// confirmedVersion is the managed version whose trial was already reported
	// as over; rollbacks lists the updates that were undone (see streamlink_update.go).
	confirmedVersion string
	rollbacks        []streamlinkRollback

	// bg tracks the update check, trial bookkeeping and rollback, none of which
	// may delay playback. useMu guards the use marker of the managed version.
	bg         sync.WaitGroup
	useMu      sync.Mutex
	useVersion string
	useRelease func() error
}

func newPlaybackBridge(mgr *player.Manager, opts ...string) *playbackBridge {
	b := &playbackBridge{
		mgr:      mgr,
		registry: provider.NewRegistry(twitch.NewProvider(nil), provider.NewURLProvider()),
	}
	if len(opts) > 0 {
		b.cfgStreamlink = opts[0]
	}
	if len(opts) > 1 {
		b.dataDir = opts[1]
	}
	return b
}

func (b *playbackBridge) setRegistry(reg *provider.Registry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.registry = reg
}

func (b *playbackBridge) setContext(ctx context.Context) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ctx = ctx
}

func (b *playbackBridge) setSender(send func(tea.Msg)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sender = send
}

func (b *playbackBridge) ensureStreamlink(ctx context.Context) error {
	if b.mgr == nil || !b.mgr.UsesStreamlink() {
		// A custom player command does not run streamlink: nothing to resolve,
		// and above all nothing to download.
		return nil
	}
	b.mu.Lock()
	if b.hasResolved {
		b.mu.Unlock()
		return nil
	}
	cfgPath := b.cfgStreamlink
	dataDir := b.dataDir
	if ctx == nil || ctx == context.Background() {
		if b.ctx != nil {
			ctx = b.ctx
		}
	}
	b.mu.Unlock()

	resolver := components.NewResolver()
	if dataDir != "" {
		resolver.DataDir = dataDir
	}
	res, err := resolver.ResolveStreamlink(cfgPath)
	if err == nil {
		b.adopt(res)
		return nil
	}

	if !errors.Is(err, components.ErrNotFound) {
		return err
	}

	if resolver.GOOS == "darwin" {
		return &player.MissingToolError{Tool: "streamlink"}
	}

	res, err = b.downloader().DownloadStreamlink(ctx, func(component string, percent int, readBytes, totalBytes int64) {
		b.mu.Lock()
		sender := b.sender
		b.mu.Unlock()
		if sender != nil {
			sender(ui.ComponentProgressMsg{
				Component: component,
				Percent:   percent,
				Read:      readBytes,
				Total:     totalBytes,
				Waiting:   percent < 0,
			})
		}
	})
	b.mu.Lock()
	sender := b.sender
	b.mu.Unlock()
	if sender != nil {
		sender(ui.ComponentProgressMsg{Done: true})
	}
	if err != nil {
		return err
	}

	b.adopt(res)
	return nil
}

func (b *playbackBridge) Start(login string, selection uint64) tea.Cmd {
	return b.StartTarget(ui.PlayTarget{
		Channel: catalog.Channel{
			Platform: platform.Twitch,
			Login:    login,
		},
	}, selection)
}

func (b *playbackBridge) StartTarget(target ui.PlayTarget, selection uint64) tea.Cmd {
	mgr := b.mgr
	b.mu.Lock()
	reg := b.registry
	b.mu.Unlock()
	if reg == nil {
		reg = provider.NewRegistry(twitch.NewProvider(nil), provider.NewURLProvider())
	}

	ch := target.Channel
	pTarget, err := reg.StreamTarget(ch)
	if err != nil {
		label := ch.PlayerLabel()
		if label == "" {
			label = ch.Login
		}
		return func() tea.Msg {
			return ui.PlayerStatusMsg{
				State:    ui.PlayerStateError,
				Login:    label,
				LastLine: err.Error(),
				Failure:  ui.PlayerFailureUnknown,
			}
		}
	}
	playTarget := player.Target{
		URL:        pTarget.URL,
		ExtraArgs:  pTarget.ExtraArgs,
		Plugins:    playerPlugins(pTarget.Plugins),
		Label:      pTarget.Label,
		NoAdSignal: pTarget.NoAdSignal,
	}
	identity := playTarget.Identity()
	return func() tea.Msg {
		b.mu.Lock()
		appCtx := b.ctx
		if appCtx == nil {
			appCtx = context.Background()
		}
		b.mu.Unlock()

		if err := b.ensureStreamlink(appCtx); err != nil {
			gen := mgr.Generation()
			lastLine := err.Error()
			var missing *player.MissingToolError
			isMissing := errors.As(err, &missing)
			if isMissing {
				lastLine = missing.Error()
			}
			return ui.PlayerStatusMsg{
				Generation:     gen,
				State:          ui.PlayerStateError,
				Login:          identity,
				LastLine:       lastLine,
				Failure:        ui.PlayerFailureStreamlinkMissing,
				DownloadFailed: !isMissing,
				Selection:      selection,
			}
		}

		// Cmds run in goroutines that may not keep the order of the selections.
		gen, err := mgr.StartInOrder(playTarget, selection)
		if err != nil {
			// Só um erro de exec culpa a versão: um erro de validação repete a
			// URL alvo, texto de terceiros que não diz nada sobre o streamlink.
			var execErr *player.ExecError
			if errors.As(err, &execErr) && player.FailureOf(err).BlamesStreamlink() {
				// The binary cannot even run: if it is an update still on
				// trial, go back to the previous one.
				b.mu.Lock()
				res, managed := b.resolved, b.hasResolved
				b.mu.Unlock()
				if managed && res.Origin == components.OriginManaged {
					b.rollbackAsync(res.Version)
				}
			}
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
			Muted:      mgr.Muted(),
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

func (b *playbackBridge) ToggleMute() tea.Cmd {
	mgr := b.mgr
	return func() tea.Msg {
		_ = mgr.ToggleMute()
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
			ev.Failure = player.ClassifyTargetLine(ev.LastLine, ev.Target)
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
		Muted:        ev.Muted,
		Failure:      ui.PlayerFailure(ev.Failure),

		InputConfFailed:     ev.InputConfFailed,
		ContainmentDegraded: ev.ContainmentDegraded,
	}
}

// logInputConfErr records why the mpv window ran without keys. It runs after the
// TUI exits; the status bar already warned during playback.
func logInputConfErr(log io.Writer, mgr *player.Manager, cat *i18n.Catalog) {
	if err := mgr.InputConfErr(); err != nil {
		writeInputConfWarning(log, err, cat)
	}
}

// writeInputConfWarning prints the key-file failure. The error carries the
// runtime path, so it goes through termtext.Line.
func writeInputConfWarning(log io.Writer, err error, cat *i18n.Catalog) {
	_, _ = fmt.Fprintf(log, cat.T("app.warn_input_conf")+"\n", termtext.Line(err.Error()))
}

// playerPlugins converts the provider's streamlink plugins for the player.
func playerPlugins(plugins []catalog.StreamlinkPlugin) []player.Plugin {
	if len(plugins) == 0 {
		return nil
	}
	out := make([]player.Plugin, len(plugins))
	for i, p := range plugins {
		out[i] = player.Plugin{Name: p.Name, Source: p.Source}
	}
	return out
}

// logPlayerNotes records what the player sessions noted while the TUI owned the
// terminal.
func logPlayerNotes(log io.Writer, notes []string, cat *i18n.Catalog) {
	for _, n := range notes {
		_, _ = fmt.Fprintf(log, cat.T("app.warn_player")+"\n", termtext.Line(n))
	}
}

// forwardPlayerEvents sends Manager events into the running tea program, after
// showing each to observe when it is set.
// It returns when ctx is cancelled or the Events channel is closed.
func forwardPlayerEvents(ctx context.Context, p *tea.Program, mgr *player.Manager, observe func(player.Event)) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-mgr.Events():
			if !ok {
				return
			}
			if observe != nil {
				observe(ev)
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
