package doctor

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/config"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

// The lists follow webbrowser/chromium.py of streamlink 8.6.2, in its order.
func TestChromiumFallbacksPerSystem(t *testing.T) {
	env := map[string]string{
		"PROGRAMFILES":      `C:\Program Files`,
		"PROGRAMFILES(X86)": `C:\Program Files (x86)`,
		"LOCALAPPDATA":      `C:\Users\u\AppData\Local`,
	}
	getenv := func(key string) string { return env[key] }
	windows := chromiumFallbacks("windows", getenv, `C:\Users\u`)
	wantWindows := []string{
		`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files\Microsoft\Edge Beta\Application\msedge.exe`,
		`C:\Program Files (x86)\Microsoft\Edge Beta\Application\msedge.exe`,
		`C:\Program Files\Microsoft\Edge Dev\Application\msedge.exe`,
		`C:\Program Files (x86)\Microsoft\Edge Dev\Application\msedge.exe`,
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
		`C:\Users\u\AppData\Local\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files\Google\Chrome Beta\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome Beta\Application\chrome.exe`,
		`C:\Users\u\AppData\Local\Google\Chrome Beta\Application\chrome.exe`,
		`C:\Program Files\Google\Chrome Canary\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome Canary\Application\chrome.exe`,
		`C:\Users\u\AppData\Local\Google\Chrome Canary\Application\chrome.exe`,
	}
	if !slices.Equal(windows, wantWindows) {
		t.Errorf("windows = %q\nwant %q", windows, wantWindows)
	}
	// A variable that is not set adds nothing, as in streamlink.
	delete(env, "PROGRAMFILES(X86)")
	if got := chromiumFallbacks("windows", getenv, ""); len(got) != 9 {
		t.Errorf("windows without PROGRAMFILES(X86) = %d paths, want 9", len(got))
	}

	darwin := chromiumFallbacks("darwin", getenv, "/Users/u")
	wantDarwin := []string{
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"/Users/u/Applications/Chromium.app/Contents/MacOS/Chromium",
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Users/u/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
	}
	if !slices.Equal(darwin, wantDarwin) {
		t.Errorf("darwin = %q\nwant %q", darwin, wantDarwin)
	}
	if got := chromiumFallbacks("linux", getenv, "/home/u"); len(got) != 0 {
		t.Errorf("linux = %q, want only the names on PATH", got)
	}
	if !slices.Equal(chromiumNames, []string{"chromium", "chromium-browser", "chrome", "google-chrome", "google-chrome-stable"}) {
		t.Errorf("names = %q", chromiumNames)
	}
}

// Names on PATH come first, then the fallback paths, as in streamlink.
func TestBrowserLookupOrder(t *testing.T) {
	var asked []string
	lookup := browserLookup{
		onPath: func(name string) (string, error) {
			asked = append(asked, "path:"+name)
			if name == "google-chrome" {
				return "/usr/bin/google-chrome", nil
			}
			return "", errors.New("not found")
		},
		executable: func(path string) (string, error) {
			asked = append(asked, "file:"+path)
			if path == "/b" {
				return path, nil
			}
			return "", errors.New("not found")
		},
	}
	path, onPath, ok := lookup.find(chromiumNames, []string{"/a", "/b"})
	if !ok || !onPath || path != "/usr/bin/google-chrome" {
		t.Fatalf("find = %q, %v, %v", path, onPath, ok)
	}
	if want := []string{"path:chromium", "path:chromium-browser", "path:chrome", "path:google-chrome"}; !slices.Equal(asked, want) {
		t.Errorf("asked %q, want %q", asked, want)
	}

	asked = nil
	path, onPath, ok = lookup.find([]string{"chromium"}, []string{"/a", "/b"})
	if !ok || onPath || path != "/b" {
		t.Fatalf("fallback find = %q, %v, %v", path, onPath, ok)
	}
	if want := []string{"path:chromium", "file:/a", "file:/b"}; !slices.Equal(asked, want) {
		t.Errorf("asked %q, want %q", asked, want)
	}

	if _, _, ok := lookup.find([]string{"chromium"}, []string{"/a"}); ok {
		t.Error("nothing to find: want not found")
	}
}

func TestKickBrowserStatusConfiguredPath(t *testing.T) {
	cat, _ := i18n.New("en")
	dir := t.TempDir()
	fakeTool(t, dir, "brave")
	browser := filepath.Join(dir, "brave")
	if runtime.GOOS == "windows" {
		browser += ".exe"
	}
	never := browserLookup{
		onPath: func(string) (string, error) {
			t.Error("searched PATH with kick_browser set")
			return "", errors.New("no")
		},
		executable: defaultBrowserLookup().executable,
	}
	got, ok := kickBrowserStatus(browser, runtime.GOOS, never, cat)
	if !ok || got != browser+" (config)" {
		t.Errorf("configured browser = %q, %v", got, ok)
	}

	// A relative path is resolved against the working directory, as streamlink_path.
	t.Chdir(dir)
	got, ok = kickBrowserStatus(filepath.Base(browser), runtime.GOOS, never, cat)
	if !ok || !strings.HasSuffix(got, string(filepath.Separator)+filepath.Base(browser)+" (config)") || !filepath.IsAbs(strings.TrimSuffix(got, " (config)")) {
		t.Errorf("relative browser = %q, %v", got, ok)
	}

	if runtime.GOOS != "windows" {
		// A file that cannot run is what streamlink refuses too.
		plain := filepath.Join(dir, "chromium")
		if err := os.WriteFile(plain, []byte("text"), 0o600); err != nil {
			t.Fatal(err)
		}
		if got, ok := kickBrowserStatus(plain, runtime.GOOS, never, cat); ok || !strings.Contains(got, "INVALID") {
			t.Errorf("non-executable browser = %q, %v", got, ok)
		}
	}
	if got, ok := kickBrowserStatus(filepath.Join(dir, "missing"), runtime.GOOS, never, cat); ok || !strings.Contains(got, "INVALID") {
		t.Errorf("missing browser = %q, %v", got, ok)
	}
	if got, ok := kickBrowserStatus("", runtime.GOOS, browserLookup{
		onPath:     func(string) (string, error) { return "", errors.New("no") },
		executable: func(string) (string, error) { return "", errors.New("no") },
	}, cat); !ok || !strings.Contains(got, "kick_browser") {
		t.Errorf("no browser = %q, %v: want a warning naming kick_browser", got, ok)
	}
}

// The report reads kick_browser from config.toml. A browser that is missing is
// a warning; a configured path that cannot run fails the report. The path is
// external text and leaves the doctor neutralized.
func TestReportKickBrowser(t *testing.T) {
	cases := []struct {
		name     string
		setup    func(t *testing.T, base string) string
		wantText func(base string) []string
		wantCode int
	}{
		{"on PATH", func(t *testing.T, base string) string {
			fakeTool(t, base, "google-chrome")
			return ""
		}, func(base string) []string {
			name := filepath.Join(base, "google-chrome")
			if runtime.GOOS == "windows" {
				name += ".exe"
			}
			return []string{"Kick browser (HTTP 403 check): " + name + " (PATH)"}
		}, 0},
		{"configured", func(t *testing.T, base string) string {
			dir := filepath.Join(base, "browsers")
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			fakeTool(t, dir, "brave")
			name := filepath.Join(dir, "brave")
			if runtime.GOOS == "windows" {
				name += ".exe"
			}
			return name
		}, func(base string) []string {
			return []string{"Kick browser (HTTP 403 check): " + filepath.Join(base, "browsers", "brave")}
		}, 0},
		{"configured but missing", func(t *testing.T, base string) string {
			return filepath.Join(base, "nada", "brave\x1b[31m")
		}, func(string) []string {
			return []string{"INVALID: kick_browser (", "is not an executable file"}
		}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			base := isolate(t)
			fakeTool(t, base, "streamlink")
			fakeTool(t, base, "mpv")
			t.Setenv("PATH", base)
			if browser := c.setup(t, base); browser != "" {
				if err := config.Save(config.Config{KickBrowser: browser}); err != nil {
					t.Fatal(err)
				}
			}
			var out bytes.Buffer
			code := Run(&out, Options{Version: "test", Lang: "en"})
			for _, text := range c.wantText(base) {
				if !strings.Contains(out.String(), text) {
					t.Errorf("report lacks %q:\n%s", text, out.String())
				}
			}
			if strings.Contains(out.String(), "\x1b") {
				t.Errorf("report carries an escape sequence:\n%q", out.String())
			}
			if code != c.wantCode {
				t.Errorf("exit = %d, want %d", code, c.wantCode)
			}
		})
	}
}

// TestReportKickBrowserMissing runs only where streamlink has no fallback
// paths: on macOS and Windows a browser installed on the machine would count.
func TestReportKickBrowserMissing(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("streamlink also looks in fixed paths outside Linux")
	}
	for lang, want := range map[string]string{
		"en":    "Kick browser (HTTP 403 check): not found",
		"pt-BR": "Navegador da Kick (checagem do HTTP 403): não encontrado",
	} {
		t.Run(lang, func(t *testing.T) {
			base := isolate(t)
			fakeTool(t, base, "streamlink")
			fakeTool(t, base, "mpv")
			t.Setenv("PATH", base)
			var out bytes.Buffer
			if code := Run(&out, Options{Version: "test", Lang: lang}); code != 0 {
				t.Errorf("exit = %d: a missing browser is only a warning", code)
			}
			if !strings.Contains(out.String(), want) || !strings.Contains(out.String(), "kick_browser") {
				t.Errorf("report lacks %q and the config key:\n%s", want, out.String())
			}
		})
	}
}
