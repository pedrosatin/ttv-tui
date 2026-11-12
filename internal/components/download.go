package components

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/filelock"
)

// AllowedDomains are the only hostnames accepted for release metadata and asset downloads.
var AllowedDomains = []string{
	"api.github.com",
	"github.com",
	"release-assets.githubusercontent.com",
	"objects.githubusercontent.com",
}

const (
	maxReleaseJSONBytes     = 2 * 1024 * 1024   // 2 MB
	maxUncompressedZipBytes = 500 * 1024 * 1024 // 500 MB

	// DefaultDownloadStaleAge is the duration after which an unrenewed download lock is considered abandoned.
	DefaultDownloadStaleAge = 30 * time.Second
	// DefaultDownloadHeartbeat is the interval at which a download lock holder refreshes its lock mtime.
	DefaultDownloadHeartbeat = 5 * time.Second
)

// ProgressFunc reports download progress. When percent < 0, it indicates waiting for another process's lock.
type ProgressFunc func(component string, percent int, readBytes, totalBytes int64)

// Downloader manages downloading and verifying managed components.
type Downloader struct {
	Transport       http.RoundTripper
	AllowedHosts    []string
	APIBaseURL      string
	DataDir         string
	GOOS            string
	GOARCH          string
	InsecureHTTP    bool // For tests only
	ExtractAppImage func(ctx context.Context, appImagePath, destDir string) error
	LookPath        func(string) (string, error)
	LockStaleAge    time.Duration
	LockHeartbeat   time.Duration
	OnLockAcquired  func()
	// Now is the clock for update checks; nil uses time.Now.
	Now func() time.Time
	// UseStaleAge and UseHeartbeat tune the use markers; zero uses the defaults.
	UseStaleAge  time.Duration
	UseHeartbeat time.Duration
}

func (d *Downloader) lockStaleAge() time.Duration {
	if d.LockStaleAge > 0 {
		return d.LockStaleAge
	}
	return DefaultDownloadStaleAge
}

func (d *Downloader) lockHeartbeat() time.Duration {
	if d.LockHeartbeat > 0 {
		return d.LockHeartbeat
	}
	return DefaultDownloadHeartbeat
}

// NewDownloader returns a Downloader with production defaults.
func NewDownloader(dataDir string) *Downloader {
	return &Downloader{
		AllowedHosts: AllowedDomains,
		DataDir:      dataDir,
		GOOS:         runtime.GOOS,
		GOARCH:       runtime.GOARCH,
		LookPath:     defaultLookPath,
	}
}

type downloadCall struct {
	done chan struct{}
	res  Result
	err  error
}

var (
	downloadMu      sync.Mutex
	activeDownloads = make(map[string]*downloadCall)
)

// gitHubRelease represents the minimal GitHub release structure.
type gitHubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []gitHubAsset `json:"assets"`
}

// gitHubAsset represents a single release asset with its published digest.
type gitHubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
	Digest             string `json:"digest"`
}

// isHostAllowed checks whether host is on the allowed list.
func (d *Downloader) isHostAllowed(host string) bool {
	if idx := strings.IndexByte(host, ':'); idx >= 0 {
		host = host[:idx]
	}
	allowed := d.AllowedHosts
	if len(allowed) == 0 {
		allowed = AllowedDomains
	}
	for _, a := range allowed {
		if idx := strings.IndexByte(a, ':'); idx >= 0 {
			a = a[:idx]
		}
		if strings.EqualFold(host, a) {
			return true
		}
	}
	return false
}

// validateURL ensures rawURL uses HTTPS (unless InsecureHTTP is enabled for tests)
// and points to an allowed domain.
func (d *Downloader) validateURL(rawURL string) (*url.URL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if !d.InsecureHTTP && u.Scheme != "https" {
		return nil, fmt.Errorf("insecure URL scheme %q, only HTTPS is allowed", u.Scheme)
	}
	if !d.isHostAllowed(u.Host) {
		return nil, fmt.Errorf("unapproved download host %q (allowed: %s)", u.Host, strings.Join(d.AllowedHosts, ", "))
	}
	return u, nil
}

// httpClient returns an HTTP client configured to enforce domain whitelisting across redirects.
func (d *Downloader) httpClient() *http.Client {
	transport := d.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &http.Client{
		Transport: transport,
		Timeout:   5 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			if _, err := d.validateURL(req.URL.String()); err != nil {
				return err
			}
			return nil
		},
	}
}

// repoForPlatform returns the GitHub repo (owner/repo) for streamlink builds on the target OS.
func (d *Downloader) repoForPlatform() (string, error) {
	switch d.GOOS {
	case "windows":
		return "streamlink/windows-builds", nil
	case "linux":
		return "streamlink/streamlink-appimage", nil
	case "darwin":
		return "", errors.New("managed streamlink is not supported on macOS")
	default:
		return "", fmt.Errorf("unsupported OS %q for managed streamlink", d.GOOS)
	}
}

// matchAsset selects the asset for the target OS and CPU architecture.
func (d *Downloader) matchAsset(assets []gitHubAsset) (*gitHubAsset, error) {
	for i := range assets {
		a := &assets[i]
		name := strings.ToLower(a.Name)
		if d.GOOS == "windows" {
			if strings.HasPrefix(name, "streamlink-") && strings.HasSuffix(name, "-x86_64.zip") {
				return a, nil
			}
		} else if d.GOOS == "linux" {
			if strings.HasSuffix(name, ".zsync") {
				continue
			}
			if !strings.HasPrefix(name, "streamlink-") {
				continue
			}
			if d.GOARCH == "amd64" && strings.HasSuffix(name, "_x86_64.appimage") {
				return a, nil
			}
			if d.GOARCH == "arm64" && strings.HasSuffix(name, "_aarch64.appimage") {
				return a, nil
			}
		}
	}
	return nil, fmt.Errorf("no matching streamlink asset found for %s/%s", d.GOOS, d.GOARCH)
}

func validateTagName(tag string) error {
	if tag == "" || tag == "." || tag == ".." {
		return errors.New("invalid empty or dot tag name")
	}
	if strings.ContainsAny(tag, `/\`) || strings.Contains(tag, "..") {
		return fmt.Errorf("tag name %q contains illegal path characters", tag)
	}
	return nil
}

// DownloadStreamlink coordinates in-process singleflight and invokes the download routine.
func (d *Downloader) DownloadStreamlink(ctx context.Context, progress ProgressFunc) (Result, error) {
	key := filepath.Clean(d.DataDir)
	downloadMu.Lock()
	call, active := activeDownloads[key]
	if active {
		downloadMu.Unlock()
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-call.done:
			return call.res, call.err
		}
	}
	call = &downloadCall{done: make(chan struct{})}
	activeDownloads[key] = call
	downloadMu.Unlock()

	defer func() {
		downloadMu.Lock()
		delete(activeDownloads, key)
		downloadMu.Unlock()
		close(call.done)
	}()

	res, err := d.doDownloadStreamlink(ctx, progress)
	call.res = res
	call.err = err
	return res, err
}

// doDownloadStreamlink performs the actual download, SHA-256 verification, and atomic install.
func (d *Downloader) doDownloadStreamlink(ctx context.Context, progress ProgressFunc) (Result, error) {
	repo, err := d.repoForPlatform()
	if err != nil {
		return Result{}, err
	}

	baseDir := StreamlinkBaseDir(d.DataDir)
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return Result{}, fmt.Errorf("create components directory: %w", err)
	}

	// Inter-process lock to prevent concurrent downloads and modifications.
	lockPath := filepath.Join(baseDir, ".lock")
	staleAge := d.lockStaleAge()
	heartbeat := d.lockHeartbeat()
	unlock, err := filelock.LockWithHeartbeat(ctx, lockPath, staleAge, heartbeat, func() {
		if progress != nil {
			progress("streamlink", -1, 0, 0)
		}
	})
	if err != nil {
		return Result{}, fmt.Errorf("acquire download lock: %w", err)
	}
	defer func() { _ = unlock() }()
	if d.OnLockAcquired != nil {
		d.OnLockAcquired()
	}

	// If another process just finished installing, use the existing installation.
	resolver := &Resolver{
		DataDir:  d.DataDir,
		GOOS:     d.GOOS,
		GOARCH:   d.GOARCH,
		LookPath: d.LookPath,
		HasFUSE:  DefaultHasFUSE,
	}
	if res, ok := resolver.findManagedStreamlink(); ok {
		return res, nil
	}

	// Clean up stale temporary directories left by crashed processes.
	cleanStaleTemps(baseDir)

	rel, err := d.fetchLatest(ctx, repo)
	if err != nil {
		return Result{}, err
	}
	// Nothing else is installed, so even a version once marked bad is the only
	// option; forget the mark so the resolver accepts the fresh install.
	if err := d.updateState(ctx, func(st *UpdateState) (bool, error) {
		if !st.IsBad(rel.Version) {
			return false, nil
		}
		st.Bad = slices.DeleteFunc(st.Bad, func(v string) bool { return v == rel.Version })
		return true, nil
	}); err != nil {
		return Result{}, err
	}

	if err := d.installRelease(ctx, baseDir, rel, progress); err != nil {
		return Result{}, err
	}
	res, ok := resolver.findManagedStreamlink()
	if !ok {
		return Result{}, fmt.Errorf("managed streamlink binary not found in %s after install", StreamlinkVersionDir(d.DataDir, rel.Version))
	}
	return res, nil
}

// latestRelease is the newest release of a platform build and the asset to
// install, with the SHA-256 GitHub publishes for it.
type latestRelease struct {
	Version string
	Asset   *gitHubAsset
	Name    string
	Hash    string
}

// fetchLatest reads the repository's latest release from the GitHub API and
// picks the asset for the target platform. It downloads no asset.
func (d *Downloader) fetchLatest(ctx context.Context, repo string) (latestRelease, error) {
	apiURL := d.APIBaseURL
	if apiURL == "" {
		apiURL = fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	}
	if _, err := d.validateURL(apiURL); err != nil {
		return latestRelease{}, fmt.Errorf("validate API URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return latestRelease{}, fmt.Errorf("create API request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "ttv-tui")

	client := d.httpClient()
	resp, err := client.Do(req)
	if err != nil {
		return latestRelease{}, fmt.Errorf("fetch latest release: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return latestRelease{}, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var rel gitHubRelease
	lr := io.LimitReader(resp.Body, maxReleaseJSONBytes)
	if err := json.NewDecoder(lr).Decode(&rel); err != nil {
		return latestRelease{}, fmt.Errorf("decode release JSON: %w", err)
	}

	if err := validateTagName(rel.TagName); err != nil {
		return latestRelease{}, fmt.Errorf("invalid release tag: %w", err)
	}

	asset, err := d.matchAsset(rel.Assets)
	if err != nil {
		return latestRelease{}, fmt.Errorf("match asset: %w", err)
	}

	assetName := filepath.Base(asset.Name)
	if assetName == "" || assetName == "." || assetName == ".." || strings.Contains(assetName, "..") {
		return latestRelease{}, fmt.Errorf("invalid asset filename %q", asset.Name)
	}

	expectedDigest := strings.TrimSpace(asset.Digest)
	if !strings.HasPrefix(strings.ToLower(expectedDigest), "sha256:") {
		return latestRelease{}, fmt.Errorf("asset %q has no valid sha256 digest in GitHub API (got %q)", asset.Name, expectedDigest)
	}
	return latestRelease{
		Version: rel.TagName,
		Asset:   asset,
		Name:    assetName,
		Hash:    strings.ToLower(strings.TrimPrefix(expectedDigest, "sha256:")),
	}, nil
}

// installRelease downloads the release asset, checks its SHA-256, and promotes
// a complete installation to the version directory with one rename. The caller
// holds the download lock. A failed check leaves nothing behind, and an
// existing complete installation of the same version is kept.
func (d *Downloader) installRelease(ctx context.Context, baseDir string, rel latestRelease, progress ProgressFunc) error {
	// Download into a temporary directory inside baseDir.
	tempDir, err := os.MkdirTemp(baseDir, ".download-*")
	if err != nil {
		return fmt.Errorf("create temp download dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	tempFile := filepath.Join(tempDir, rel.Name)
	if err := d.downloadAndVerify(ctx, rel.Asset.BrowserDownloadURL, tempFile, rel.Asset.Size, rel.Hash, progress); err != nil {
		return fmt.Errorf("download asset: %w", err)
	}

	// Prepare complete installation in a separate staging directory inside baseDir.
	stagingDir, err := os.MkdirTemp(baseDir, ".install-*")
	if err != nil {
		return fmt.Errorf("create staging directory: %w", err)
	}
	// stagingDir is deleted if not promoted.
	staged := false
	defer func() {
		if !staged {
			_ = os.RemoveAll(stagingDir)
		}
	}()

	if d.GOOS == "windows" {
		if _, err := installWindowsZip(tempFile, stagingDir); err != nil {
			return fmt.Errorf("extract streamlink archive: %w", err)
		}
	} else {
		// Linux AppImage: extract via --appimage-extract
		extractFn := d.ExtractAppImage
		if extractFn == nil {
			extractFn = defaultExtractAppImage
		}
		if err := extractFn(ctx, tempFile, stagingDir); err != nil {
			return fmt.Errorf("extract appimage: %w", err)
		}
		if findStreamlinkBinaryInDir(stagingDir, d.GOOS) == "" {
			return fmt.Errorf("AppRun executable not found after extracting AppImage")
		}
	}

	targetDir := StreamlinkVersionDir(d.DataDir, rel.Version)
	if findStreamlinkBinaryInDir(targetDir, d.GOOS) != "" {
		_ = os.RemoveAll(stagingDir)
		staged = true
		return nil
	}
	// Under the lock, remove an empty or partial target directory before promotion.
	_ = os.RemoveAll(targetDir)
	if err := renameDir(stagingDir, targetDir); err != nil {
		return fmt.Errorf("atomic rename staging to version dir %s: %w", targetDir, err)
	}
	staged = true
	return nil
}

func renameDir(src, dst string) error {
	if runtime.GOOS != "windows" {
		return os.Rename(src, dst)
	}
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		err = os.Rename(src, dst)
		if err == nil {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return err
}

func cleanStaleTemps(baseDir string) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".download-") || strings.HasPrefix(name, ".install-") || strings.HasPrefix(name, ".tmp-") {
			_ = os.RemoveAll(filepath.Join(baseDir, name))
		}
	}
}

func defaultExtractAppImage(ctx context.Context, appImagePath, destDir string) error {
	absAppImage, err := filepath.Abs(appImagePath)
	if err != nil {
		return fmt.Errorf("resolve appimage path: %w", err)
	}
	absDest, err := filepath.Abs(destDir)
	if err != nil {
		return fmt.Errorf("resolve dest dir: %w", err)
	}
	if err := os.Chmod(absAppImage, 0o700); err != nil {
		return fmt.Errorf("chmod appimage: %w", err)
	}
	cmd := exec.CommandContext(ctx, absAppImage, "--appimage-extract")
	cmd.Dir = absDest
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("execute appimage --appimage-extract: %w (output: %s)", err, strings.TrimSpace(string(output)))
	}
	appRun := filepath.Join(absDest, "squashfs-root", "AppRun")
	if fi, err := os.Stat(appRun); err != nil || fi.IsDir() {
		return fmt.Errorf("extracted AppRun not found at %s: %w", appRun, err)
	}
	return nil
}

// downloadAndVerify streams the download from urlStr to destPath while computing
// SHA-256 and enforcing size limits. If verification fails, destPath is deleted immediately.
func (d *Downloader) downloadAndVerify(ctx context.Context, urlStr, destPath string, expectedSize int64, expectedHash string, progress ProgressFunc) error {
	if _, err := d.validateURL(urlStr); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}
	req.Header.Set("User-Agent", "ttv-tui")

	client := d.httpClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("execute download request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download server returned HTTP %d", resp.StatusCode)
	}

	totalBytes := expectedSize
	if totalBytes <= 0 && resp.ContentLength > 0 {
		totalBytes = resp.ContentLength
	}

	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create download file %s: %w", destPath, err)
	}
	defer func() { _ = f.Close() }()

	hasher := sha256.New()
	buf := make([]byte, 64*1024)
	var readBytes int64
	lastPct := -1

	// Read with LimitReader to prevent exceeding expected asset size + 1.
	var bodyReader io.Reader = resp.Body
	if expectedSize > 0 {
		bodyReader = io.LimitReader(resp.Body, expectedSize+1)
	}

	for {
		nr, rerr := bodyReader.Read(buf)
		if nr > 0 {
			if expectedSize > 0 && readBytes+int64(nr) > expectedSize {
				_ = os.Remove(destPath)
				return fmt.Errorf("download exceeded expected asset size of %d bytes", expectedSize)
			}
			nw, werr := f.Write(buf[:nr])
			if werr != nil {
				_ = os.Remove(destPath)
				return fmt.Errorf("write download chunk: %w", werr)
			}
			hasher.Write(buf[:nw])
			readBytes += int64(nw)
			if progress != nil && totalBytes > 0 {
				pct := int(float64(readBytes) / float64(totalBytes) * 100)
				if pct != lastPct {
					lastPct = pct
					progress("streamlink", pct, readBytes, totalBytes)
				}
			}
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				break
			}
			_ = os.Remove(destPath)
			return fmt.Errorf("read download chunk: %w", rerr)
		}
	}
	_ = f.Close()

	if expectedSize > 0 && readBytes < expectedSize {
		_ = os.Remove(destPath)
		return fmt.Errorf("download truncated: got %d bytes, expected %d", readBytes, expectedSize)
	}

	computedHash := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(computedHash, expectedHash) {
		_ = os.Remove(destPath)
		return fmt.Errorf("SHA-256 digest mismatch: got %s, expected %s", computedHash, expectedHash)
	}

	if progress != nil {
		progress("streamlink", 100, readBytes, totalBytes)
	}
	return nil
}

// installWindowsZip extracts a Windows portable zip into destDir, stripping
// any common root directory if all entries share one.
func installWindowsZip(zipPath, destDir string) (string, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", fmt.Errorf("open zip archive: %w", err)
	}
	defer func() { _ = r.Close() }()

	// Detect if all files share a common root directory
	var commonPrefix string
	first := true
	for _, f := range r.File {
		parts := strings.Split(strings.Trim(f.Name, "/\\"), "/")
		if len(parts) > 1 {
			prefix := parts[0] + "/"
			if first {
				commonPrefix = prefix
				first = false
			} else if !strings.HasPrefix(f.Name, commonPrefix) {
				commonPrefix = ""
				break
			}
		} else if !f.FileInfo().IsDir() {
			commonPrefix = ""
			break
		}
	}

	cleanDest := filepath.Clean(destDir) + string(filepath.Separator)
	var totalUncompressed int64

	for _, f := range r.File {
		mode := f.Mode()
		if !mode.IsDir() && !mode.IsRegular() {
			return "", fmt.Errorf("illegal entry in zip archive: %s (mode: %v)", f.Name, mode)
		}

		name := f.Name
		if hasDotDotSegment(name) {
			return "", fmt.Errorf("illegal relative path in zip: %s", name)
		}
		if commonPrefix != "" && strings.HasPrefix(name, commonPrefix) {
			name = strings.TrimPrefix(name, commonPrefix)
		}
		name = strings.TrimLeft(name, "/\\")
		if name == "" {
			continue
		}

		targetPath := filepath.Join(destDir, filepath.FromSlash(name))
		cleanTarget := filepath.Clean(targetPath)
		if !strings.HasPrefix(cleanTarget+string(filepath.Separator), cleanDest) && cleanTarget != filepath.Clean(destDir) {
			return "", fmt.Errorf("illegal file path in zip: %s", f.Name)
		}

		if mode.IsDir() {
			if err := os.MkdirAll(targetPath, 0o700); err != nil {
				return "", err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
			return "", err
		}

		rc, err := f.Open()
		if err != nil {
			return "", err
		}

		remaining := maxUncompressedZipBytes - totalUncompressed
		if remaining <= 0 {
			_ = rc.Close()
			return "", fmt.Errorf("zip archive exceeds maximum uncompressed size of %d bytes", maxUncompressedZipBytes)
		}

		perm := os.FileMode(0o600)
		if mode&0o111 != 0 || strings.HasSuffix(strings.ToLower(targetPath), ".exe") {
			perm = 0o700
		}

		outFile, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
		if err != nil {
			_ = rc.Close()
			return "", err
		}

		limitedReader := io.LimitReader(rc, remaining+1)
		nw, copyErr := io.Copy(outFile, limitedReader)
		closeErr := outFile.Close()
		_ = rc.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}

		totalUncompressed += nw
		if totalUncompressed > maxUncompressedZipBytes {
			return "", fmt.Errorf("zip archive exceeds maximum uncompressed size of %d bytes", maxUncompressedZipBytes)
		}
	}

	bin := findStreamlinkBinaryInDir(destDir, "windows")
	if bin == "" {
		return "", fmt.Errorf("streamlink.exe not found after extracting zip to %s", destDir)
	}
	return bin, nil
}

func hasDotDotSegment(path string) bool {
	for _, part := range strings.FieldsFunc(path, func(r rune) bool {
		return r == '/' || r == '\\'
	}) {
		if part == ".." {
			return true
		}
	}
	return false
}
