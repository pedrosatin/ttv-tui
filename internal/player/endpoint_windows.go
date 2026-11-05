//go:build windows

package player

import (
	"crypto/rand"
	"os"
)

// checkRuntimeDirFits always accepts: a named pipe is not a path inside the
// directory, so the directory's length does not limit the endpoint.
func checkRuntimeDirFits(string, uint64) error {
	return nil
}

// endpointInDir is the Windows endpoint: only the name of a named pipe, with
// no `\\.\pipe\` prefix and no separators (mpv adds the prefix; streamlink's
// shlex would eat the backslashes). The pipe is not created here and does not
// live in dir; dir only lends its unique name.
//
// Pipe names are global to the machine and a fallback directory's name is
// predictable, so a random suffix keeps a reused PID from reaching a player an
// earlier process left behind. rand.Text is base32: letters and digits only,
// which shlex leaves alone.
func endpointInDir(dir string) (string, error) {
	return pipeName(dir, os.Getpid()) + "-" + rand.Text()[:pipeSuffixLen], nil
}

// pipeSuffixLen is 40 bits of rand.Text.
const pipeSuffixLen = 8

// removeEndpoint does nothing: a named pipe disappears with its last handle and
// leaves no file to delete.
func removeEndpoint(string) {}
