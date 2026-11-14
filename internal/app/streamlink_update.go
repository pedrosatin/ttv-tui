package app

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/components"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/player"
	"github.com/pedrosatin/ttv-tui/internal/termtext"
)

// closeWait bounds how long closing the bridge waits for background work to
// notice that the app is exiting.
const closeWait = 5 * time.Second

// streamlinkRollback records that a managed streamlink update was undone.
type streamlinkRollback struct {
	From string
	To   string
}

// downloader is the one used for the first install and for updates.
func (b *playbackBridge) downloader() *components.Downloader {
	if b.down != nil {
		return b.down
	}
	return components.NewDownloader(b.dataDir)
}

// background runs fn off the caller's goroutine with the app's context and
// lets close wait for it.
func (b *playbackBridge) background(fn func(ctx context.Context)) {
	b.mu.Lock()
	ctx := b.ctx
	b.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	b.bg.Add(1)
	go func() {
		defer b.bg.Done()
		fn(ctx)
	}()
}

// startUpdateCheck looks for a newer managed streamlink in the background, at
// most once a day. Playback and start-up never wait for it. The newer version
// is used from the next start, so a channel being watched is never disturbed.
func (b *playbackBridge) startUpdateCheck() {
	if b.mgr == nil || !b.mgr.UsesStreamlink() {
		return
	}
	b.background(func(ctx context.Context) {
		_, _ = b.checkStreamlinkUpdate(ctx)
	})
}

// checkStreamlinkUpdate updates the streamlink only when the one in use is the
// managed one. A path from the config and a streamlink on PATH belong to the
// user and are never touched.
func (b *playbackBridge) checkStreamlinkUpdate(ctx context.Context) (components.UpdateResult, error) {
	if b.mgr == nil || !b.mgr.UsesStreamlink() {
		return components.UpdateResult{}, nil
	}
	resolver := components.NewResolver()
	if b.dataDir != "" {
		resolver.DataDir = b.dataDir
	}
	res, err := resolver.ResolveStreamlink(b.cfgStreamlink)
	if err != nil || res.Origin != components.OriginManaged {
		return components.UpdateResult{}, nil
	}
	return b.downloader().UpdateStreamlink(ctx)
}

// adopt makes res the streamlink for the next playback and, for a managed
// one, marks the version as in use so another instance's cleanup spares it.
func (b *playbackBridge) adopt(res components.Result) {
	b.mu.Lock()
	b.resolved = res
	b.hasResolved = true
	b.mgr.SetStreamlink(res.Path, res.Env...)
	b.mu.Unlock()
	b.markInUse(res)
}

// markInUse holds the use marker for the managed version in res, replacing the
// one for the version used before.
func (b *playbackBridge) markInUse(res components.Result) {
	if res.Origin != components.OriginManaged {
		return
	}
	b.useMu.Lock()
	defer b.useMu.Unlock()
	if b.useVersion == res.Version {
		return
	}
	release, err := b.downloader().AcquireStreamlinkUse(res.Version)
	if err != nil {
		// Without the marker the version could be pruned by another instance;
		// playback still works, so this is not worth failing a start for.
		return
	}
	if b.useRelease != nil {
		_ = b.useRelease()
	}
	b.useRelease, b.useVersion = release, res.Version
}

// observePlayer watches player events for what they say about the managed
// streamlink: a first successful playback ends the trial of an updated
// version, and a failure of the build itself on trial undoes the update.
func (b *playbackBridge) observePlayer(ev player.Event) {
	b.mu.Lock()
	res, ok := b.resolved, b.hasResolved
	confirmed := b.confirmedVersion == res.Version
	b.mu.Unlock()
	if !ok || res.Origin != components.OriginManaged {
		return
	}
	switch {
	case ev.State == player.StatePlaying && !confirmed:
		b.mu.Lock()
		b.confirmedVersion = res.Version
		b.mu.Unlock()
		b.background(func(ctx context.Context) {
			_ = b.downloader().ConfirmStreamlink(ctx, res.Version)
		})
	case ev.State == player.StateError && ev.Failure.BlamesStreamlink() && !confirmed:
		// confirmed is set in memory as soon as the version plays, before the
		// background ConfirmStreamlink reaches the disk: checking it here keeps
		// a failure right after the first playback from marking a good
		// version bad.
		b.rollbackAsync(res.Version)
	}
}

// rollbackAsync goes back to the previous managed version when version is an
// update still on trial.
func (b *playbackBridge) rollbackAsync(version string) {
	b.background(func(ctx context.Context) {
		b.rollback(ctx, version)
	})
}

// rollback marks version bad and switches the next playback to the previous
// version. Only a failure of the build counts (player.Failure.BlamesStreamlink):
// an offline channel, the network or an unsupported URL never get here. A
// version that is not on trial, or with nothing to go back to, is left alone.
func (b *playbackBridge) rollback(ctx context.Context, version string) {
	d := b.downloader()
	prev, err := d.MarkStreamlinkBad(ctx, version)
	if err != nil {
		return
	}
	b.mu.Lock()
	if b.resolved.Version == version {
		b.resolved = prev
		b.mgr.SetStreamlink(prev.Path, prev.Env...)
	}
	b.rollbacks = append(b.rollbacks, streamlinkRollback{From: version, To: prev.Version})
	b.mu.Unlock()
	b.markInUse(prev)
	_ = d.PruneStreamlink(ctx)
}

// Rollbacks lists the streamlink updates undone so far.
func (b *playbackBridge) Rollbacks() []streamlinkRollback {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]streamlinkRollback(nil), b.rollbacks...)
}

// close lets background work finish and drops the use marker. The app context
// is already canceled by now, so the work returns quickly.
func (b *playbackBridge) close() {
	done := make(chan struct{})
	go func() {
		b.bg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(closeWait):
	}
	b.useMu.Lock()
	defer b.useMu.Unlock()
	if b.useRelease != nil {
		_ = b.useRelease()
		b.useRelease, b.useVersion = nil, ""
	}
}

// logStreamlinkRollbacks tells, after the TUI exits, which streamlink update
// was undone: the screen only showed the failure of the playback.
func logStreamlinkRollbacks(log io.Writer, rollbacks []streamlinkRollback, cat *i18n.Catalog) {
	for _, r := range rollbacks {
		_, _ = fmt.Fprintf(log, cat.T("app.warn_streamlink_rollback")+"\n", termtext.Line(r.From), termtext.Line(r.To))
	}
}
