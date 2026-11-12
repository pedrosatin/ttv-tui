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
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
	if err := os.MkdirAll(managedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	managedBin := filepath.Join(managedDir, "streamlink")
	if runtime.GOOS == "windows" {
		managedBin = filepath.Join(managedDir, "bin", "streamlink.exe")
		_ = os.MkdirAll(filepath.Dir(managedBin), 0o755)
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
		DataDir:  t.TempDir(), // empty data dir
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

func TestLinuxNoFUSESetsEnv(t *testing.T) {
	dataDir := t.TempDir()
	managedDir := filepath.Join(dataDir, "components", "streamlink", "8.6.3-1")
	if err := os.MkdirAll(managedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	binPath := filepath.Join(managedDir, "streamlink")
	if err := os.WriteFile(binPath, []byte("appimage-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	// When HasFUSE returns false, APPIMAGE_EXTRACT_AND_RUN=1 must be added
	rNoFuse := &Resolver{
		DataDir:  dataDir,
		GOOS:     "linux",
		GOARCH:   "amd64",
		LookPath: func(string) (string, error) { return "", errors.New("not found") },
		HasFUSE:  func() bool { return false },
	}
	res, err := rNoFuse.ResolveStreamlink("")
	if err != nil {
		t.Fatal(err)
	}
	hasExtractEnv := false
	for _, env := range res.Env {
		if env == "APPIMAGE_EXTRACT_AND_RUN=1" {
			hasExtractEnv = true
		}
	}
	if !hasExtractEnv {
		t.Fatalf("expected APPIMAGE_EXTRACT_AND_RUN=1 in res.Env when FUSE is absent, got %v", res.Env)
	}

	// When HasFUSE returns true, APPIMAGE_EXTRACT_AND_RUN=1 must NOT be added
	rFuse := &Resolver{
		DataDir:  dataDir,
		GOOS:     "linux",
		GOARCH:   "amd64",
		LookPath: func(string) (string, error) { return "", errors.New("not found") },
		HasFUSE:  func() bool { return true },
	}
	res, err = rFuse.ResolveStreamlink("")
	if err != nil {
		t.Fatal(err)
	}
	for _, env := range res.Env {
		if env == "APPIMAGE_EXTRACT_AND_RUN=1" {
			t.Fatalf("unexpected APPIMAGE_EXTRACT_AND_RUN=1 when FUSE is available")
		}
	}
}

func TestDownloadAndVerifySuccess(t *testing.T) {
	payload := []byte("#!/bin/sh\necho streamlink 8.6.3\n")
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

	var progressCalls int
	progress := func(component string, percent int, readBytes, totalBytes int64) {
		progressCalls++
		if component != "streamlink" {
			t.Errorf("expected component streamlink, got %s", component)
		}
	}

	d := &Downloader{
		AllowedHosts: []string{u.Host},
		APIBaseURL:   ts.URL + "/api/releases/latest",
		DataDir:      dataDir,
		GOOS:         "linux",
		GOARCH:       "amd64",
		InsecureHTTP: true,
	}

	res, err := d.DownloadStreamlink(context.Background(), progress)
	if err != nil {
		t.Fatalf("DownloadStreamlink: %v", err)
	}

	if res.Origin != OriginManaged {
		t.Fatalf("origin = %s, want %s", res.Origin, OriginManaged)
	}
	if res.Version != "8.6.3-1" {
		t.Fatalf("version = %s, want 8.6.3-1", res.Version)
	}
	fi, err := os.Stat(res.Path)
	if err != nil {
		t.Fatalf("stat installed binary: %v", err)
	}
	if fi.Mode()&0o111 == 0 {
		t.Fatalf("installed binary is not executable: %v", fi.Mode())
	}
	if progressCalls == 0 {
		t.Fatal("progress callback was not called")
	}
}

func TestDownloadDigestMismatchDeletesFile(t *testing.T) {
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

	// Verify no corrupt files were left behind in dataDir
	baseDir := StreamlinkBaseDir(dataDir)
	entries, _ := os.ReadDir(baseDir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".download-") {
			t.Fatalf("temporary download dir was not deleted: %s", e.Name())
		}
	}
	versionDir := StreamlinkVersionDir(dataDir, "8.6.3-1")
	if _, err := os.Stat(versionDir); !os.IsNotExist(err) {
		t.Fatalf("version directory exists after failed download: %v", err)
	}
}

func TestDisallowedDomainRejected(t *testing.T) {
	d := &Downloader{
		AllowedHosts: []string{"api.github.com"},
		APIBaseURL:   "https://evil.attacker.com/releases/latest",
		GOOS:         "linux",
		GOARCH:       "amd64",
	}
	_, err := d.DownloadStreamlink(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for unapproved domain")
	}
	if !strings.Contains(err.Error(), "unapproved download host") {
		t.Fatalf("expected unapproved download host error, got: %v", err)
	}
}

func TestWindowsZipExtraction(t *testing.T) {
	// Build a fake portable zip
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
