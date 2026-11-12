package components

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// releaseServer is a fake GitHub API plus asset host. Tests change what the
// "latest release" is, and count the requests it receives.
type releaseServer struct {
	*httptest.Server

	mu          sync.Mutex
	tag         string
	payload     []byte
	wrongDigest bool
	apiHits     int
	assetHits   int
}

func newReleaseServer(t *testing.T, tag string) *releaseServer {
	t.Helper()
	rs := &releaseServer{tag: tag, payload: []byte("fake appimage payload " + tag)}
	rs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs.mu.Lock()
		defer rs.mu.Unlock()
		switch r.URL.Path {
		case "/api/releases/latest":
			rs.apiHits++
			sum := sha256.Sum256(rs.payload)
			digest := "sha256:" + hex.EncodeToString(sum[:])
			if rs.wrongDigest {
				digest = "sha256:" + strings.Repeat("0", 64)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(gitHubRelease{
				TagName: rs.tag,
				Assets: []gitHubAsset{{
					Name:               "streamlink-" + rs.tag + "-cp315-cp315-manylinux_2_28_x86_64.AppImage",
					BrowserDownloadURL: rs.URL + "/assets/streamlink.AppImage",
					Size:               int64(len(rs.payload)),
					Digest:             digest,
				}},
			})
		case "/assets/streamlink.AppImage":
			rs.assetHits++
			_, _ = w.Write(rs.payload)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(rs.Close)
	return rs
}

func (rs *releaseServer) setTag(tag string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.tag = tag
	rs.payload = []byte("fake appimage payload " + tag)
}

func (rs *releaseServer) hits() (api, assets int) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.apiHits, rs.assetHits
}

// fakeClock is a settable clock for the update checks.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newUpdateDownloader(t *testing.T, rs *releaseServer, dataDir string, clock *fakeClock) *Downloader {
	t.Helper()
	u, err := url.Parse(rs.URL)
	if err != nil {
		t.Fatal(err)
	}
	return &Downloader{
		Transport:    rs.Client().Transport,
		AllowedHosts: []string{u.Host},
		APIBaseURL:   rs.URL + "/api/releases/latest",
		DataDir:      dataDir,
		GOOS:         "linux",
		GOARCH:       "amd64",
		InsecureHTTP: true,
		Now:          clock.now,
		ExtractAppImage: func(_ context.Context, _, destDir string) error {
			dir := filepath.Join(destDir, "squashfs-root")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(dir, "AppRun"), []byte("#!/bin/sh\n"), 0o755)
		},
	}
}

// installFakeVersion lays out an installed managed streamlink version.
func installFakeVersion(t *testing.T, dataDir, version string) {
	t.Helper()
	dir := filepath.Join(StreamlinkVersionDir(dataDir, version), "squashfs-root")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AppRun"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func versionDirExists(dataDir, version string) bool {
	_, err := os.Stat(StreamlinkVersionDir(dataDir, version))
	return err == nil
}

func linuxResolver(dataDir string) *Resolver {
	return &Resolver{DataDir: dataDir, GOOS: "linux", GOARCH: "amd64", HasFUSE: func() bool { return true }}
}

func newClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)}
}

func TestUpdateChecksAtMostOncePerDay(t *testing.T) {
	dataDir := t.TempDir()
	installFakeVersion(t, dataDir, "8.6.3-1")
	rs := newReleaseServer(t, "8.6.3-1")
	clock := newClock()
	d := newUpdateDownloader(t, rs, dataDir, clock)
	ctx := context.Background()

	res, err := d.UpdateStreamlink(ctx)
	if err != nil || res.Status != UpdateCurrent {
		t.Fatalf("first check = %+v, %v; want current", res, err)
	}
	if api, _ := rs.hits(); api != 1 {
		t.Fatalf("api hits = %d, want 1", api)
	}
	if got := LoadUpdateState(dataDir).LastCheck; !got.Equal(clock.now()) {
		t.Fatalf("last check = %v, want %v", got, clock.now())
	}

	// Same day, even after a restart (a new Downloader reads the saved date).
	clock.advance(23 * time.Hour)
	d = newUpdateDownloader(t, rs, dataDir, clock)
	res, err = d.UpdateStreamlink(ctx)
	if err != nil || res.Status != UpdateNotDue {
		t.Fatalf("same-day check = %+v, %v; want not due", res, err)
	}
	if api, _ := rs.hits(); api != 1 {
		t.Fatalf("api hits after same-day check = %d, want 1 (no second check)", api)
	}

	clock.advance(2 * time.Hour)
	res, err = d.UpdateStreamlink(ctx)
	if err != nil || res.Status != UpdateCurrent {
		t.Fatalf("next-day check = %+v, %v; want current", res, err)
	}
	if api, _ := rs.hits(); api != 2 {
		t.Fatalf("api hits next day = %d, want 2", api)
	}
}

func TestUpdateDueWhenClockWentBack(t *testing.T) {
	now := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	if !checkDue(UpdateState{LastCheck: now.Add(48 * time.Hour)}, now) {
		t.Fatal("a last check in the future must not block updates")
	}
	if checkDue(UpdateState{LastCheck: now.Add(-time.Hour)}, now) {
		t.Fatal("a check one hour ago must not be due")
	}
	if !checkDue(UpdateState{}, now) {
		t.Fatal("no check recorded must be due")
	}
}

func TestUpdateInstallsVerifiedVersionBesideCurrent(t *testing.T) {
	dataDir := t.TempDir()
	installFakeVersion(t, dataDir, "8.6.3-1")
	rs := newReleaseServer(t, "8.7.0")
	d := newUpdateDownloader(t, rs, dataDir, newClock())

	res, err := d.UpdateStreamlink(context.Background())
	if err != nil {
		t.Fatalf("UpdateStreamlink: %v", err)
	}
	if res.Status != UpdateInstalled || res.Version != "8.7.0" || res.Previous != "8.6.3-1" {
		t.Fatalf("result = %+v", res)
	}
	current, previous := linuxResolver(dataDir).ManagedVersions()
	if current != "8.7.0" || previous != "8.6.3-1" {
		t.Fatalf("versions = %q, %q; want 8.7.0 and 8.6.3-1", current, previous)
	}
	if !versionDirExists(dataDir, "8.6.3-1") {
		t.Fatal("previous version must stay installed")
	}
	if got := LoadUpdateState(dataDir).Trial; got != "8.7.0" {
		t.Fatalf("trial = %q, want the new version", got)
	}
}

func TestUpdateWrongDigestKeepsCurrentVersion(t *testing.T) {
	dataDir := t.TempDir()
	installFakeVersion(t, dataDir, "8.6.3-1")
	rs := newReleaseServer(t, "8.7.0")
	rs.wrongDigest = true
	clock := newClock()
	d := newUpdateDownloader(t, rs, dataDir, clock)

	_, err := d.UpdateStreamlink(context.Background())
	if err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("error = %v, want a digest mismatch", err)
	}
	current, previous := linuxResolver(dataDir).ManagedVersions()
	if current != "8.6.3-1" || previous != "" {
		t.Fatalf("versions = %q, %q; the current version must not change", current, previous)
	}
	if versionDirExists(dataDir, "8.7.0") {
		t.Fatal("unverified version was installed")
	}
	entries, _ := os.ReadDir(StreamlinkBaseDir(dataDir))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".download-") || strings.HasPrefix(e.Name(), ".install-") {
			t.Fatalf("temporary directory left behind: %s", e.Name())
		}
	}
	st := LoadUpdateState(dataDir)
	if st.Trial != "" || len(st.Bad) != 0 {
		t.Fatalf("state = %+v; a failed download must not start a trial or mark anything", st)
	}
	// The failed attempt counts as today's check: no retry download loop.
	clock.advance(time.Hour)
	if res, err := d.UpdateStreamlink(context.Background()); err != nil || res.Status != UpdateNotDue {
		t.Fatalf("retry the same day = %+v, %v; want not due", res, err)
	}
}

func TestUpdateSkipsVersionMarkedBad(t *testing.T) {
	dataDir := t.TempDir()
	installFakeVersion(t, dataDir, "8.6.3-1")
	if err := saveUpdateState(dataDir, UpdateState{Bad: []string{"8.7.0"}}); err != nil {
		t.Fatal(err)
	}
	rs := newReleaseServer(t, "8.7.0")
	d := newUpdateDownloader(t, rs, dataDir, newClock())

	res, err := d.UpdateStreamlink(context.Background())
	if err != nil || res.Status != UpdateCurrent {
		t.Fatalf("result = %+v, %v; want current", res, err)
	}
	if _, assets := rs.hits(); assets != 0 {
		t.Fatalf("asset downloads = %d; a version marked bad must not be downloaded again", assets)
	}
	if versionDirExists(dataDir, "8.7.0") {
		t.Fatal("bad version was installed")
	}
}

func TestUpdateIgnoresOlderOrEqualRelease(t *testing.T) {
	dataDir := t.TempDir()
	installFakeVersion(t, dataDir, "8.10.0")
	rs := newReleaseServer(t, "8.9.5")
	d := newUpdateDownloader(t, rs, dataDir, newClock())

	res, err := d.UpdateStreamlink(context.Background())
	if err != nil || res.Status != UpdateCurrent {
		t.Fatalf("result = %+v, %v; want current", res, err)
	}
	if _, assets := rs.hits(); assets != 0 {
		t.Fatalf("asset downloads = %d for an older release", assets)
	}
}

func TestUpdateWithoutManagedInstallDoesNothing(t *testing.T) {
	dataDir := t.TempDir()
	rs := newReleaseServer(t, "8.7.0")
	d := newUpdateDownloader(t, rs, dataDir, newClock())

	res, err := d.UpdateStreamlink(context.Background())
	if err != nil || res.Status != UpdateNotManaged {
		t.Fatalf("result = %+v, %v; want not managed", res, err)
	}
	if api, _ := rs.hits(); api != 0 {
		t.Fatalf("api hits = %d; nothing managed means no network", api)
	}
}

func TestUpdateYieldsToHeldLock(t *testing.T) {
	dataDir := t.TempDir()
	installFakeVersion(t, dataDir, "8.6.3-1")
	rs := newReleaseServer(t, "8.7.0")
	d := newUpdateDownloader(t, rs, dataDir, newClock())
	lock := filepath.Join(StreamlinkBaseDir(dataDir), ".lock.lock")
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := d.UpdateStreamlink(context.Background())
	if err != nil || res.Status != UpdateBusy {
		t.Fatalf("result = %+v, %v; want busy", res, err)
	}
	if api, _ := rs.hits(); api != 0 {
		t.Fatalf("api hits = %d while another process holds the lock", api)
	}
	if !LoadUpdateState(dataDir).LastCheck.IsZero() {
		t.Fatal("a skipped check must stay due")
	}
}

func TestUpdateKeepsOnlyCurrentAndPrevious(t *testing.T) {
	dataDir := t.TempDir()
	installFakeVersion(t, dataDir, "8.5.0")
	installFakeVersion(t, dataDir, "8.6.0")
	rs := newReleaseServer(t, "8.7.0")
	d := newUpdateDownloader(t, rs, dataDir, newClock())

	if res, err := d.UpdateStreamlink(context.Background()); err != nil || res.Status != UpdateInstalled {
		t.Fatalf("result = %+v, %v", res, err)
	}
	if versionDirExists(dataDir, "8.5.0") {
		t.Fatal("the oldest version must be pruned")
	}
	if !versionDirExists(dataDir, "8.6.0") || !versionDirExists(dataDir, "8.7.0") {
		t.Fatal("current and previous versions must stay")
	}
}

func TestRollbackMarksVersionBadAndItIsNotDownloadedAgain(t *testing.T) {
	dataDir := t.TempDir()
	installFakeVersion(t, dataDir, "8.6.3-1")
	rs := newReleaseServer(t, "8.7.0")
	clock := newClock()
	d := newUpdateDownloader(t, rs, dataDir, clock)
	ctx := context.Background()

	if res, err := d.UpdateStreamlink(ctx); err != nil || res.Status != UpdateInstalled {
		t.Fatalf("update = %+v, %v", res, err)
	}
	_, assetsBefore := rs.hits()

	back, err := d.MarkStreamlinkBad(ctx, "8.7.0")
	if err != nil {
		t.Fatalf("MarkStreamlinkBad: %v", err)
	}
	if back.Version != "8.6.3-1" || back.Origin != OriginManaged {
		t.Fatalf("rolled back to %+v, want 8.6.3-1", back)
	}
	resolved, err := linuxResolver(dataDir).ResolveStreamlink("")
	if err != nil || resolved.Version != "8.6.3-1" {
		t.Fatalf("resolver after rollback = %+v, %v; want the previous version", resolved, err)
	}
	st := LoadUpdateState(dataDir)
	if !st.IsBad("8.7.0") || st.Trial != "" {
		t.Fatalf("state = %+v; want 8.7.0 bad and no trial", st)
	}

	// Another day: the release is still the bad one, and is not fetched again.
	clock.advance(25 * time.Hour)
	res, err := d.UpdateStreamlink(ctx)
	if err != nil || res.Status != UpdateCurrent {
		t.Fatalf("check after rollback = %+v, %v; want current", res, err)
	}
	if _, assets := rs.hits(); assets != assetsBefore {
		t.Fatalf("asset downloads went from %d to %d; the bad version was downloaded again", assetsBefore, assets)
	}

	// A fixed release is picked up normally.
	rs.setTag("8.7.1")
	clock.advance(25 * time.Hour)
	res, err = d.UpdateStreamlink(ctx)
	if err != nil || res.Status != UpdateInstalled || res.Version != "8.7.1" {
		t.Fatalf("check with a fixed release = %+v, %v", res, err)
	}
}

func TestRollbackOnlyForVersionOnTrial(t *testing.T) {
	dataDir := t.TempDir()
	installFakeVersion(t, dataDir, "8.6.3-1")
	rs := newReleaseServer(t, "8.7.0")
	d := newUpdateDownloader(t, rs, dataDir, newClock())
	ctx := context.Background()

	// The long-working version is not on trial: its failures are not an update's fault.
	if _, err := d.MarkStreamlinkBad(ctx, "8.6.3-1"); !errors.Is(err, ErrNotOnTrial) {
		t.Fatalf("MarkStreamlinkBad(stable) = %v, want ErrNotOnTrial", err)
	}

	if _, err := d.UpdateStreamlink(ctx); err != nil {
		t.Fatal(err)
	}
	if err := d.ConfirmStreamlink(ctx, "8.7.0"); err != nil {
		t.Fatalf("ConfirmStreamlink: %v", err)
	}
	if got := LoadUpdateState(dataDir).Trial; got != "" {
		t.Fatalf("trial after a successful playback = %q, want none", got)
	}
	if _, err := d.MarkStreamlinkBad(ctx, "8.7.0"); !errors.Is(err, ErrNotOnTrial) {
		t.Fatalf("MarkStreamlinkBad(confirmed) = %v, want ErrNotOnTrial", err)
	}
	if st := LoadUpdateState(dataDir); st.IsBad("8.7.0") {
		t.Fatalf("a confirmed version was marked bad: %+v", st)
	}
}

func TestRollbackWithoutPreviousChangesNothing(t *testing.T) {
	dataDir := t.TempDir()
	installFakeVersion(t, dataDir, "8.7.0")
	if err := saveUpdateState(dataDir, UpdateState{Trial: "8.7.0"}); err != nil {
		t.Fatal(err)
	}
	d := &Downloader{DataDir: dataDir, GOOS: "linux", GOARCH: "amd64"}

	if _, err := d.MarkStreamlinkBad(context.Background(), "8.7.0"); !errors.Is(err, ErrNoPrevious) {
		t.Fatalf("error = %v, want ErrNoPrevious", err)
	}
	st := LoadUpdateState(dataDir)
	if st.IsBad("8.7.0") || st.Trial != "8.7.0" {
		t.Fatalf("state = %+v; nothing must change without a version to go back to", st)
	}
}

func TestResolverSkipsBadVersions(t *testing.T) {
	dataDir := t.TempDir()
	installFakeVersion(t, dataDir, "8.6.3-1")
	installFakeVersion(t, dataDir, "8.7.0")
	if err := saveUpdateState(dataDir, UpdateState{Bad: []string{"8.7.0"}}); err != nil {
		t.Fatal(err)
	}
	res, err := linuxResolver(dataDir).ResolveStreamlink("")
	if err != nil || res.Version != "8.6.3-1" {
		t.Fatalf("resolved = %+v, %v; want 8.6.3-1", res, err)
	}
}

func TestFirstInstallAcceptsVersionOnceMarkedBad(t *testing.T) {
	dataDir := t.TempDir()
	if err := saveUpdateState(dataDir, UpdateState{Bad: []string{"8.7.0"}}); err != nil {
		t.Fatal(err)
	}
	rs := newReleaseServer(t, "8.7.0")
	d := newUpdateDownloader(t, rs, dataDir, newClock())

	res, err := d.DownloadStreamlink(context.Background(), nil)
	if err != nil || res.Version != "8.7.0" {
		t.Fatalf("DownloadStreamlink = %+v, %v; with nothing else installed the latest is the only option", res, err)
	}
	if LoadUpdateState(dataDir).IsBad("8.7.0") {
		t.Fatal("the mark must be cleared when the version is installed again")
	}
}

func TestPruneKeepsTwoNewestAndDropsBad(t *testing.T) {
	dataDir := t.TempDir()
	for _, v := range []string{"8.4.0", "8.5.0", "8.6.0", "8.7.0"} {
		installFakeVersion(t, dataDir, v)
	}
	if err := saveUpdateState(dataDir, UpdateState{Bad: []string{"8.7.0"}}); err != nil {
		t.Fatal(err)
	}
	d := &Downloader{DataDir: dataDir, GOOS: "linux", GOARCH: "amd64"}

	if err := d.PruneStreamlink(context.Background()); err != nil {
		t.Fatalf("PruneStreamlink: %v", err)
	}
	for v, want := range map[string]bool{"8.4.0": false, "8.5.0": true, "8.6.0": true, "8.7.0": false} {
		if got := versionDirExists(dataDir, v); got != want {
			t.Errorf("version %s present = %v, want %v", v, got, want)
		}
	}
}

func TestPruneSparesVersionInUseByAnotherInstance(t *testing.T) {
	dataDir := t.TempDir()
	for _, v := range []string{"8.5.0", "8.6.0", "8.7.0"} {
		installFakeVersion(t, dataDir, v)
	}
	d := &Downloader{DataDir: dataDir, GOOS: "linux", GOARCH: "amd64"}

	release, err := d.AcquireStreamlinkUse("8.5.0")
	if err != nil {
		t.Fatalf("AcquireStreamlinkUse: %v", err)
	}
	if err := d.PruneStreamlink(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !versionDirExists(dataDir, "8.5.0") {
		t.Fatal("a version in use must not be deleted")
	}

	if err := release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := d.PruneStreamlink(context.Background()); err != nil {
		t.Fatal(err)
	}
	if versionDirExists(dataDir, "8.5.0") {
		t.Fatal("a version no longer in use must be pruned")
	}
}

func TestPruneIgnoresMarkerOfDeadProcess(t *testing.T) {
	dataDir := t.TempDir()
	for _, v := range []string{"8.5.0", "8.6.0", "8.7.0"} {
		installFakeVersion(t, dataDir, v)
	}
	d := &Downloader{DataDir: dataDir, GOOS: "linux", GOARCH: "amd64", UseStaleAge: time.Minute}
	marker := filepath.Join(StreamlinkBaseDir(dataDir), useMarkerName(999999, "8.5.0")+".lock")
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(marker, old, old); err != nil {
		t.Fatal(err)
	}

	if err := d.PruneStreamlink(context.Background()); err != nil {
		t.Fatal(err)
	}
	if versionDirExists(dataDir, "8.5.0") {
		t.Fatal("a marker nobody renews must not keep a version alive")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("stale marker was not cleaned up: %v", err)
	}
}

func TestUseMarkerNameRoundTrip(t *testing.T) {
	for _, v := range []string{"8.6.3-1", "8.7.0", "8.7.0-rc-2"} {
		got, ok := parseUseMarker(useMarkerName(4242, v) + ".lock")
		if !ok || got != v {
			t.Errorf("parseUseMarker(%q) = %q, %v", v, got, ok)
		}
	}
	for _, name := range []string{".lock", ".use-.lock", ".use-abc-8.7.0.lock", ".use-12-8.7.0", "8.7.0"} {
		if v, ok := parseUseMarker(name); ok {
			t.Errorf("parseUseMarker(%q) = %q, want no match", name, v)
		}
	}
}

func TestUpdateStateUnreadableIsZero(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.MkdirAll(StreamlinkBaseDir(dataDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stateFilePath(dataDir), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if st := LoadUpdateState(dataDir); !st.LastCheck.IsZero() || st.Trial != "" || len(st.Bad) != 0 {
		t.Fatalf("state = %+v, want the zero state", st)
	}
}

func TestSaveUpdateStateReplacesAtomicallyAndDropsLeftovers(t *testing.T) {
	dataDir := t.TempDir()
	baseDir := StreamlinkBaseDir(dataDir)
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	leftover := filepath.Join(baseDir, ".statetmp-crashed")
	if err := os.WriteFile(leftover, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := UpdateState{LastCheck: time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC), Trial: "8.7.0", Bad: []string{"8.6.9"}}
	if err := saveUpdateState(dataDir, want); err != nil {
		t.Fatal(err)
	}
	got := LoadUpdateState(dataDir)
	if !got.LastCheck.Equal(want.LastCheck) || got.Trial != want.Trial || !got.IsBad("8.6.9") {
		t.Fatalf("state round trip = %+v, want %+v", got, want)
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Fatalf("temp file of a dead writer was kept: %v", err)
	}
	// The temp name must survive cleanStaleTemps, which runs under another lock.
	live := filepath.Join(baseDir, ".statetmp-live")
	if err := os.WriteFile(live, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cleanStaleTemps(baseDir)
	if _, err := os.Stat(live); err != nil {
		t.Fatalf("cleanStaleTemps removed a live state writer's file: %v", err)
	}
}
