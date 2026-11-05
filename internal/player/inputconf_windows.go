//go:build windows

package player

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/pedrosatin/ttv-tui/internal/config"
)

// checkDirPrivacy accepts dir only if it sits inside %LOCALAPPDATA% with no
// reparse point between that folder and dir.
//
// Mode bits say nothing here. Go derives them from the read-only attribute, so
// every directory reads as 0777, and Mkdir(0o700) and Chmod do not touch who can
// open it. Who can is decided by the ACL, and a new directory inherits it from
// its parent. Windows creates the user's profile, %LOCALAPPDATA% included, with
// an ACL for that user, SYSTEM and Administrators only, so a directory created
// inside it is out of reach of other unprivileged users.
//
// That inheritance is assumed, not read: this does not parse the ACL, and an
// operator who loosened the profile's ACL by hand is not detected. What it does
// prove is that the path stays inside the profile. Anything else (a TEMP moved to
// C:\Temp, an XDG_RUNTIME_DIR elsewhere) is refused: the session plays without
// keys in the mpv window and Event.InputConfFailed says so.
//
// The walk compares file identity instead of path text because the same folder
// has several spellings: on GitHub's runners TEMP is C:\Users\RUNNER~1\... while
// LOCALAPPDATA is C:\Users\runneradmin\....
func checkDirPrivacy(dir string, _ os.FileInfo) error {
	local, err := config.LocalAppData()
	if err != nil {
		return fmt.Errorf("player: session directory %q: %w", dir, err)
	}
	// Both identities: a moved AppData\Local is a junction at the usual path, and
	// dir may be spelled through either side of it.
	root, err := os.Stat(local)
	if err != nil {
		return fmt.Errorf("player: session directory %q: %w", dir, err)
	}
	rootLink, err := os.Lstat(local)
	if err != nil {
		return fmt.Errorf("player: session directory %q: %w", dir, err)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("player: session directory %q: %w", dir, err)
	}
	for current := abs; ; {
		fi, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("player: session directory %q: %w", dir, err)
		}
		if os.SameFile(fi, root) || os.SameFile(fi, rootLink) {
			return nil
		}
		if isReparsePoint(fi) {
			return fmt.Errorf("player: session directory %q goes through reparse point %q", dir, current)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return fmt.Errorf("player: session directory %q is outside %q", dir, local)
		}
		current = parent
	}
}

// checkInputConfTarget refuses a reparse point at path instead of replacing it.
// A session directory starts empty and only this user can write to it, so one
// there means something is wrong with the directory. A plain leftover file is
// fine: writeInputConf removes it.
func checkInputConfTarget(path string) error {
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("player: inspect input.conf: %w", err)
	}
	if isReparsePoint(fi) {
		return fmt.Errorf("player: input.conf %q is a reparse point", path)
	}
	return nil
}

// isReparsePoint reads the attribute itself: it covers symlinks, junctions and
// any other tag, while the mode bits Go derives from it depend on the tag.
func isReparsePoint(fi os.FileInfo) bool {
	attrs, ok := fi.Sys().(*syscall.Win32FileAttributeData)
	return !ok || attrs.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
}
