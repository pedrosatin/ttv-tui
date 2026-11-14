package doctor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/pathlookup"
)

// chromiumNames are the executables streamlink 8.x looks up on PATH for its
// Chromium-based browser, on every system (webbrowser/chromium.py, names).
var chromiumNames = []string{
	"chromium",
	"chromium-browser",
	"chrome",
	"google-chrome",
	"google-chrome-stable",
}

// chromiumFallbacks are the paths streamlink 8.x tries after the names, in its
// order (webbrowser/chromium.py, fallback_paths). Linux has none. getenv and
// home stand for the environment and the home directory of the user.
func chromiumFallbacks(goos string, getenv func(string) string, home string) []string {
	switch goos {
	case "windows":
		var out []string
		join := func(base, sub, exe string) string { return base + `\` + sub + `\` + exe }
		for _, sub := range []string{`Microsoft\Edge\Application`, `Microsoft\Edge Beta\Application`, `Microsoft\Edge Dev\Application`} {
			for _, env := range []string{"PROGRAMFILES", "PROGRAMFILES(X86)"} {
				if base := getenv(env); base != "" {
					out = append(out, join(base, sub, "msedge.exe"))
				}
			}
		}
		for _, sub := range []string{`Google\Chrome\Application`, `Google\Chrome Beta\Application`, `Google\Chrome Canary\Application`} {
			for _, env := range []string{"PROGRAMFILES", "PROGRAMFILES(X86)", "LOCALAPPDATA"} {
				if base := getenv(env); base != "" {
					out = append(out, join(base, sub, "chrome.exe"))
				}
			}
		}
		return out
	case "darwin":
		out := []string{"/Applications/Chromium.app/Contents/MacOS/Chromium"}
		if home != "" {
			out = append(out, home+"/Applications/Chromium.app/Contents/MacOS/Chromium")
		}
		out = append(out, "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome")
		if home != "" {
			out = append(out, home+"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome")
		}
		return out
	default:
		return nil
	}
}

// browserLookup finds the browser the way streamlink does, with what the
// doctor may use: names only in the absolute PATH entries, and executable files.
type browserLookup struct {
	// onPath finds a name on PATH.
	onPath func(name string) (string, error)
	// executable checks one path, as Python's shutil.which does with a path.
	executable func(path string) (string, error)
}

func defaultBrowserLookup() browserLookup {
	return browserLookup{onPath: pathlookup.Abs, executable: exec.LookPath}
}

// find returns the first browser of names on PATH, then of fallbacks, and
// whether it came from PATH.
func (l browserLookup) find(names, fallbacks []string) (path string, onPath, ok bool) {
	for _, name := range names {
		if found, err := l.onPath(name); err == nil {
			return found, true, true
		}
	}
	for _, candidate := range fallbacks {
		if found, err := l.executable(candidate); err == nil {
			return found, false, true
		}
	}
	return "", false, false
}

// kickBrowserStatus reports the browser streamlink opens when the Kick API
// answers 403. With kick_browser set, only that path counts, as
// --webbrowser-executable replaces streamlink's search. ok is false only for a
// configured path that is not an executable file: without the key, a missing
// browser is a warning, since Kick asks for it only now and then.
func kickBrowserStatus(cfgBrowser, goos string, lookup browserLookup, cat *i18n.Catalog) (string, bool) {
	if cfgBrowser != "" {
		abs, err := filepath.Abs(cfgBrowser)
		if err == nil {
			_, err = lookup.executable(abs)
		}
		if err != nil {
			return fmt.Sprintf(cat.T("doctor.kick_browser.invalid"), cfgBrowser), false
		}
		return fmt.Sprintf("%s (%s)", abs, cat.T("doctor.origin.config")), true
	}
	home, _ := os.UserHomeDir()
	path, onPath, found := lookup.find(chromiumNames, chromiumFallbacks(goos, os.Getenv, home))
	switch {
	case !found:
		return cat.T("doctor.kick_browser.missing"), true
	case onPath:
		return fmt.Sprintf("%s (%s)", path, cat.T("doctor.origin.path")), true
	default:
		return path, true
	}
}
