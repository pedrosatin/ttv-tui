package player

import (
	"errors"
	"fmt"
)

// STUB: real detection arrives with the Job Object (v1.8 ticket 09). Until then
// both functions answer "cannot tell", never "gone": FindOrphans keeps every
// record it cannot judge and no orphan is detected or killed here.

func osGroupHasArg(int, string) (bool, error) {
	return false, fmt.Errorf("player: inspect process group on windows: %w", errors.ErrUnsupported)
}

func osProcStartTime(int) (uint64, error) {
	return 0, fmt.Errorf("player: read process start time on windows: %w", errors.ErrUnsupported)
}
