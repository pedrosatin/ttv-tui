//go:build !windows

package app

import (
	"os"

	"golang.org/x/sys/unix"
)

// cellSize reports the terminal cell in pixels, or zeros when the terminal does
// not fill in the pixel fields of its window size.
func cellSize(f *os.File) (width, height int) {
	ws, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws.Col == 0 || ws.Row == 0 {
		return 0, 0
	}
	return int(ws.Xpixel) / int(ws.Col), int(ws.Ypixel) / int(ws.Row)
}
