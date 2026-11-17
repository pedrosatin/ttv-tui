//go:build unix

package config

import (
	"os"
	"syscall"
)

// openNoFollow recusa symlink no último componente e não bloqueia em FIFO.
const openNoFollow = syscall.O_NOFOLLOW | syscall.O_NONBLOCK

func ownedByCurrentUser(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Geteuid()
}
