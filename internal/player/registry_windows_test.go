package player

// processAlive asks about one process, through the same OpenProcess check the
// production code uses. It sees neither descendants nor a PID reused by a stranger.
func processAlive(pid int) bool {
	return recordedGroup(pid).alive()
}
