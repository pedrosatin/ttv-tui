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
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// AllowedDomains are the only hostnames accepted for release metadata and asset downloads.
var AllowedDomains = []string{
	"api.github.com",
	"github.com",
	"release-assets.githubusercontent.com",
	"objects.githubusercontent.com",
}

// ProgressFunc reports download progress.
type ProgressFunc func(component string, percent int, readBytes, totalBytes int64)

// Downloader manages downloading and verifying managed components.
type Downloader struct {
	HTTPClient   *http.Client
	AllowedHosts []string
	APIBaseURL   string
	DataDir      string
	GOOS         string
	GOARCH       string
	InsecureHTTP bool // For tests only
}

// NewDownloader returns a Downloader with production defaults.
func NewDownloader(dataDir string) *Downloader {
	return &Downloader{
		AllowedHosts: AllowedDomains,
		DataDir:      dataDir,
		GOOS:         runtime.GOOS,
		GOARCH:       runtime.GOARCH,
	}
}

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

// validateURL ensures urlStr uses HTTPS (unless InsecureHTTP is enabled for tests)
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
	if d.HTTPClient != nil {
		return d.HTTPClient
	}
	return &http.Client{
		Timeout: 5 * time.Minute,
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

// ManualInstallHint returns platform-specific instructions for installing streamlink manually.
func ManualInstallHint(goos string) string {
	switch goos {
	case "windows":
		return "baixe o instalador oficial em https://github.com/streamlink/windows-builds/releases ou use o Scoop ('scoop install streamlink')"
	case "darwin":
		return "instale o streamlink com 'brew install streamlink'"
	default:
		return "instale o streamlink pelo gerenciador da sua distribuição (ex: pacman, apt) ou com 'pipx install streamlink' (https://streamlink.github.io/install.html)"
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
		return "", errors.New("managed streamlink is not supported on macOS (use brew install streamlink)")
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

// DownloadStreamlink downloads, verifies SHA-256 against GitHub API digest,
// and installs streamlink in the components data directory.
func (d *Downloader) DownloadStreamlink(ctx context.Context, progress ProgressFunc) (Result, error) {
	repo, err := d.repoForPlatform()
	if err != nil {
		return Result{}, err
	}

	apiURL := d.APIBaseURL
	if apiURL == "" {
		apiURL = fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	}
	if _, err := d.validateURL(apiURL); err != nil {
		return Result{}, fmt.Errorf("validate API URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return Result{}, fmt.Errorf("create API request: %w; %s", err, ManualInstallHint(d.GOOS))
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "ttv-tui")

	client := d.httpClient()
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("fetch latest release: %w; %s", err, ManualInstallHint(d.GOOS))
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("GitHub API returned status %d; %s", resp.StatusCode, ManualInstallHint(d.GOOS))
	}

	var rel gitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return Result{}, fmt.Errorf("decode release JSON: %w; %s", err, ManualInstallHint(d.GOOS))
	}

	asset, err := d.matchAsset(rel.Assets)
	if err != nil {
		return Result{}, fmt.Errorf("match asset: %w; %s", err, ManualInstallHint(d.GOOS))
	}

	expectedDigest := strings.TrimSpace(asset.Digest)
	if !strings.HasPrefix(strings.ToLower(expectedDigest), "sha256:") {
		return Result{}, fmt.Errorf("asset %q has no valid sha256 digest in GitHub API (got %q); %s", asset.Name, expectedDigest, ManualInstallHint(d.GOOS))
	}
	expectedHash := strings.ToLower(strings.TrimPrefix(expectedDigest, "sha256:"))

	version := rel.TagName
	if version == "" {
		version = "latest"
	}

	targetDir := StreamlinkVersionDir(d.DataDir, version)
	baseDir := StreamlinkBaseDir(d.DataDir)
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("create components directory: %w", err)
	}

	// Download into a temporary file
	tempDir, err := os.MkdirTemp(baseDir, ".download-*")
	if err != nil {
		return Result{}, fmt.Errorf("create temp download dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	tempFile := filepath.Join(tempDir, asset.Name)
	if err := d.downloadAndVerify(ctx, asset.BrowserDownloadURL, tempFile, asset.Size, expectedHash, progress); err != nil {
		return Result{}, fmt.Errorf("download asset: %w; %s", err, ManualInstallHint(d.GOOS))
	}

	// Unpack or install into targetDir
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("create version directory %s: %w", targetDir, err)
	}

	var binPath string
	if d.GOOS == "windows" {
		binPath, err = installWindowsZip(tempFile, targetDir)
		if err != nil {
			_ = os.RemoveAll(targetDir)
			return Result{}, fmt.Errorf("extract streamlink archive: %w", err)
		}
	} else {
		// Linux AppImage
		destFile := filepath.Join(targetDir, "streamlink")
		if err := copyAndChmodExecutable(tempFile, destFile); err != nil {
			_ = os.RemoveAll(targetDir)
			return Result{}, fmt.Errorf("install streamlink binary: %w", err)
		}
		binPath = destFile
	}

	resolver := &Resolver{
		DataDir:  d.DataDir,
		GOOS:     d.GOOS,
		GOARCH:   d.GOARCH,
		LookPath: osLookPath,
		HasFUSE:  DefaultHasFUSE,
	}
	res, ok := resolver.findManagedStreamlink()
	if !ok {
		return Result{
			Path:    binPath,
			Version: version,
			Origin:  OriginManaged,
		}, nil
	}
	return res, nil
}

// downloadAndVerify streams the download from urlStr to destPath while computing
// SHA-256. If digest verification fails, destPath is deleted immediately.
func (d *Downloader) downloadAndVerify(ctx context.Context, urlStr, destPath string, totalBytes int64, expectedHash string, progress ProgressFunc) error {
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

	for {
		nr, rerr := resp.Body.Read(buf)
		if nr > 0 {
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

// copyAndChmodExecutable copies src to dst and sets 0755 permissions.
func copyAndChmodExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		_ = os.Remove(dst)
		return err
	}
	return out.Close()
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

	for _, f := range r.File {
		name := f.Name
		if commonPrefix != "" && strings.HasPrefix(name, commonPrefix) {
			name = strings.TrimPrefix(name, commonPrefix)
		}
		name = strings.TrimLeft(name, "/\\")
		if name == "" {
			continue
		}

		targetPath := filepath.Join(destDir, filepath.FromSlash(name))
		if !strings.HasPrefix(filepath.Clean(targetPath)+string(filepath.Separator), cleanDest) && filepath.Clean(targetPath) != filepath.Clean(destDir) {
			return "", fmt.Errorf("illegal file path in zip: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return "", err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return "", err
		}

		rc, err := f.Open()
		if err != nil {
			return "", err
		}

		outFile, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			_ = rc.Close()
			return "", err
		}

		_, copyErr := io.Copy(outFile, rc)
		closeErr := outFile.Close()
		_ = rc.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
	}

	bin := findStreamlinkBinaryInDir(destDir, "windows")
	if bin == "" {
		return "", fmt.Errorf("streamlink.exe not found after extracting zip to %s", destDir)
	}
	return bin, nil
}
