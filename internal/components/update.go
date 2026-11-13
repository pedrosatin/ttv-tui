package components

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/filelock"
)

const (
	// UpdateInterval is the least time between two checks for a newer streamlink.
	UpdateInterval = 24 * time.Hour
	// keepVersions is how many managed versions stay on disk: the current one
	// and the one to go back to.
	keepVersions = 2

	// DefaultUseStaleAge is how long a process's use marker survives without a
	// heartbeat. Pruning treats a version as free once every marker is older.
	DefaultUseStaleAge = 10 * time.Minute
	// DefaultUseHeartbeat is how often a running process renews its use marker.
	DefaultUseHeartbeat = time.Minute

	useMarkerPrefix = ".use-"
	useMarkerSuffix = ".lock"
)

var (
	// ErrNotOnTrial means the version is not an update waiting for its first
	// successful playback, so there is nothing to undo.
	ErrNotOnTrial = errors.New("streamlink version is not on trial")
	// ErrNoPrevious means no other usable version is installed to go back to.
	ErrNoPrevious = errors.New("no previous streamlink version to go back to")
)

// UpdateStatus is what UpdateStreamlink did.
type UpdateStatus string

const (
	// UpdateNotDue: the last check is less than UpdateInterval old.
	UpdateNotDue UpdateStatus = "not_due"
	// UpdateNotManaged: no managed streamlink is installed, so there is nothing
	// to update.
	UpdateNotManaged UpdateStatus = "not_managed"
	// UpdateBusy: another process holds the components lock; the check was
	// skipped and stays due.
	UpdateBusy UpdateStatus = "busy"
	// UpdateCurrent: the installed version is the newest, or the newest was
	// marked bad.
	UpdateCurrent UpdateStatus = "current"
	// UpdateInstalled: a newer, verified version was installed and is now the
	// current one; the previous version was kept.
	UpdateInstalled UpdateStatus = "installed"
)

// UpdateResult describes one UpdateStreamlink call.
type UpdateResult struct {
	Status UpdateStatus
	// Version is the installed version for UpdateInstalled.
	Version string
	// Previous is the version it replaced for UpdateInstalled.
	Previous string
}

func (d *Downloader) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d *Downloader) useStaleAge() time.Duration {
	if d.UseStaleAge > 0 {
		return d.UseStaleAge
	}
	return DefaultUseStaleAge
}

func (d *Downloader) useHeartbeat() time.Duration {
	if d.UseHeartbeat > 0 {
		return d.UseHeartbeat
	}
	return DefaultUseHeartbeat
}

func (d *Downloader) resolver() *Resolver {
	return &Resolver{
		DataDir:  d.DataDir,
		GOOS:     d.GOOS,
		GOARCH:   d.GOARCH,
		LookPath: d.LookPath,
		HasFUSE:  DefaultHasFUSE,
	}
}

// checkDue reports whether a check for a newer version is allowed at now. A
// last check in the future (a clock set back) counts as due, so a wrong clock
// cannot stop updates for good.
func checkDue(st UpdateState, now time.Time) bool {
	if st.LastCheck.IsZero() {
		return true
	}
	elapsed := now.Sub(st.LastCheck)
	return elapsed < 0 || elapsed >= UpdateInterval
}

// UpdateStreamlink looks for a newer managed streamlink, at most once per
// UpdateInterval, and installs it next to the current one. The new version is
// downloaded and its SHA-256 checked by the same path as the first install, and
// only becomes the one in use after it is complete on disk. It never touches a
// streamlink that was not installed by this package, and never installs a
// version marked bad. The new version starts on trial (see UpdateState.Trial).
// At most the new and the previous version stay on disk afterwards.
func (d *Downloader) UpdateStreamlink(ctx context.Context) (UpdateResult, error) {
	repo, err := d.repoForPlatform()
	if err != nil {
		return UpdateResult{}, err
	}
	resolver := d.resolver()
	if !checkDue(LoadUpdateState(d.DataDir), d.now()) {
		return UpdateResult{Status: UpdateNotDue}, nil
	}
	if _, ok := resolver.findManagedStreamlink(); !ok {
		return UpdateResult{Status: UpdateNotManaged}, nil
	}

	baseDir := StreamlinkBaseDir(d.DataDir)
	// Never wait for the lock: a background check yields to a download or
	// another update and runs again on the next start.
	lockCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	busy := false
	unlock, err := filelock.LockWithHeartbeat(lockCtx, filepath.Join(baseDir, ".lock"), d.lockStaleAge(), d.lockHeartbeat(), func() {
		busy = true
		cancel()
	})
	if err != nil {
		if busy && ctx.Err() == nil {
			return UpdateResult{Status: UpdateBusy}, nil
		}
		return UpdateResult{}, fmt.Errorf("acquire download lock: %w", err)
	}
	defer func() { _ = unlock() }()
	if d.OnLockAcquired != nil {
		d.OnLockAcquired()
	}

	// Another process may have checked while this one waited.
	if !checkDue(LoadUpdateState(d.DataDir), d.now()) {
		return UpdateResult{Status: UpdateNotDue}, nil
	}
	current, ok := resolver.findManagedStreamlink()
	if !ok {
		return UpdateResult{Status: UpdateNotManaged}, nil
	}
	cleanStaleTemps(baseDir)

	rel, err := d.fetchLatest(ctx, repo)
	if err != nil {
		return UpdateResult{}, err
	}

	// The check counts once the release is known, whatever follows: a failing
	// download must not repeat on every start.
	skip := false
	if err := d.updateState(ctx, func(st *UpdateState) (bool, error) {
		st.LastCheck = d.now()
		skip = st.IsBad(rel.Version) || compareVersions(rel.Version, current.Version) <= 0
		return true, nil
	}); err != nil {
		return UpdateResult{}, err
	}
	if err := d.step(stepChecked); err != nil {
		return UpdateResult{}, err
	}
	if skip {
		return UpdateResult{Status: UpdateCurrent}, nil
	}

	// A versão nova entra em teste antes do rename que a torna atual. Uma
	// interrupção depois da gravação e antes do rename deixa a anterior atual e
	// um teste de uma versão ausente, que nenhuma reprodução confirma nem desfaz;
	// a ordem inversa podia deixar a nova atual sem teste e sem volta.
	if err := d.installRelease(ctx, baseDir, rel, nil, d.recordTrial(rel.Version)); err != nil {
		return UpdateResult{}, err
	}
	if err := d.step(stepPromoted); err != nil {
		return UpdateResult{}, err
	}
	d.pruneLocked(baseDir)
	if err := d.step(stepPruned); err != nil {
		return UpdateResult{}, err
	}

	installed, ok := resolver.findManagedStreamlink()
	if !ok || installed.Version != rel.Version {
		return UpdateResult{}, fmt.Errorf("managed streamlink %s not usable after install", rel.Version)
	}
	return UpdateResult{Status: UpdateInstalled, Version: installed.Version, Previous: current.Version}, nil
}

// recordTrial devolve o passo que põe version em teste antes da promoção. O
// desfazer devolve o teste anterior quando a promoção não acontece, para que um
// erro ou um cancelamento não deixe em teste uma versão que não foi instalada.
func (d *Downloader) recordTrial(version string) beforePromoteFunc {
	return func(ctx context.Context) (func(), error) {
		previous := ""
		if err := d.updateState(ctx, func(st *UpdateState) (bool, error) {
			previous = st.Trial
			st.Trial = version
			return true, nil
		}); err != nil {
			return nil, fmt.Errorf("record trial: %w", err)
		}
		undo := func() {
			// O contexto pode já ter caído: o desfazer roda mesmo assim.
			_ = d.updateState(context.WithoutCancel(ctx), func(st *UpdateState) (bool, error) {
				if st.Trial != version {
					return false, nil
				}
				st.Trial = previous
				return true, nil
			})
		}
		return undo, nil
	}
}

// ConfirmStreamlink ends the trial of version after it played a stream. It does
// nothing when the version is not on trial.
func (d *Downloader) ConfirmStreamlink(ctx context.Context, version string) error {
	if LoadUpdateState(d.DataDir).Trial != version {
		return nil
	}
	return d.updateState(ctx, func(st *UpdateState) (bool, error) {
		if st.Trial != version {
			return false, nil
		}
		st.Trial = ""
		return true, nil
	})
}

// MarkStreamlinkBad records that version, an update still on trial, failed on
// its own, so it is never downloaded or used again, and returns the previous
// version to use instead. It returns ErrNotOnTrial for a version that is not on
// trial and ErrNoPrevious when nothing else is installed; nothing changes then.
// Callers decide what counts as a failure of the build itself (see
// player.Failure.BlamesStreamlink). The bad version's files go away on the next
// PruneStreamlink, once no process uses them.
func (d *Downloader) MarkStreamlinkBad(ctx context.Context, version string) (Result, error) {
	resolver := d.resolver()
	var res Result
	err := d.updateState(ctx, func(st *UpdateState) (bool, error) {
		if st.Trial != version {
			return false, ErrNotOnTrial
		}
		if !st.IsBad(version) {
			st.Bad = append(st.Bad, version)
		}
		st.Trial = ""
		installed := resolver.installedStreamlinks(*st)
		if len(installed) == 0 {
			return false, ErrNoPrevious
		}
		res = resolver.managedResult(installed[0])
		return true, nil
	})
	if err != nil {
		return Result{}, err
	}
	return res, nil
}

// PruneStreamlink deletes managed versions beyond the current and the previous
// one, and versions marked bad, sparing any version another process still uses.
// It waits for the components lock.
func (d *Downloader) PruneStreamlink(ctx context.Context) error {
	baseDir := StreamlinkBaseDir(d.DataDir)
	unlock, err := filelock.LockWithHeartbeat(ctx, filepath.Join(baseDir, ".lock"), d.lockStaleAge(), d.lockHeartbeat(), nil)
	if err != nil {
		return fmt.Errorf("acquire download lock: %w", err)
	}
	defer func() { _ = unlock() }()
	d.pruneLocked(baseDir)
	return nil
}

// pruneLocked is PruneStreamlink with the components lock held.
func (d *Downloader) pruneLocked(baseDir string) {
	resolver := d.resolver()
	keep := map[string]bool{}
	for i, m := range resolver.installedStreamlinks(LoadUpdateState(d.DataDir)) {
		if i < keepVersions {
			keep[m.Version] = true
		}
	}
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return
	}
	inUse := d.versionsInUse(baseDir, entries)
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || strings.HasPrefix(name, ".") || keep[name] || inUse[name] {
			continue
		}
		_ = os.RemoveAll(filepath.Join(baseDir, name))
	}
}

// versionsInUse lists the versions some live process holds a use marker for,
// and removes the markers of processes that stopped renewing theirs.
func (d *Downloader) versionsInUse(baseDir string, entries []os.DirEntry) map[string]bool {
	inUse := map[string]bool{}
	for _, e := range entries {
		version, ok := parseUseMarker(e.Name())
		if !ok || e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if time.Since(info.ModTime()) > d.useStaleAge() {
			_ = os.Remove(filepath.Join(baseDir, e.Name()))
			continue
		}
		inUse[version] = true
	}
	return inUse
}

// useMarkerName is the file a process keeps fresh while it uses version. The
// lock helper appends the ".lock" suffix.
func useMarkerName(pid int, version string) string {
	return useMarkerPrefix + strconv.Itoa(pid) + "-" + version
}

// parseUseMarker returns the version a use marker file name stands for.
func parseUseMarker(name string) (version string, ok bool) {
	rest, found := strings.CutPrefix(name, useMarkerPrefix)
	if !found {
		return "", false
	}
	rest, found = strings.CutSuffix(rest, useMarkerSuffix)
	if !found {
		return "", false
	}
	pidText, version, found := strings.Cut(rest, "-")
	if !found || version == "" {
		return "", false
	}
	if _, err := strconv.Atoi(pidText); err != nil {
		return "", false
	}
	return version, true
}

// AcquireStreamlinkUse marks version as in use by this process until release is
// called, so pruning in another process spares it. The marker is renewed in the
// background and expires on its own if the process dies.
func (d *Downloader) AcquireStreamlinkUse(version string) (release func() error, err error) {
	if err := validateTagName(version); err != nil {
		return nil, fmt.Errorf("invalid version: %w", err)
	}
	baseDir := StreamlinkBaseDir(d.DataDir)
	// The marker is private to this process, so acquiring never waits long;
	// the timeout covers a leftover of a dead process that reused this pid.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return filelock.LockWithHeartbeat(ctx, filepath.Join(baseDir, useMarkerName(os.Getpid(), version)), d.useStaleAge(), d.useHeartbeat(), nil)
}
