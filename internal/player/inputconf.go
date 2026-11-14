package player

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// inputConfName is the mpv input.conf written in the session directory.
const inputConfName = "mpv-input.conf"

// volumeOSD shows the new volume as a bare number. The add commands go through
// no-osd because mpv's own volume OSD is the English "Volume: 50%".
const volumeOSD = `show-text "${volume}%"`

// pauseOSD uses symbols shared by every app language.
const pauseText = "${?pause==yes:‖}${?pause==no:▶}"
const pauseOSD = `show-text "` + pauseText + `"`

// muteText also avoids words: a note with a cross when muted, the volume when not.
const muteText = "${?mute==yes:♪ ✕}${?mute==no:♪ ${volume}%}"
const muteOSD = `show-text "` + muteText + `"`

// inputBindings is every key the mpv window answers to, mirroring the TUI's
// player keys. mpv runs with --no-input-default-bindings, so a key left out does
// nothing. The window's close button does not depend on this list: mpv quits on
// CLOSE_WIN even with no binding for it.
var inputBindings = []struct{ key, command string }{
	{"SPACE", "no-osd cycle pause; " + pauseOSD},
	{"+", "no-osd add volume 5; " + volumeOSD},
	{"=", "no-osd add volume 5; " + volumeOSD},
	{"-", "no-osd add volume -5; " + volumeOSD},
	{"_", "no-osd add volume -5; " + volumeOSD},
	{"m", "no-osd cycle mute; " + muteOSD},
}

// inputConfContent returns the input.conf text, always the same bytes.
func inputConfContent() string {
	var b strings.Builder
	for _, binding := range inputBindings {
		fmt.Fprintf(&b, "%s %s\n", binding.key, binding.command)
	}
	return b.String()
}

// inputConfPath is where a session's input.conf lives: its private directory,
// which is removed with the session. It takes the directory, never the IPC
// endpoint: on Windows the endpoint is a pipe name, not a path inside it.
func inputConfPath(sessionDir string) string {
	return filepath.Join(sessionDir, inputConfName)
}

// writeInputConf creates the input.conf at path. input.conf can run arbitrary
// commands, and the temp fallback directory has a predictable name that may
// already exist, so it refuses a directory other users can write to and never
// writes through a file or symlink left at path. What "other users" and "left
// at path" mean is per OS: see checkDirPrivacy and checkInputConfTarget.
func writeInputConf(path string) error {
	if err := checkPrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	if err := checkInputConfTarget(path); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("player: replace input.conf: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("player: create input.conf: %w", err)
	}
	_, err = f.WriteString(inputConfContent())
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("player: write input.conf: %w", err)
	}
	return nil
}

// checkPrivateDir accepts only a real directory that no other user can reach.
func checkPrivateDir(dir string) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("player: session directory: %w", err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("player: session directory %q is not a directory", dir)
	}
	return checkDirPrivacy(dir, fi)
}
