package config

import (
	"errors"
	"path/filepath"
	"testing"
)

// fakeEnv builds a pathEnv that touches neither the real environment nor the
// real home directory.
func fakeEnv(goos string, vars map[string]string) pathEnv {
	return pathEnv{
		goos:    goos,
		getenv:  func(k string) string { return vars[k] },
		home:    func() (string, error) { return filepath.Join(string(filepath.Separator), "fake", "home"), nil },
		tempDir: func() string { return filepath.Join(string(filepath.Separator), "fake", "tmp") },
	}
}

func fakeHome(elem ...string) string {
	return filepath.Join(append([]string{string(filepath.Separator), "fake", "home"}, elem...)...)
}

var allGOOS = []string{"linux", "darwin", "windows", "freebsd"}

func TestXDGWinsOnEveryOS(t *testing.T) {
	vars := map[string]string{
		"XDG_CONFIG_HOME": "/xdg/config",
		"XDG_STATE_HOME":  "/xdg/state",
		"XDG_RUNTIME_DIR": "/xdg/run",
		"APPDATA":         "/win/roaming",
		"LOCALAPPDATA":    "/win/local",
		"TMPDIR":          "/mac/tmp",
	}
	for _, goos := range allGOOS {
		t.Run(goos, func(t *testing.T) {
			e := fakeEnv(goos, vars)
			if got, err := e.configDir(); err != nil || got != filepath.Join("/xdg/config", "ttv-tui") {
				t.Fatalf("configDir = %q, %v", got, err)
			}
			if got, err := e.twtDir(); err != nil || got != filepath.Join("/xdg/config", "twt") {
				t.Fatalf("twtDir = %q, %v", got, err)
			}
			if got, err := e.stateDir(); err != nil || got != filepath.Join("/xdg/state", "ttv-tui") {
				t.Fatalf("stateDir = %q, %v", got, err)
			}
			if got := e.runtimeDir(); got != filepath.Join("/xdg/run", "ttv-tui") {
				t.Fatalf("runtimeDir = %q", got)
			}
		})
	}
}

func TestXDGWinsEvenWithoutHome(t *testing.T) {
	vars := map[string]string{"XDG_CONFIG_HOME": "/xdg/config", "XDG_STATE_HOME": "/xdg/state"}
	for _, goos := range allGOOS {
		e := fakeEnv(goos, vars)
		e.home = func() (string, error) { return "", errors.New("no home") }
		if _, err := e.configDir(); err != nil {
			t.Errorf("%s: configDir with XDG must not need a home: %v", goos, err)
		}
		if _, err := e.stateDir(); err != nil {
			t.Errorf("%s: stateDir with XDG must not need a home: %v", goos, err)
		}
	}
}

func TestFallbackPerOS(t *testing.T) {
	appSupport := fakeHome("Library", "Application Support")
	tests := []struct {
		goos    string
		vars    map[string]string
		config  string
		twt     string
		state   string
		runtime string
	}{
		{
			goos:    "linux",
			config:  fakeHome(".config", "ttv-tui"),
			twt:     fakeHome(".config", "twt"),
			state:   fakeHome(".local", "state", "ttv-tui"),
			runtime: filepath.Join("/fake/tmp", "ttv-tui"),
		},
		{
			goos:    "freebsd",
			config:  fakeHome(".config", "ttv-tui"),
			twt:     fakeHome(".config", "twt"),
			state:   fakeHome(".local", "state", "ttv-tui"),
			runtime: filepath.Join("/fake/tmp", "ttv-tui"),
		},
		{
			goos:    "darwin",
			vars:    map[string]string{"TMPDIR": "/var/folders/ab/T"},
			config:  filepath.Join(appSupport, "ttv-tui"),
			twt:     filepath.Join(appSupport, "twt"),
			state:   filepath.Join(appSupport, "ttv-tui", "state"),
			runtime: filepath.Join("/var/folders/ab/T", "ttv-tui"),
		},
		{
			goos:    "windows",
			vars:    map[string]string{"APPDATA": `/win/roaming`, "LOCALAPPDATA": `/win/local`},
			config:  filepath.Join("/win/roaming", "ttv-tui"),
			twt:     filepath.Join("/win/roaming", "twt"),
			state:   filepath.Join("/win/local", "ttv-tui"),
			runtime: filepath.Join("/win/local", "ttv-tui", "run"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.goos, func(t *testing.T) {
			e := fakeEnv(tc.goos, tc.vars)
			if got, err := e.configDir(); err != nil || got != tc.config {
				t.Errorf("configDir = %q, %v; want %q", got, err, tc.config)
			}
			if got, err := e.twtDir(); err != nil || got != tc.twt {
				t.Errorf("twtDir = %q, %v; want %q", got, err, tc.twt)
			}
			if got, err := e.stateDir(); err != nil || got != tc.state {
				t.Errorf("stateDir = %q, %v; want %q", got, err, tc.state)
			}
			if got := e.runtimeDir(); got != tc.runtime {
				t.Errorf("runtimeDir = %q; want %q", got, tc.runtime)
			}
		})
	}
}

func TestEmptyXDGVarsAreIgnored(t *testing.T) {
	vars := map[string]string{"XDG_CONFIG_HOME": "", "XDG_STATE_HOME": "", "XDG_RUNTIME_DIR": ""}
	e := fakeEnv("linux", vars)
	if got, _ := e.configDir(); got != fakeHome(".config", "ttv-tui") {
		t.Errorf("configDir = %q", got)
	}
	if got, _ := e.stateDir(); got != fakeHome(".local", "state", "ttv-tui") {
		t.Errorf("stateDir = %q", got)
	}
	if got := e.runtimeDir(); got != filepath.Join("/fake/tmp", "ttv-tui") {
		t.Errorf("runtimeDir = %q", got)
	}
}

func TestWindowsFallsBackToHomeWhenKnownFolderVarsAreEmpty(t *testing.T) {
	e := fakeEnv("windows", nil)
	if got, err := e.configDir(); err != nil || got != fakeHome("AppData", "Roaming", "ttv-tui") {
		t.Errorf("configDir = %q, %v", got, err)
	}
	if got, err := e.stateDir(); err != nil || got != fakeHome("AppData", "Local", "ttv-tui") {
		t.Errorf("stateDir = %q, %v", got, err)
	}
	if got := e.runtimeDir(); got != fakeHome("AppData", "Local", "ttv-tui", "run") {
		t.Errorf("runtimeDir = %q", got)
	}
}

func TestDarwinRuntimeWithoutTMPDIRUsesTempDir(t *testing.T) {
	e := fakeEnv("darwin", nil)
	if got := e.runtimeDir(); got != filepath.Join("/fake/tmp", "ttv-tui") {
		t.Errorf("runtimeDir = %q", got)
	}
}

func TestRuntimeDirNeverFailsWithoutHome(t *testing.T) {
	e := fakeEnv("windows", nil)
	e.home = func() (string, error) { return "", errors.New("no home") }
	if got := e.runtimeDir(); got != filepath.Join("/fake/tmp", "ttv-tui") {
		t.Errorf("runtimeDir = %q", got)
	}
}

func TestMissingHomeFailsWithoutXDG(t *testing.T) {
	for _, goos := range allGOOS {
		e := fakeEnv(goos, nil)
		e.home = func() (string, error) { return "", errors.New("no home") }
		if _, err := e.configDir(); err == nil && goos != "windows" {
			t.Errorf("%s: configDir should fail without a home", goos)
		}
		if _, err := e.stateDir(); err == nil && goos != "windows" {
			t.Errorf("%s: stateDir should fail without a home", goos)
		}
	}
}

// The public functions read the real process environment; with the XDG
// variables pointing at a temp dir they never reach the operator's real
// ~/.config or ~/.local/state, whatever OS the suite runs on.
func TestPublicDefaultsHonourXDG(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(base, "state"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(base, "run"))

	if got, err := DefaultPath(); err != nil || got != filepath.Join(base, "config", "ttv-tui", "config.toml") {
		t.Errorf("DefaultPath = %q, %v", got, err)
	}
	if got, err := DefaultTwtPath(); err != nil || got != filepath.Join(base, "config", "twt", "config.toml") {
		t.Errorf("DefaultTwtPath = %q, %v", got, err)
	}
	if got, err := DefaultStateDir(); err != nil || got != filepath.Join(base, "state", "ttv-tui") {
		t.Errorf("DefaultStateDir = %q, %v", got, err)
	}
	if got := RuntimeDir(); got != filepath.Join(base, "run", "ttv-tui") {
		t.Errorf("RuntimeDir = %q", got)
	}
}
