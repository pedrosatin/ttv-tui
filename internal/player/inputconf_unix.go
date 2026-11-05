//go:build unix

package player

import (
	"fmt"
	"os"
	"syscall"
)

// checkDirPrivacy accepts only a directory owned by this user with no group or
// other permission bits.
func checkDirPrivacy(dir string, fi os.FileInfo) error {
	if fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("player: session directory %q is not private (mode %o)", dir, fi.Mode().Perm())
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Geteuid() {
		return fmt.Errorf("player: session directory %q has another owner", dir)
	}
	return nil
}
