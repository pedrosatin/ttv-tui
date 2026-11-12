package components

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/filelock"
)

func TestResolvePrecedence(t *testing.T) {
	dataDir := t.TempDir()
	configDir := t.TempDir()
	pathDir := t.TempDir()

	// 1. Prepare fake config binary
	cfgBin := filepath.Join(configDir, "streamlink-cfg")
	if err := os.WriteFile(cfgBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	// 2. Prepare fake managed binary in dataDir
	managedDir := filepath.Join(dataDir, "components", "streamlink", "8.6.3-1")
	if err := os.MkdirAll(managedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	managedBin := filepath.Join(managedDir, "squashfs-root", "AppRun")
	if runtime.GOOS == "windows" {
		managedBin = filepath.Join(managedDir, "bin", "streamlink.exe")
	}
	if err := os.MkdirAll(filepath.Dir(managedBin), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managedBin, []byte("fake-streamlink"), 0o755); err != nil {
		t.Fatal(err)
	}

	// 3. Prepare fake PATH binary
	pathBin := filepath.Join(pathDir, "streamlink")
	if runtime.GOOS == "windows" {
		pathBin += ".exe"
	}
	if err := os.WriteFile(pathBin, []byte("fake-path"), 0o755); err != nil {
		t.Fatal(err)
	}
	fakeLookPath := func(file string) (string, error) {
		if file == "streamlink" {
			return pathBin, nil
		}
		return "", errors.New("not found")
	}

	r := &Resolver{
		DataDir:  dataDir,
		GOOS:     runtime.GOOS,
		GOARCH:   runtime.GOARCH,
		LookPath: fakeLookPath,
		HasFUSE:  func() bool { return true },
	}

	// Case A: Config path takes absolute precedence
	res, err := r.ResolveStreamlink(cfgBin)
	if err != nil {
		t.Fatalf("resolve config: %v", err)
	}
	if res.Origin != OriginConfig || res.Path != cfgBin {
		t.Fatalf("expected OriginConfig with %s, got %+v", cfgBin, res)
	}

	// Case B: Non-existent config path returns error
	_, err = r.ResolveStreamlink(filepath.Join(configDir, "non-existent"))
	if err == nil {
		t.Fatal("expected error for non-existent config path")
	}

	// Case C: Without config path, managed streamlink takes precedence over PATH
	if runtime.GOOS != "darwin" {
		res, err = r.ResolveStreamlink("")
		if err != nil {
			t.Fatalf("resolve managed: %v", err)
		}
		if res.Origin != OriginManaged || res.Path != managedBin {
			t.Fatalf("expected OriginManaged with %s, got %+v", managedBin, res)
		}
	}

	// Case D: Without managed streamlink, PATH is used
	rEmptyManaged := &Resolver{
		DataDir:  t.TempDir(),
		GOOS:     runtime.GOOS,
		GOARCH:   runtime.GOARCH,
		LookPath: fakeLookPath,
		HasFUSE:  func() bool { return true },
	}
	res, err = rEmptyManaged.ResolveStreamlink("")
	if err != nil {
		t.Fatalf("resolve PATH: %v", err)
	}
	if res.Origin != OriginPath || res.Path != pathBin {
		t.Fatalf("expected OriginPath with %s, got %+v", pathBin, res)
	}

	// Case E: Nothing present -> ErrNotFound
	rNone := &Resolver{
		DataDir:  t.TempDir(),
		GOOS:     runtime.GOOS,
		GOARCH:   runtime.GOARCH,
		LookPath: func(string) (string, error) { return "", errors.New("not found") },
		HasFUSE:  func() bool { return true },
	}
	_, err = rNone.ResolveStreamlink("")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestResolveStreamlink_ErrDotNotSwallowed(t *testing.T) {
	r := &Resolver{
		DataDir: t.TempDir(),
		LookPath: func(string) (string, error) {
			return "", exec.ErrDot
		},
	}
	_, err := r.ResolveStreamlink("")
	if err == nil {
		t.Fatal("expected error when LookPath returns ErrDot")
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatal("exec.ErrDot must not be converted to ErrNotFound")
	}
	if !errors.Is(err, exec.ErrDot) {
		t.Fatalf("expected error wrapping exec.ErrDot, got: %v", err)
	}
}

func TestVersionSortingNumeric(t *testing.T) {
	dataDir := t.TempDir()
	baseDir := StreamlinkBaseDir(dataDir)

	// Create directories: 8.9.0 and 8.10.0
	for _, v := range []string{"8.9.0", "8.10.0", "8.6.3-1", "8.6.3"} {
		vDir := filepath.Join(baseDir, v, "squashfs-root")
		if err := os.MkdirAll(vDir, 0o700); err != nil {
			t.Fatal(err)
		}
		appRun := filepath.Join(vDir, "AppRun")
		if err := os.WriteFile(appRun, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	r := &Resolver{
		DataDir: dataDir,
		GOOS:    "linux",
		GOARCH:  "amd64",
	}

	res, ok := r.findManagedStreamlink()
	if !ok {
		t.Fatal("expected to find managed streamlink")
	}
	// 8.10.0 must be selected over 8.9.0 numerically
	if res.Version != "8.10.0" {
		t.Fatalf("selected version = %q, want %q (numeric sort failed)", res.Version, "8.10.0")
	}
}

func TestDownloadAndVerifySuccess_LinuxExtracted(t *testing.T) {
	payload := []byte("fake appimage payload")
	hasher := sha256.New()
	hasher.Write(payload)
	expectedHash := hex.EncodeToString(hasher.Sum(nil))

	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/releases/latest":
			rel := gitHubRelease{
				TagName: "8.6.3-1",
				Assets: []gitHubAsset{
					{
						Name:               "streamlink-8.6.3-1-cp315-cp315-manylinux_2_28_x86_64.AppImage",
						BrowserDownloadURL: ts.URL + "/assets/streamlink.AppImage",
						Size:               int64(len(payload)),
						Digest:             "sha256:" + expectedHash,
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(rel)
		case "/assets/streamlink.AppImage":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	dataDir := t.TempDir()

	var extractedAppImagePath string
	var extractedDestDir string
	mockExtract := func(ctx context.Context, appImagePath, destDir string) error {
		extractedAppImagePath = appImagePath
		extractedDestDir = destDir
		appRunDir := filepath.Join(destDir, "squashfs-root")
		if err := os.MkdirAll(appRunDir, 0o700); err != nil {
			return err
		}
		appRun := filepath.Join(appRunDir, "AppRun")
		return os.WriteFile(appRun, []byte("#!/bin/sh\necho streamlink\n"), 0o755)
	}

	d := &Downloader{
		Transport:       ts.Client().Transport,
		AllowedHosts:    []string{u.Host},
		APIBaseURL:      ts.URL + "/api/releases/latest",
		DataDir:         dataDir,
		GOOS:            "linux",
		GOARCH:          "amd64",
		InsecureHTTP:    true,
		ExtractAppImage: mockExtract,
	}

	res, err := d.DownloadStreamlink(context.Background(), nil)
	if err != nil {
		t.Fatalf("DownloadStreamlink: %v", err)
	}

	if res.Origin != OriginManaged {
		t.Fatalf("origin = %s, want %s", res.Origin, OriginManaged)
	}
	if res.Version != "8.6.3-1" {
		t.Fatalf("version = %s, want 8.6.3-1", res.Version)
	}
	if !strings.HasSuffix(filepath.ToSlash(res.Path), "squashfs-root/AppRun") {
		t.Fatalf("expected binary at squashfs-root/AppRun, got: %s", res.Path)
	}
	if len(res.Env) != 0 {
		t.Fatalf("expected no extra env for extracted AppRun (no FUSE requirement), got: %v", res.Env)
	}
	if extractedAppImagePath == "" || extractedDestDir == "" {
		t.Fatal("expected mockExtract to be called")
	}
}

func TestAssetRedirectDisallowedDomainRejected(t *testing.T) {
	// Tests redirect refusal passing through production httpClient CheckRedirect
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/releases/latest":
			rel := gitHubRelease{
				TagName: "8.6.3-1",
				Assets: []gitHubAsset{
					{
						Name:               "streamlink-8.6.3-1-py315-x86_64.zip",
						BrowserDownloadURL: ts.URL + "/redirect-disallowed",
						Size:               100,
						Digest:             "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(rel)
		case "/redirect-disallowed":
			http.Redirect(w, r, "https://disallowed.attacker.com/asset.zip", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	dataDir := t.TempDir()

	d := &Downloader{
		Transport:    ts.Client().Transport,
		AllowedHosts: []string{u.Host},
		APIBaseURL:   ts.URL + "/api/releases/latest",
		DataDir:      dataDir,
		GOOS:         "windows",
		GOARCH:       "amd64",
		InsecureHTTP: true,
	}

	_, err := d.DownloadStreamlink(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error on redirect to disallowed domain")
	}
	if !strings.Contains(err.Error(), "unapproved download host") {
		t.Fatalf("expected unapproved download host error, got: %v", err)
	}
}

func TestInsecureHTTPSchemeRejected(t *testing.T) {
	// InsecureHTTP: false (default). Initial http:// URL must be rejected.
	d := &Downloader{
		AllowedHosts: []string{"example.com"},
		APIBaseURL:   "http://example.com/releases/latest",
		DataDir:      t.TempDir(),
		GOOS:         "windows",
		GOARCH:       "amd64",
		InsecureHTTP: false,
	}
	_, err := d.DownloadStreamlink(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for initial http:// URL when InsecureHTTP is false")
	}
	if !strings.Contains(err.Error(), "only HTTPS is allowed") {
		t.Fatalf("expected HTTPS error, got: %v", err)
	}

	// Redirect to http:// scheme must also be rejected by CheckRedirect
	var ts *httptest.Server
	ts = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/releases/latest":
			rel := gitHubRelease{
				TagName: "8.6.3-1",
				Assets: []gitHubAsset{
					{
						Name:               "streamlink-8.6.3-1-py315-x86_64.zip",
						BrowserDownloadURL: ts.URL + "/redirect-http",
						Size:               100,
						Digest:             "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(rel)
		case "/redirect-http":
			http.Redirect(w, r, "http://example.com/asset.zip", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	dHTTPS := &Downloader{
		Transport:    ts.Client().Transport,
		AllowedHosts: []string{u.Host, "example.com"},
		APIBaseURL:   ts.URL + "/api/releases/latest",
		DataDir:      t.TempDir(),
		GOOS:         "windows",
		GOARCH:       "amd64",
		InsecureHTTP: false,
	}
	_, err = dHTTPS.DownloadStreamlink(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error on redirect to insecure http:// URL")
	}
	if !strings.Contains(err.Error(), "only HTTPS is allowed") {
		t.Fatalf("expected HTTPS error on redirect, got: %v", err)
	}
}

func TestWindowsZipSlipRejected(t *testing.T) {
	destDir := t.TempDir()

	// 1. Entry with ../ traversal
	bufDotDot := new(bytes.Buffer)
	zw := zip.NewWriter(bufDotDot)
	w, err := zw.Create("../../evil.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("evil"))
	_ = zw.Close()

	zipFileDotDot := filepath.Join(t.TempDir(), "slip.zip")
	if err := os.WriteFile(zipFileDotDot, bufDotDot.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = installWindowsZip(zipFileDotDot, destDir)
	if err == nil {
		t.Fatal("expected error on zip slip ../ entry")
	}

	// 2. Entry with symlink mode
	bufSymlink := new(bytes.Buffer)
	zwSym := zip.NewWriter(bufSymlink)
	h := &zip.FileHeader{
		Name: "symlink-entry",
	}
	h.SetMode(os.ModeSymlink | 0o777)
	w, err = zwSym.CreateHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("target"))
	_ = zwSym.Close()

	zipFileSymlink := filepath.Join(t.TempDir(), "symlink.zip")
	if err := os.WriteFile(zipFileSymlink, bufSymlink.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = installWindowsZip(zipFileSymlink, destDir)
	if err == nil {
		t.Fatal("expected error on symlink entry in zip")
	}
	if !strings.Contains(err.Error(), "illegal entry in zip archive") {
		t.Fatalf("expected illegal entry error for symlink, got: %v", err)
	}
}

func TestDownloadDigestMismatchDeletesTemp(t *testing.T) {
	payload := []byte("corrupted payload")
	wrongDigest := "sha256:0000000000000000000000000000000000000000000000000000000000000000"

	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/releases/latest":
			rel := gitHubRelease{
				TagName: "8.6.3-1",
				Assets: []gitHubAsset{
					{
						Name:               "streamlink-8.6.3-1-cp315-cp315-manylinux_2_28_x86_64.AppImage",
						BrowserDownloadURL: ts.URL + "/assets/streamlink.AppImage",
						Size:               int64(len(payload)),
						Digest:             wrongDigest,
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(rel)
		case "/assets/streamlink.AppImage":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	dataDir := t.TempDir()

	d := &Downloader{
		Transport:    ts.Client().Transport,
		AllowedHosts: []string{u.Host},
		APIBaseURL:   ts.URL + "/api/releases/latest",
		DataDir:      dataDir,
		GOOS:         "linux",
		GOARCH:       "amd64",
		InsecureHTTP: true,
	}

	_, err := d.DownloadStreamlink(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error on digest mismatch")
	}
	if !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("error missing digest mismatch description: %v", err)
	}

	baseDir := StreamlinkBaseDir(dataDir)
	entries, _ := os.ReadDir(baseDir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".download-") || strings.HasPrefix(e.Name(), ".install-") {
			t.Fatalf("temporary dir was not cleaned up: %s", e.Name())
		}
	}
	versionDir := StreamlinkVersionDir(dataDir, "8.6.3-1")
	if _, err := os.Stat(versionDir); !os.IsNotExist(err) {
		t.Fatalf("version directory exists after failed download: %v", err)
	}
}

func TestDownloadInterruptedRecovery(t *testing.T) {
	fullPayload := []byte("full valid streamlink executable payload")
	hasher := sha256.New()
	hasher.Write(fullPayload)
	expectedHash := hex.EncodeToString(hasher.Sum(nil))

	var attempt atomic.Int32
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/releases/latest":
			rel := gitHubRelease{
				TagName: "8.6.3-1",
				Assets: []gitHubAsset{
					{
						Name:               "streamlink-8.6.3-1-cp315-cp315-manylinux_2_28_x86_64.AppImage",
						BrowserDownloadURL: ts.URL + "/assets/streamlink.AppImage",
						Size:               int64(len(fullPayload)),
						Digest:             "sha256:" + expectedHash,
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(rel)
		case "/assets/streamlink.AppImage":
			if attempt.Add(1) == 1 {
				// First attempt: truncate payload abruptly
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("short"))
				return
			}
			// Second attempt: full payload
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(fullPayload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	dataDir := t.TempDir()

	mockExtract := func(ctx context.Context, appImagePath, destDir string) error {
		appRunDir := filepath.Join(destDir, "squashfs-root")
		_ = os.MkdirAll(appRunDir, 0o700)
		return os.WriteFile(filepath.Join(appRunDir, "AppRun"), []byte("#!/bin/sh\n"), 0o755)
	}

	d := &Downloader{
		Transport:       ts.Client().Transport,
		AllowedHosts:    []string{u.Host},
		APIBaseURL:      ts.URL + "/api/releases/latest",
		DataDir:         dataDir,
		GOOS:            "linux",
		GOARCH:          "amd64",
		InsecureHTTP:    true,
		ExtractAppImage: mockExtract,
	}

	// 1. First attempt fails due to truncated download
	_, err := d.DownloadStreamlink(context.Background(), nil)
	if err == nil {
		t.Fatal("expected first attempt to fail due to truncation")
	}

	// Verify nothing installed
	versionDir := StreamlinkVersionDir(dataDir, "8.6.3-1")
	if _, err := os.Stat(versionDir); !os.IsNotExist(err) {
		t.Fatalf("target dir exists after truncated download: %v", err)
	}

	// 2. Second attempt succeeds and installs properly
	res, err := d.DownloadStreamlink(context.Background(), nil)
	if err != nil {
		t.Fatalf("expected second attempt to succeed, got: %v", err)
	}
	if res.Version != "8.6.3-1" {
		t.Fatalf("expected version 8.6.3-1, got %s", res.Version)
	}
	if _, err := os.Stat(res.Path); err != nil {
		t.Fatalf("binary not found after recovery: %v", err)
	}
}

func TestMatchAssetByArchitecture(t *testing.T) {
	assets := []gitHubAsset{
		{Name: "streamlink+ffmpeg-8.6.3-1-cp315-cp315-manylinux_2_28_x86_64.AppImage"},
		{Name: "streamlink+ffmpeg-8.6.3-1-cp315-cp315-manylinux_2_28_aarch64.AppImage"},
		{Name: "streamlink-8.6.3-1-cp315-cp315-manylinux_2_28_x86_64.AppImage"},
		{Name: "streamlink-8.6.3-1-cp315-cp315-manylinux_2_28_aarch64.AppImage"},
		{Name: "streamlink-8.6.3-1-py315-x86_64.zip"},
		{Name: "streamlink-8.6.3-1-cp315-cp315-manylinux_2_28_x86_64.AppImage.zsync"},
	}

	// Linux amd64: must pick streamlink- asset over streamlink+ffmpeg-
	dLinuxAmd64 := &Downloader{GOOS: "linux", GOARCH: "amd64"}
	a, err := dLinuxAmd64.matchAsset(assets)
	if err != nil {
		t.Fatalf("Linux amd64 match: %v", err)
	}
	if a.Name != "streamlink-8.6.3-1-cp315-cp315-manylinux_2_28_x86_64.AppImage" {
		t.Fatalf("Linux amd64 must pick streamlink- prefix, got: %s", a.Name)
	}

	// Linux arm64: must pick streamlink- asset over streamlink+ffmpeg-
	dLinuxArm64 := &Downloader{GOOS: "linux", GOARCH: "arm64"}
	a, err = dLinuxArm64.matchAsset(assets)
	if err != nil {
		t.Fatalf("Linux arm64 match: %v", err)
	}
	if a.Name != "streamlink-8.6.3-1-cp315-cp315-manylinux_2_28_aarch64.AppImage" {
		t.Fatalf("Linux arm64 must pick streamlink- prefix, got: %s", a.Name)
	}

	// Windows amd64
	dWindows := &Downloader{GOOS: "windows", GOARCH: "amd64"}
	a, err = dWindows.matchAsset(assets)
	if err != nil || a.Name != "streamlink-8.6.3-1-py315-x86_64.zip" {
		t.Fatalf("Windows match: %v, asset: %+v", err, a)
	}
}

func TestConcurrentDownloadsSingleflight(t *testing.T) {
	payload := []byte("concurrent download test payload")
	hasher := sha256.New()
	hasher.Write(payload)
	expectedHash := hex.EncodeToString(hasher.Sum(nil))

	var assetRequests atomic.Int32
	var lockAcquisitions atomic.Int32
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/releases/latest":
			rel := gitHubRelease{
				TagName: "8.6.3-1",
				Assets: []gitHubAsset{
					{
						Name:               "streamlink-8.6.3-1-cp315-cp315-manylinux_2_28_x86_64.AppImage",
						BrowserDownloadURL: ts.URL + "/assets/streamlink.AppImage",
						Size:               int64(len(payload)),
						Digest:             "sha256:" + expectedHash,
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(rel)
		case "/assets/streamlink.AppImage":
			assetRequests.Add(1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	dataDir := t.TempDir()

	mockExtract := func(ctx context.Context, appImagePath, destDir string) error {
		appRunDir := filepath.Join(destDir, "squashfs-root")
		_ = os.MkdirAll(appRunDir, 0o700)
		return os.WriteFile(filepath.Join(appRunDir, "AppRun"), []byte("#!/bin/sh\n"), 0o755)
	}

	d := &Downloader{
		Transport:       ts.Client().Transport,
		AllowedHosts:    []string{u.Host},
		APIBaseURL:      ts.URL + "/api/releases/latest",
		DataDir:         dataDir,
		GOOS:            "linux",
		GOARCH:          "amd64",
		InsecureHTTP:    true,
		ExtractAppImage: mockExtract,
		OnLockAcquired: func() {
			lockAcquisitions.Add(1)
		},
	}

	var wg sync.WaitGroup
	var res1, res2 Result
	var err1, err2 error

	wg.Add(2)
	go func() {
		defer wg.Done()
		res1, err1 = d.DownloadStreamlink(context.Background(), nil)
	}()
	go func() {
		defer wg.Done()
		res2, err2 = d.DownloadStreamlink(context.Background(), nil)
	}()
	wg.Wait()

	if err1 != nil {
		t.Fatalf("goroutine 1 error: %v", err1)
	}
	if err2 != nil {
		t.Fatalf("goroutine 2 error: %v", err2)
	}
	if res1.Path != res2.Path {
		t.Fatalf("paths differ: %s vs %s", res1.Path, res2.Path)
	}
	if reqCount := assetRequests.Load(); reqCount != 1 {
		t.Fatalf("expected exactly 1 asset request due to singleflight, got: %d", reqCount)
	}
	if acquired := lockAcquisitions.Load(); acquired != 1 {
		t.Fatalf("expected exactly 1 lock acquisition due to singleflight, got: %d", acquired)
	}
}

func TestWindowsZipExtraction(t *testing.T) {
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)

	binHeader := &zip.FileHeader{
		Name:   "streamlink-8.6.3-1-py315-x86_64/bin/streamlink.exe",
		Method: zip.Deflate,
	}
	binHeader.SetMode(0o755)
	w, err := zw.CreateHeader(binHeader)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("fake streamlink.exe binary"))

	pyHeader := &zip.FileHeader{
		Name:   "streamlink-8.6.3-1-py315-x86_64/Python/python.exe",
		Method: zip.Deflate,
	}
	w, err = zw.CreateHeader(pyHeader)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("fake python"))

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zipBytes := buf.Bytes()

	hasher := sha256.New()
	hasher.Write(zipBytes)
	zipHash := hex.EncodeToString(hasher.Sum(nil))

	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/releases/latest":
			rel := gitHubRelease{
				TagName: "8.6.3-1",
				Assets: []gitHubAsset{
					{
						Name:               "streamlink-8.6.3-1-py315-x86_64.zip",
						BrowserDownloadURL: ts.URL + "/assets/streamlink.zip",
						Size:               int64(len(zipBytes)),
						Digest:             "sha256:" + zipHash,
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(rel)
		case "/assets/streamlink.zip":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(zipBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	dataDir := t.TempDir()

	d := &Downloader{
		Transport:    ts.Client().Transport,
		AllowedHosts: []string{u.Host},
		APIBaseURL:   ts.URL + "/api/releases/latest",
		DataDir:      dataDir,
		GOOS:         "windows",
		GOARCH:       "amd64",
		InsecureHTTP: true,
	}

	res, err := d.DownloadStreamlink(context.Background(), nil)
	if err != nil {
		t.Fatalf("DownloadStreamlink Windows: %v", err)
	}
	if !strings.HasSuffix(filepath.ToSlash(res.Path), "bin/streamlink.exe") {
		t.Fatalf("expected binary at bin/streamlink.exe, got: %s", res.Path)
	}
	if _, err := os.Stat(res.Path); err != nil {
		t.Fatalf("stat extracted Windows binary: %v", err)
	}
}

func TestTargetDirExisting_CompletePreserved(t *testing.T) {
	dataDir := t.TempDir()
	vDir := StreamlinkVersionDir(dataDir, "8.6.3-1")
	existingAppRunDir := filepath.Join(vDir, "squashfs-root")
	if err := os.MkdirAll(existingAppRunDir, 0o700); err != nil {
		t.Fatal(err)
	}
	existingBin := filepath.Join(existingAppRunDir, "AppRun")
	if err := os.WriteFile(existingBin, []byte("#!/bin/sh\necho existing\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	payload := []byte("new payload")
	hasher := sha256.New()
	hasher.Write(payload)
	expectedHash := hex.EncodeToString(hasher.Sum(nil))

	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/releases/latest":
			rel := gitHubRelease{
				TagName: "8.6.3-1",
				Assets: []gitHubAsset{
					{
						Name:               "streamlink-8.6.3-1-cp315-cp315-manylinux_2_28_x86_64.AppImage",
						BrowserDownloadURL: ts.URL + "/assets/streamlink.AppImage",
						Size:               int64(len(payload)),
						Digest:             "sha256:" + expectedHash,
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(rel)
		case "/assets/streamlink.AppImage":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	extractCalled := false
	mockExtract := func(ctx context.Context, appImagePath, destDir string) error {
		extractCalled = true
		appRunDir := filepath.Join(destDir, "squashfs-root")
		_ = os.MkdirAll(appRunDir, 0o700)
		return os.WriteFile(filepath.Join(appRunDir, "AppRun"), []byte("#!/bin/sh\necho new\n"), 0o755)
	}

	d := &Downloader{
		Transport:       ts.Client().Transport,
		AllowedHosts:    []string{u.Host},
		APIBaseURL:      ts.URL + "/api/releases/latest",
		DataDir:         dataDir,
		GOOS:            "linux",
		GOARCH:          "amd64",
		InsecureHTTP:    true,
		ExtractAppImage: mockExtract,
	}

	res, err := d.DownloadStreamlink(context.Background(), nil)
	if err != nil {
		t.Fatalf("DownloadStreamlink: %v", err)
	}
	if res.Path != existingBin {
		t.Fatalf("expected existing binary %s, got %s", existingBin, res.Path)
	}
	// The binary content must still be the existing one
	content, _ := os.ReadFile(existingBin)
	if !strings.Contains(string(content), "existing") {
		t.Fatalf("expected existing binary content preserved, got: %s", string(content))
	}
	if extractCalled {
		t.Fatal("expected extract not to be called when targetDir is already complete")
	}
}

func TestTargetDirExisting_PartialRemovedAndReplaced(t *testing.T) {
	dataDir := t.TempDir()
	vDir := StreamlinkVersionDir(dataDir, "8.6.3-1")
	// Partial/broken target directory without valid binary
	if err := os.MkdirAll(filepath.Join(vDir, "leftover"), 0o700); err != nil {
		t.Fatal(err)
	}

	payload := []byte("new payload")
	hasher := sha256.New()
	hasher.Write(payload)
	expectedHash := hex.EncodeToString(hasher.Sum(nil))

	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/releases/latest":
			rel := gitHubRelease{
				TagName: "8.6.3-1",
				Assets: []gitHubAsset{
					{
						Name:               "streamlink-8.6.3-1-cp315-cp315-manylinux_2_28_x86_64.AppImage",
						BrowserDownloadURL: ts.URL + "/assets/streamlink.AppImage",
						Size:               int64(len(payload)),
						Digest:             "sha256:" + expectedHash,
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(rel)
		case "/assets/streamlink.AppImage":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	mockExtract := func(ctx context.Context, appImagePath, destDir string) error {
		appRunDir := filepath.Join(destDir, "squashfs-root")
		_ = os.MkdirAll(appRunDir, 0o700)
		return os.WriteFile(filepath.Join(appRunDir, "AppRun"), []byte("#!/bin/sh\necho installed\n"), 0o755)
	}

	d := &Downloader{
		Transport:       ts.Client().Transport,
		AllowedHosts:    []string{u.Host},
		APIBaseURL:      ts.URL + "/api/releases/latest",
		DataDir:         dataDir,
		GOOS:            "linux",
		GOARCH:          "amd64",
		InsecureHTTP:    true,
		ExtractAppImage: mockExtract,
	}

	res, err := d.DownloadStreamlink(context.Background(), nil)
	if err != nil {
		t.Fatalf("DownloadStreamlink: %v", err)
	}
	expectedBin := filepath.Join(vDir, "squashfs-root", "AppRun")
	if res.Path != expectedBin {
		t.Fatalf("expected binary at %s, got: %s", expectedBin, res.Path)
	}
	// Leftover dir should have been removed
	if _, err := os.Stat(filepath.Join(vDir, "leftover")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected leftover dir in partial targetDir to be removed, got: %v", err)
	}
}

func TestCleanStaleTemps(t *testing.T) {
	baseDir := t.TempDir()

	// Create temporary directories to clean
	for _, name := range []string{".download-123", ".install-456", ".tmp-789"} {
		p := filepath.Join(baseDir, name)
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(filepath.Join(p, "junk.tmp"), []byte("junk"), 0o600)
	}

	// Create a real version directory and .lock file that must be preserved
	vDir := filepath.Join(baseDir, "8.6.3-1")
	if err := os.MkdirAll(vDir, 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(vDir, "keep.txt"), []byte("keep"), 0o600)
	lockFile := filepath.Join(baseDir, ".lock")
	_ = os.WriteFile(lockFile, []byte("lock"), 0o600)

	cleanStaleTemps(baseDir)

	for _, name := range []string{".download-123", ".install-456", ".tmp-789"} {
		if _, err := os.Stat(filepath.Join(baseDir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("expected temp dir %s to be removed", name)
		}
	}
	if _, err := os.Stat(vDir); err != nil {
		t.Errorf("real version dir should be preserved: %v", err)
	}
	if _, err := os.Stat(lockFile); err != nil {
		t.Errorf("lock file should be preserved: %v", err)
	}
}

func TestDownloadSingleflight_WaitingCallerCancellation(t *testing.T) {
	payload := []byte("cancellation payload")
	hasher := sha256.New()
	hasher.Write(payload)
	expectedHash := hex.EncodeToString(hasher.Sum(nil))

	startedDownload := make(chan struct{})
	blockDownload := make(chan struct{})

	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/releases/latest":
			rel := gitHubRelease{
				TagName: "8.6.3-1",
				Assets: []gitHubAsset{
					{
						Name:               "streamlink-8.6.3-1-cp315-cp315-manylinux_2_28_x86_64.AppImage",
						BrowserDownloadURL: ts.URL + "/assets/streamlink.AppImage",
						Size:               int64(len(payload)),
						Digest:             "sha256:" + expectedHash,
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(rel)
		case "/assets/streamlink.AppImage":
			close(startedDownload)
			<-blockDownload
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	dataDir := t.TempDir()

	mockExtract := func(ctx context.Context, appImagePath, destDir string) error {
		appRunDir := filepath.Join(destDir, "squashfs-root")
		_ = os.MkdirAll(appRunDir, 0o700)
		return os.WriteFile(filepath.Join(appRunDir, "AppRun"), []byte("#!/bin/sh\n"), 0o755)
	}

	d := &Downloader{
		Transport:       ts.Client().Transport,
		AllowedHosts:    []string{u.Host},
		APIBaseURL:      ts.URL + "/api/releases/latest",
		DataDir:         dataDir,
		GOOS:            "linux",
		GOARCH:          "amd64",
		InsecureHTTP:    true,
		ExtractAppImage: mockExtract,
	}

	var wg sync.WaitGroup
	var res1 Result
	var err1 error

	// Goroutine 1 starts the download
	wg.Add(1)
	go func() {
		defer wg.Done()
		res1, err1 = d.DownloadStreamlink(context.Background(), nil)
	}()

	<-startedDownload

	// Goroutine 2 joins via singleflight with a context that gets canceled
	ctx2, cancel2 := context.WithCancel(context.Background())
	var err2 error
	waitingDone := make(chan struct{})
	go func() {
		_, err2 = d.DownloadStreamlink(ctx2, nil)
		close(waitingDone)
	}()

	// Cancel caller 2 while waiting
	cancel2()
	<-waitingDone

	if !errors.Is(err2, context.Canceled) {
		t.Fatalf("expected caller 2 to return context.Canceled, got: %v", err2)
	}

	// Unblock download for caller 1
	close(blockDownload)
	wg.Wait()

	if err1 != nil {
		t.Fatalf("caller 1 should succeed, got: %v", err1)
	}
	if res1.Path == "" {
		t.Fatal("caller 1 returned empty path")
	}
}

func TestDownloadLock_WaitingProcessCancellation(t *testing.T) {
	dataDir := t.TempDir()
	baseDir := StreamlinkBaseDir(dataDir)
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// Simulate an active download by acquiring the lock
	lockPath := filepath.Join(baseDir, ".lock")
	unlock, err := filelock.Lock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unlock() }()

	d := &Downloader{
		DataDir:      dataDir,
		GOOS:         "linux",
		GOARCH:       "amd64",
		LockStaleAge: 30 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = d.DownloadStreamlink(ctx, nil)
	if err == nil {
		t.Fatal("expected DownloadStreamlink to fail on canceled context waiting for lock")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context deadline error, got: %v", err)
	}
}

func TestInstallWindowsZip_DotDotCheck(t *testing.T) {
	// Case 1: valid filename with double dots like "a..b" is allowed
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	hdr := &zip.FileHeader{
		Name: "prefix/a..b/bin/streamlink.exe",
	}
	hdr.SetMode(0o755)
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("mock streamlink"))
	_ = zw.Close()

	destDir := t.TempDir()
	zipFile := filepath.Join(destDir, "test.zip")
	_ = os.WriteFile(zipFile, buf.Bytes(), 0o600)

	bin, err := installWindowsZip(zipFile, destDir)
	if err != nil {
		t.Fatalf("installWindowsZip should accept valid name with .. substring: %v", err)
	}
	if !strings.HasSuffix(filepath.ToSlash(bin), "bin/streamlink.exe") {
		t.Fatalf("expected binary path, got: %s", bin)
	}

	// Case 2: true relative path traversal with .. segment must fail
	for _, traversal := range []string{"../escape.exe", "foo/../../escape.exe"} {
		t.Run(traversal, func(t *testing.T) {
			b := new(bytes.Buffer)
			z := zip.NewWriter(b)
			h := &zip.FileHeader{Name: traversal}
			h.SetMode(0o755)
			w, err := z.CreateHeader(h)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte("malicious"))
			_ = z.Close()

			badZip := filepath.Join(t.TempDir(), "bad.zip")
			_ = os.WriteFile(badZip, b.Bytes(), 0o600)
			_, err = installWindowsZip(badZip, t.TempDir())
			if err == nil || !strings.Contains(err.Error(), "illegal relative path in zip") {
				t.Fatalf("expected illegal relative path error for %s, got: %v", traversal, err)
			}
		})
	}
}

func TestDefaultExtractAppImage_RelativePaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("AppImage extraction runs a shell script and only happens on Linux")
	}
	tempDir := t.TempDir()

	// Create a mock executable script acting as the AppImage
	mockScript := filepath.Join(tempDir, "mock-appimage.sh")
	scriptContent := "#!/bin/sh\nmkdir -p squashfs-root\ntouch squashfs-root/AppRun\nchmod +x squashfs-root/AppRun\n"
	if err := os.WriteFile(mockScript, []byte(scriptContent), 0o755); err != nil {
		t.Fatal(err)
	}

	destDir := filepath.Join(tempDir, "dest")
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// Pass relative paths by computing relative paths from current working directory
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relScript, err := filepath.Rel(cwd, mockScript)
	if err != nil {
		t.Fatal(err)
	}
	relDest, err := filepath.Rel(cwd, destDir)
	if err != nil {
		t.Fatal(err)
	}

	if err := defaultExtractAppImage(context.Background(), relScript, relDest); err != nil {
		t.Fatalf("defaultExtractAppImage with relative paths: %v", err)
	}

	appRun := filepath.Join(destDir, "squashfs-root", "AppRun")
	if _, err := os.Stat(appRun); err != nil {
		t.Fatalf("AppRun not found after extraction: %v", err)
	}
}
