package player

import (
	"errors"
	"fmt"
)

// STUB: macOS has no /proc, and reading a process's start time and command line
// takes sysctl (v1.8 ticket 05). Until then both functions answer "cannot tell",
// never "gone": FindOrphans keeps every record it cannot judge, so a second
// instance does not delete the session of the first. No orphan is detected here.

func osGroupHasArg(int, string) (bool, error) {
	return false, fmt.Errorf("player: inspect process group on darwin: %w", errors.ErrUnsupported)
}

func osProcStartTime(int) (uint64, error) {
	return 0, fmt.Errorf("player: read process start time on darwin: %w", errors.ErrUnsupported)
}
