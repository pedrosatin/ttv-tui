package player

import "syscall"

// processAlive treats zombies as dead: they no longer run or play audio.
func processAlive(pid int) bool {
	if err := syscall.Kill(pid, 0); err != nil {
		return false
	}
	fields := procStatFields(pid)
	return len(fields) > 0 && fields[0] != "Z"
}
