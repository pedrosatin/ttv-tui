//go:build windows

package player

import (
	"errors"
	"fmt"
	"os"
)

// checkDirPrivacy always refuses. Privacy on Windows lives in the directory's
// ACL, and reading it takes an API outside the standard library; the mode bits Go
// reports mean nothing here. input.conf can run arbitrary commands, so an
// unproven directory is a refusal: the session plays, with no keys in the mpv
// window, and Event.InputConfFailed says so.
//
// STUB: replace with a real owner and ACL check once x/sys/windows is in the
// module (v1.8 tickets 08/09).
func checkDirPrivacy(dir string, _ os.FileInfo) error {
	return fmt.Errorf("player: session directory %q: privacy check: %w", dir, errors.ErrUnsupported)
}
