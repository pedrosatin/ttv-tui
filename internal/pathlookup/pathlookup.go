// Package pathlookup finds executables on PATH without ever looking in the
// directory the app was started from.
package pathlookup

import (
	"os"
	"os/exec"
	"path/filepath"
)

// Abs finds file on PATH like exec.LookPath, but only in absolute directories.
// An empty entry, "." or any other relative entry names the directory the user
// started the app from; on Windows CreateProcess also looks there first.
// Skipping them keeps an executable planted in that directory out of the player
// chain. Unlike exec.LookPath it never returns exec.ErrDot: a relative entry is
// not a match, so the search goes on to the absolute ones.
func Abs(file string) (string, error) {
	return AbsIn(file, os.Getenv("PATH"))
}

// AbsIn is Abs over an explicit PATH value.
func AbsIn(file, path string) (string, error) {
	for _, dir := range filepath.SplitList(path) {
		if !filepath.IsAbs(dir) {
			continue
		}
		// With a separator in the name, LookPath checks that one path, adding
		// the PATHEXT extensions on Windows, and searches nothing else.
		if found, err := exec.LookPath(filepath.Join(dir, file)); err == nil {
			return found, nil
		}
	}
	return "", &exec.Error{Name: file, Err: exec.ErrNotFound}
}
