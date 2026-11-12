// Package doctor reports local prerequisites without starting the TUI or player.
package doctor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/components"
	"github.com/pedrosatin/ttv-tui/internal/config"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/player"
)

// Options selects the version, language and configuration to inspect.
type Options struct {
	Version    string
	Lang       string
	ConfigPath string
	ExeDir     string
	GOOS       string
}

// Run prints a read-only report. Missing tools and unreadable configuration or
// state return 1. A token's presence says nothing about its validity on Twitch.
func Run(out io.Writer, opts Options) int {
	cat, _ := i18n.New(opts.Lang)
	code := 0
	line := func(key, value string) { _, _ = fmt.Fprintf(out, "%s: %s\n", cat.T(key), value) }
	line("doctor.version", opts.Version)
	line("doctor.platform", runtime.GOOS+"/"+runtime.GOARCH)
	path := opts.ConfigPath
	if path == "" {
		path, _ = config.DefaultPath()
	}
	cfg, cfgErr := config.LoadFrom(path)

	streamlinkVal, streamlinkOK := streamlinkStatus(cfg.StreamlinkPath, cat)
	_, _ = fmt.Fprintf(out, "streamlink: %s\n", streamlinkVal)
	if !streamlinkOK {
		code = 1
	}
	if previous, lastCheck, managed := managedUpdateInfo(cfg.StreamlinkPath, cat); managed {
		line("doctor.streamlink_previous", previous)
		line("doctor.streamlink_last_check", lastCheck)
	}

	mpvVal, mpvOK := mpvStatus(opts, cat)
	_, _ = fmt.Fprintf(out, "mpv: %s\n", mpvVal)
	if !mpvOK {
		code = 1
	}

	if path == "" {
		line("doctor.config", cat.T("doctor.unavailable"))
		code = 1
	} else {
		line("doctor.config", filepath.Dir(path))
	}
	token := cat.T("doctor.no")
	if cfgErr != nil || path == "" {
		token = cat.T("doctor.unreadable")
		code = 1
	} else if cfg.HasToken() {
		token = cat.T("doctor.yes")
	}
	line("doctor.token", token)
	state, err := config.DefaultStateDir()
	if err != nil {
		line("doctor.state", cat.T("doctor.unavailable"))
		code = 1
	} else {
		line("doctor.state", state)
	}
	line("doctor.runtime", config.RuntimeDir())
	status := "doctor.unreadable"
	if err == nil {
		status = registryStatus(filepath.Join(state, "players"))
	}
	if status == "doctor.unreadable" {
		code = 1
	}
	line("doctor.registry", cat.T(status))
	_, _ = fmt.Fprintln(out, cat.T("doctor.scope"))
	return code
}

// Records disappear after cleanup, so the registry cannot prove past IPC health.
func registryStatus(dir string) string {
	pending, err := player.RegistryPending(dir)
	if err != nil {
		return "doctor.unreadable"
	}
	if pending {
		return "doctor.pending"
	}
	return "doctor.clean"
}

var versionPattern = regexp.MustCompile(`(?m)^(?:mpv v?|streamlink )?([0-9]+\.[0-9]+(?:\.[0-9]+)?)\b`)

func streamlinkStatus(cfgStreamlink string, cat *i18n.Catalog) (string, bool) {
	resolver := components.NewResolver()
	resolver.LookPath = exec.LookPath
	res, err := resolver.ResolveStreamlink(cfgStreamlink)
	if err != nil {
		return cat.T("doctor.missing"), false
	}
	ver, ok := toolVersionAtPath(res.Path, res.Env, cat)
	if !ok {
		return ver, false
	}
	originLabel := string(res.Origin)
	switch res.Origin {
	case components.OriginConfig:
		originLabel = cat.T("doctor.origin.config")
	case components.OriginManaged:
		originLabel = cat.T("doctor.origin.managed")
	case components.OriginPath:
		originLabel = cat.T("doctor.origin.path")
	}
	return fmt.Sprintf("%s (%s)", ver, originLabel), true
}

// managedUpdateInfo describes the update side of a managed streamlink: the
// version kept to go back to and when the last check for a newer one happened.
// It reports managed=false for a streamlink from the config or PATH, which the
// app never updates. Local files only, nothing is written.
func managedUpdateInfo(cfgStreamlink string, cat *i18n.Catalog) (previous, lastCheck string, managed bool) {
	resolver := components.NewResolver()
	resolver.LookPath = exec.LookPath
	res, err := resolver.ResolveStreamlink(cfgStreamlink)
	if err != nil || res.Origin != components.OriginManaged {
		return "", "", false
	}
	previous = cat.T("doctor.none")
	if _, prev := resolver.ManagedVersions(); prev != "" {
		previous = prev
	}
	lastCheck = cat.T("doctor.never")
	if checked := components.LoadUpdateState(resolver.DataDir).LastCheck; !checked.IsZero() {
		lastCheck = checked.Local().Format("2006-01-02 15:04")
	}
	return previous, lastCheck, true
}

func mpvStatus(opts Options, cat *i18n.Catalog) (string, bool) {
	goos := opts.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	if _, ok := findWindowMpv(opts.ExeDir, goos); ok {
		status := fmt.Sprintf("libmpv (%s)", cat.T("doctor.origin.window"))
		// The window plays without mpv, but --terminal, or a window whose probe
		// fails, falls back to the mpv executable: say so instead of hiding it.
		if version, found := toolVersion("mpv", cat); found {
			return status + "; mpv " + version, true
		}
		return status + "; " + cat.T("doctor.window.terminal_needs_mpv"), true
	}
	return toolVersion("mpv", cat)
}

func findWindowMpv(dir, goos string) (string, bool) {
	if goos != "windows" {
		return "", false
	}
	if dir == "" {
		if exe, err := os.Executable(); err == nil {
			dir = filepath.Dir(exe)
		}
	}
	candidates := []string{
		filepath.Join(dir, "ttv-tui-window.exe"),
		filepath.Join(dir, "ttv-tui-window", "ttv-tui-window.exe"),
	}
	for _, candidate := range candidates {
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			dll := filepath.Join(filepath.Dir(candidate), "libmpv-2.dll")
			if stDLL, errDLL := os.Stat(dll); errDLL == nil && !stDLL.IsDir() {
				return candidate, true
			}
		}
	}
	return "", false
}

func toolVersion(tool string, cat *i18n.Catalog) (string, bool) {
	path, err := exec.LookPath(tool)
	if err != nil {
		return cat.T("doctor.missing"), false
	}
	return toolVersionAtPath(path, nil, cat)
}

func toolVersionAtPath(path string, env []string, cat *i18n.Catalog) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	if len(env) > 0 {
		cmd.Env = append(cmd.Environ(), env...)
	}
	cmd.WaitDelay = 100 * time.Millisecond
	var output cappedOutput
	cmd.Stdout = &output
	// Discard stderr and accept only a numeric version, never arbitrary tool logs.
	if err := cmd.Run(); err != nil {
		return cat.T("doctor.version_failed"), true
	}
	match := versionPattern.FindSubmatch(output.Bytes())
	if len(match) < 2 {
		return cat.T("doctor.version_failed"), true
	}
	return string(match[1]), true
}

type cappedOutput struct{ bytes.Buffer }

func (b *cappedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if remaining := 4096 - b.Len(); remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = b.Buffer.Write(p)
	}
	return n, nil
}
