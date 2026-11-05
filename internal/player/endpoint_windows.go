//go:build windows

package player

import "os"

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
// STUB: dialIPC still dials a Unix socket, so a session started on Windows
// cannot connect until v1.8 ticket 08 adds the pipe dial.
func endpointInDir(dir string) (string, error) {
	return pipeName(dir, os.Getpid()), nil
}

// removeEndpoint does nothing: a named pipe disappears with its last handle and
// leaves no file to delete.
func removeEndpoint(string) {}
