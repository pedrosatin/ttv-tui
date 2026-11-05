package player

import (
	"errors"
	"fmt"
)

// A player cannot outlive its owner here: the job dies with the owner and takes
// the tree with it (see processGroup). So the registry is not a way to find
// players on Windows, only the list of what a dead session left on disk: its
// directory, its input.conf and the record itself.
//
// Nothing tries to prove who a process is. There is no /proc to read, and both
// functions below stop at "cannot tell", which FindOrphans treats as "keep": no
// orphan is ever reported or killed here. A record goes away only when its owner's
// PID and its player's PID are both gone. If Windows reused either one, the record
// waits for a later run, which costs a few small files and nothing else.

// osGroupHasArg cannot answer: reading another process's command line needs its
// PEB, and a PID alone would not make the answer about our player anyway.
func osGroupHasArg(int, string) (bool, error) {
	return false, fmt.Errorf("player: inspect process group on windows: %w", errors.ErrUnsupported)
}

// osProcStartTime only answers for a PID that is gone (0). Any running process may
// be the owner or a stranger that reused its PID, which is "cannot tell".
func osProcStartTime(pid int) (uint64, error) {
	running, err := pidRunning(pid)
	if err != nil {
		return 0, err
	}
	if !running {
		return 0, nil
	}
	return 0, fmt.Errorf("player: read process start time on windows: %w", errors.ErrUnsupported)
}
