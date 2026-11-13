//go:build unix && !linux && !darwin

package player

import (
	"errors"
	"fmt"
)

// This OS is not supported: the player runs, but the registry answers "cannot
// tell", never "gone". FindOrphans keeps every record it cannot judge, and no
// orphan is detected or killed here.

func osGroupHasEndpoint(int, string) (bool, error) {
	return false, fmt.Errorf("player: inspect process group: %w", errors.ErrUnsupported)
}

func osProcStartTime(int) (uint64, error) {
	return 0, fmt.Errorf("player: read process start time: %w", errors.ErrUnsupported)
}
