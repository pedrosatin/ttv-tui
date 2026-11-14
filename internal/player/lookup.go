package player

import (
	"os"
	"os/exec"
	"path/filepath"
)

// lookAbsPath finds file on PATH like exec.LookPath, but only in absolute
// directories. An empty entry, "." or any other relative entry names the
// directory the user started the app from; on Windows CreateProcess also looks
// there first. Skipping them keeps an executable planted in that directory out
// of the player chain.
func lookAbsPath(file string) (string, error) {
	return lookAbsPathIn(file, os.Getenv("PATH"))
}

// lookAbsPathIn is lookAbsPath over an explicit PATH value.
func lookAbsPathIn(file, path string) (string, error) {
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
