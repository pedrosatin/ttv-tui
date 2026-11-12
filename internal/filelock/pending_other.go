//go:build !windows

package filelock

// isDeletePending is always false here: removing a file frees its name at once.
func isDeletePending(error) bool { return false }
