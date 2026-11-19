package player

import (
	"os"
	"os/exec"
	"path/filepath"
)

// PreviewQuality is the streamlink quality for a preview session: the lowest
// available rung on Twitch and Kick.
const PreviewQuality = "worst"

// PreviewAutofit is the mpv size hint for a preview window outside the hosted
// single window. The compositor still places the window (ADR 0006).
const PreviewAutofit = "480x270"

// previewSubdir nests the preview runtime and registry directories under the
// main ones, so the two managers never share a session path.
const previewSubdir = "preview"

// PreviewRegistryDir is where NewPreview keeps its registry records for a
// main registryDir; startup cleanup scans it for orphans too.
func PreviewRegistryDir(registryDir string) string {
	return filepath.Join(registryDir, previewSubdir)
}

// NewPreview builds a Manager with the preview profile: lowest quality, muted
// mpv, no input.conf, a small-window hint, and no focus steal when mpv accepts
// --focus-on=never. Generations are independent of the main Manager. Runtime
// and registry directories nest under a "preview" subdirectory so two managers
// never share a session path.
func NewPreview(opts Options) *Manager {
	opts.Preview = true
	if opts.RuntimeDir == "" {
		opts.RuntimeDir = filepath.Join(defaultRuntimeDir(), previewSubdir)
	} else {
		opts.RuntimeDir = filepath.Join(opts.RuntimeDir, previewSubdir)
	}
	if opts.RegistryDir != "" {
		opts.RegistryDir = PreviewRegistryDir(opts.RegistryDir)
	}
	// Preview never needs mpv key bindings: skip writing input.conf entirely.
	if opts.WriteInputConf == nil {
		opts.WriteInputConf = func(string) error { return nil }
	}
	return New(opts)
}

// previewMPVArgs is the mpv argv for a preview: muted, small, no keys file, and
// no focus steal when the option exists.
func previewMPVArgs(endpoint string) []string {
	return []string{
		"--no-config",
		"--no-input-default-bindings",
		"--no-osc",
		"--osd-level=1",
		"--osd-bar=no",
		"--volume-max=100",
		"--input-ipc-server=" + endpoint,
		"--force-window=immediate",
		"--hwdec=auto-safe",
		"--mute=yes",
		"--autofit=" + PreviewAutofit,
		"--focus-on=never",
	}
}

// previewCommand builds the preview streamlink+mpv invocation: quality worst,
// muted small window, no input.conf.
func (s streamlinkInvocation) previewCommand(target Target, layout sessionLayout) *exec.Cmd {
	args := append([]string{"--no-config", "--loglevel", "info"}, sessionExtraArgs(target, layout)...)
	args = append(args,
		target.URL,
		PreviewQuality,
		"--player", s.mpv,
		"--player-args", buildPlayerArgs(previewMPVArgs(layout.Endpoint)),
	)
	cmd := exec.Command(s.bin, args...)
	cmd.Dir = layout.Dir
	if len(s.env) > 0 {
		cmd.Env = append(os.Environ(), s.env...)
	}
	return cmd
}
