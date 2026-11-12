package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/components"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/player"
)

// skipOnDarwin skips a test that needs the bridge's own resolver to see managed
// installs, which macOS does not use.
func skipOnDarwin(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "darwin" {
		t.Skip("managed streamlink is not used on macOS")
	}
}

// installManagedVersion lays out a managed streamlink for every platform's
// resolver, so the bridge's own resolver finds it wherever the tests run.
func installManagedVersion(t *testing.T, dataDir, version string) {
	t.Helper()
	dir := components.StreamlinkVersionDir(dataDir, version)
	if err := os.MkdirAll(filepath.Join(dir, "squashfs-root"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{filepath.Join("squashfs-root", "AppRun"), "streamlink.exe"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func managedDirExists(dataDir, version string) bool {
	_, err := os.Stat(components.StreamlinkVersionDir(dataDir, version))
	return err == nil
}

func linuxResolver(dataDir string) *components.Resolver {
	return &components.Resolver{DataDir: dataDir, GOOS: "linux", GOARCH: "amd64", HasFUSE: func() bool { return true }}
}

// releaseAPI is a fake GitHub release API counting its requests.
type releaseAPI struct {
	*httptest.Server
	apiHits   atomic.Int32
	assetHits atomic.Int32
}

func newReleaseAPI(t *testing.T, tag string) *releaseAPI {
	t.Helper()
	payload := []byte("fake appimage " + tag)
	sum := sha256.Sum256(payload)
	api := &releaseAPI{}
	api.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/releases/latest":
			api.apiHits.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": tag,
				"assets": []map[string]any{{
					"name":                 "streamlink-" + tag + "-cp315-cp315-manylinux_2_28_x86_64.AppImage",
					"browser_download_url": api.URL + "/asset",
					"size":                 len(payload),
					"digest":               "sha256:" + hex.EncodeToString(sum[:]),
				}},
			})
		case "/asset":
			api.assetHits.Add(1)
			_, _ = w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(api.Close)
	return api
}

func (api *releaseAPI) downloader(t *testing.T, dataDir string) *components.Downloader {
	t.Helper()
	u, err := url.Parse(api.URL)
	if err != nil {
		t.Fatal(err)
	}
	return &components.Downloader{
		Transport:    api.Client().Transport,
		AllowedHosts: []string{u.Host},
		APIBaseURL:   api.URL + "/api/releases/latest",
		DataDir:      dataDir,
		GOOS:         "linux",
		GOARCH:       "amd64",
		InsecureHTTP: true,
		ExtractAppImage: func(_ context.Context, _, destDir string) error {
			dir := filepath.Join(destDir, "squashfs-root")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(dir, "AppRun"), []byte("#!/bin/sh\n"), 0o755)
		},
	}
}

func newManagedBridge(t *testing.T, cfgStreamlink, dataDir string, down *components.Downloader) *playbackBridge {
	t.Helper()
	mgr := player.New(player.Options{})
	t.Cleanup(mgr.Close)
	b := newPlaybackBridge(mgr, cfgStreamlink, dataDir)
	b.down = down
	t.Cleanup(b.close)
	return b
}

func TestCheckStreamlinkUpdateInstallsNewerManagedVersion(t *testing.T) {
	skipOnDarwin(t)
	dataDir := t.TempDir()
	installManagedVersion(t, dataDir, "8.6.3-1")
	t.Setenv("PATH", t.TempDir())
	api := newReleaseAPI(t, "8.7.0")
	b := newManagedBridge(t, "", dataDir, api.downloader(t, dataDir))

	res, err := b.checkStreamlinkUpdate(context.Background())
	if err != nil || res.Status != components.UpdateInstalled {
		t.Fatalf("checkStreamlinkUpdate = %+v, %v; want installed", res, err)
	}
	current, previous := linuxResolver(dataDir).ManagedVersions()
	if current != "8.7.0" || previous != "8.6.3-1" {
		t.Fatalf("versions = %q, %q", current, previous)
	}

	// Same day: no second check.
	res, err = b.checkStreamlinkUpdate(context.Background())
	if err != nil || res.Status != components.UpdateNotDue {
		t.Fatalf("second check = %+v, %v; want not due", res, err)
	}
	if hits := api.apiHits.Load(); hits != 1 {
		t.Fatalf("api hits = %d, want 1", hits)
	}
}

func TestCheckStreamlinkUpdateLeavesUnmanagedStreamlinkAlone(t *testing.T) {
	t.Run("path from the config", func(t *testing.T) {
		dataDir := t.TempDir()
		installManagedVersion(t, dataDir, "8.6.3-1")
		own := filepath.Join(t.TempDir(), "my-streamlink")
		if err := os.WriteFile(own, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		api := newReleaseAPI(t, "8.7.0")
		b := newManagedBridge(t, own, dataDir, api.downloader(t, dataDir))

		res, err := b.checkStreamlinkUpdate(context.Background())
		if err != nil || res.Status != "" {
			t.Fatalf("result = %+v, %v; a configured streamlink is never updated", res, err)
		}
		if api.apiHits.Load() != 0 {
			t.Fatal("network used for a streamlink that is not managed")
		}
	})

	t.Run("streamlink on PATH", func(t *testing.T) {
		dataDir := t.TempDir()
		pathDir := t.TempDir()
		for _, name := range []string{"streamlink", "streamlink.exe"} {
			if err := os.WriteFile(filepath.Join(pathDir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		t.Setenv("PATH", pathDir)
		api := newReleaseAPI(t, "8.7.0")
		b := newManagedBridge(t, "", dataDir, api.downloader(t, dataDir))

		res, err := b.checkStreamlinkUpdate(context.Background())
		if err != nil || res.Status != "" {
			t.Fatalf("result = %+v, %v; a PATH streamlink is never updated", res, err)
		}
		if api.apiHits.Load() != 0 {
			t.Fatal("network used for a streamlink that is not managed")
		}
	})

	t.Run("custom player command", func(t *testing.T) {
		dataDir := t.TempDir()
		installManagedVersion(t, dataDir, "8.6.3-1")
		api := newReleaseAPI(t, "8.7.0")
		mgr := player.New(player.Options{Command: func(player.Target, string) *exec.Cmd { return nil }})
		t.Cleanup(mgr.Close)
		b := newPlaybackBridge(mgr, "", dataDir)
		b.down = api.downloader(t, dataDir)
		b.startUpdateCheck()
		b.bg.Wait()
		if api.apiHits.Load() != 0 {
			t.Fatal("update check ran although the player does not use streamlink")
		}
	})
}

// onTrialBridge is a bridge using version 8.7.0, an update on trial, with
// 8.6.3-1 kept as the previous version.
func onTrialBridge(t *testing.T) (*playbackBridge, string) {
	t.Helper()
	skipOnDarwin(t)
	dataDir := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	installManagedVersion(t, dataDir, "8.6.3-1")
	api := newReleaseAPI(t, "8.7.0")
	b := newManagedBridge(t, "", dataDir, api.downloader(t, dataDir))
	if res, err := b.checkStreamlinkUpdate(context.Background()); err != nil || res.Status != components.UpdateInstalled {
		t.Fatalf("update = %+v, %v", res, err)
	}
	if err := b.ensureStreamlink(context.Background()); err != nil {
		t.Fatalf("ensureStreamlink: %v", err)
	}
	if got := b.resolved.Version; got != "8.7.0" {
		t.Skipf("the platform's resolver does not see the fake install (version %q)", got)
	}
	return b, dataDir
}

func TestObservePlayerRollsBackOnlyForStreamlinkFailure(t *testing.T) {
	failures := []player.Failure{
		player.FailureNone,
		player.FailureOffline,
		player.FailureNetwork,
		player.FailureTimeout,
		player.FailureStreamlinkMissing,
		player.FailureMPVMissing,
		player.FailureNoPlugin,
		player.FailureUnknown,
		player.FailureStreamlinkBroken,
	}
	for _, f := range failures {
		name := string(f)
		if name == "" {
			name = "none"
		}
		t.Run(name, func(t *testing.T) {
			b, dataDir := onTrialBridge(t)
			newPath := b.mgr.StreamlinkPath()

			b.observePlayer(player.Event{State: player.StateError, Failure: f})
			b.bg.Wait()

			st := components.LoadUpdateState(dataDir)
			rolledBack := f == player.FailureStreamlinkBroken
			if got := st.IsBad("8.7.0"); got != rolledBack {
				t.Fatalf("8.7.0 marked bad = %v, want %v (state %+v)", got, rolledBack, st)
			}
			if got := b.mgr.StreamlinkPath() != newPath; got != rolledBack {
				t.Fatalf("player switched streamlink = %v, want %v", got, rolledBack)
			}
			if got := len(b.Rollbacks()) == 1; got != rolledBack {
				t.Fatalf("rollbacks = %v, want recorded = %v", b.Rollbacks(), rolledBack)
			}
			if got := managedDirExists(dataDir, "8.7.0"); got == rolledBack {
				t.Fatalf("8.7.0 files present = %v, want %v", got, !rolledBack)
			}
			if !managedDirExists(dataDir, "8.6.3-1") {
				t.Fatal("the previous version must always stay")
			}
			if rolledBack {
				if cur, _ := linuxResolver(dataDir).ManagedVersions(); cur != "8.6.3-1" {
					t.Fatalf("version in use after rollback = %q, want 8.6.3-1", cur)
				}
			}
		})
	}
}

func TestRolledBackVersionIsNotDownloadedAgain(t *testing.T) {
	b, dataDir := onTrialBridge(t)
	api := newReleaseAPI(t, "8.7.0")
	b.down = api.downloader(t, dataDir)
	b.observePlayer(player.Event{State: player.StateError, Failure: player.FailureStreamlinkBroken})
	b.bg.Wait()

	// The next day's check sees the same, bad, release and downloads nothing.
	b.down.Now = func() time.Time { return time.Now().Add(48 * time.Hour) }
	res, err := b.checkStreamlinkUpdate(context.Background())
	if err != nil || res.Status != components.UpdateCurrent {
		t.Fatalf("check after rollback = %+v, %v; want current", res, err)
	}
	if api.assetHits.Load() != 0 {
		t.Fatal("the version marked bad was downloaded again")
	}
}

func TestObservePlayerFirstPlaybackEndsTrial(t *testing.T) {
	b, dataDir := onTrialBridge(t)

	b.observePlayer(player.Event{State: player.StatePlaying})
	b.bg.Wait()
	if got := components.LoadUpdateState(dataDir).Trial; got != "" {
		t.Fatalf("trial after a successful playback = %q, want none", got)
	}

	// A version that already played is not an update's fault any more.
	b.observePlayer(player.Event{State: player.StateError, Failure: player.FailureStreamlinkBroken})
	b.bg.Wait()
	if components.LoadUpdateState(dataDir).IsBad("8.7.0") || len(b.Rollbacks()) != 0 {
		t.Fatal("a version that played was rolled back")
	}
}

func TestObservePlayerIgnoresUnmanagedStreamlink(t *testing.T) {
	dataDir := t.TempDir()
	own := filepath.Join(t.TempDir(), "my-streamlink")
	if err := os.WriteFile(own, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	b := newManagedBridge(t, own, dataDir, nil)
	if err := b.ensureStreamlink(context.Background()); err != nil {
		t.Fatalf("ensureStreamlink: %v", err)
	}

	b.observePlayer(player.Event{State: player.StateError, Failure: player.FailureStreamlinkBroken})
	b.observePlayer(player.Event{State: player.StatePlaying})
	b.bg.Wait()
	if _, err := os.Stat(filepath.Join(components.StreamlinkBaseDir(dataDir), "state.json")); !os.IsNotExist(err) {
		t.Fatalf("a configured streamlink must leave no update bookkeeping (stat err = %v)", err)
	}
	if len(b.Rollbacks()) != 0 {
		t.Fatal("rollback recorded for a streamlink the app does not manage")
	}
}

func TestLogStreamlinkRollbacks(t *testing.T) {
	for lang, want := range map[string]string{
		i18n.LangEN:   "ttv-tui: streamlink 8.7.0 failed after an update and will not be used again; went back to 8.6.3-1\n",
		i18n.LangPTBR: "ttv-tui: o streamlink 8.7.0 falhou depois de uma atualização e não será usado de novo; voltou para o 8.6.3-1\n",
	} {
		cat, err := i18n.New(lang)
		if err != nil {
			t.Fatal(err)
		}
		var log bytes.Buffer
		logStreamlinkRollbacks(&log, []streamlinkRollback{{From: "8.7.0", To: "8.6.3-1"}}, cat)
		if log.String() != want {
			t.Fatalf("%s: log = %q, want %q", lang, log.String(), want)
		}
	}
}
