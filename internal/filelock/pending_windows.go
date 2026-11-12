//go:build windows

package filelock

import (
	"errors"

	"golang.org/x/sys/windows"
)

// isDeletePending reports a lock file that is being deleted. A file deleted while
// another handle is still open (antivirus, indexer, a competing process) stays in
// "delete pending" until that handle closes, and both CreateFile(CREATE_NEW) and
// GetFileAttributesEx fail with ERROR_ACCESS_DENIED meanwhile. A real permission
// error looks the same; it surfaces after DeletePendingWait.
func isDeletePending(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED)
}
