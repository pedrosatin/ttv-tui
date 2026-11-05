// Package doctor reports local prerequisites without starting the TUI or player.
package doctor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/config"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

// Options selects the version, language and configuration to inspect.
type Options struct {
	Version    string
	Lang       string
	ConfigPath string
}

// Run prints a read-only report. Missing tools and unreadable configuration or
// state return 1. A token's presence says nothing about its validity on Twitch.
func Run(out io.Writer, opts Options) int {
	cat, _ := i18n.New(opts.Lang)
	code := 0
	line := func(key, value string) { _, _ = fmt.Fprintf(out, "%s: %s\n", cat.T(key), value) }
	line("doctor.version", opts.Version)
	line("doctor.platform", runtime.GOOS+"/"+runtime.GOARCH)
	for _, tool := range []string{"streamlink", "mpv"} {
		value, ok := toolVersion(tool, cat)
		_, _ = fmt.Fprintf(out, "%s: %s\n", tool, value)
		if !ok {
			code = 1
		}
	}
	path := opts.ConfigPath
	if path == "" {
		path, _ = config.DefaultPath()
	}
	if path == "" {
		line("doctor.config", cat.T("doctor.unavailable"))
		code = 1
	} else {
		line("doctor.config", filepath.Dir(path))
	}
	cfg, err := config.LoadFrom(path)
	token := cat.T("doctor.no")
	if err != nil || path == "" {
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
// Do not call FindOrphans here: that operation deletes stale records and files.
func registryStatus(dir string) string {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return "doctor.clean"
	}
	if err != nil {
		return "doctor.unreadable"
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "player-") && strings.HasSuffix(e.Name(), ".json") {
			return "doctor.pending"
		}
	}
	return "doctor.clean"
}

var versionPattern = regexp.MustCompile(`(?m)^(?:mpv v?|streamlink )?([0-9]+\.[0-9]+(?:\.[0-9]+)?)\b`)

func toolVersion(tool string, cat *i18n.Catalog) (string, bool) {
	path, err := exec.LookPath(tool)
	if err != nil {
		return cat.T("doctor.missing"), false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
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
