package player

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Failure classifies why playback could not start or stopped with an error.
// The UI turns each class into a localized message with the next step; the
// original streamlink/mpv line stays in Event.LastLine as the technical detail.
type Failure string

const (
	// FailureNone means no classified failure (normal states, or no evidence).
	FailureNone Failure = ""
	// FailureOffline: streamlink found no playable stream (channel offline or
	// inexistent). Retrying right away is pointless; pick another channel.
	FailureOffline Failure = "offline"
	// FailureNetwork: connection refused, DNS, proxy, TLS or unreachable network.
	FailureNetwork Failure = "network"
	// FailureTimeout: a connection or read timed out, or mpv never opened its
	// IPC socket within SocketWait.
	FailureTimeout Failure = "timeout"
	// FailureStreamlinkMissing: the streamlink executable is not installed.
	FailureStreamlinkMissing Failure = "streamlink_missing"
	// FailureMPVMissing: the mpv executable is not installed.
	FailureMPVMissing Failure = "mpv_missing"
	// FailureNoPlugin: streamlink has no plugin capable of handling the URL.
	FailureNoPlugin Failure = "no_plugin"
	// FailureBlocked: the site answered HTTP 403 to streamlink, as an anti-bot
	// wall does. The connection worked; the request was refused.
	FailureBlocked Failure = "blocked"
	// FailureStreamlinkBroken: this streamlink build failed on its own, not
	// because of the channel or the network: a plugin or Python runtime error,
	// a binary that cannot run, or an exit with an error status before any
	// stream reached the player. It is the only class that counts against a
	// freshly updated managed streamlink (see BlamesStreamlink).
	FailureStreamlinkBroken Failure = "streamlink_broken"
	// FailureUnknown: an error without a recognized cause.
	FailureUnknown Failure = "unknown"
)

// BlamesStreamlink reports whether f is evidence that the streamlink build in
// use is broken. Offline channels, network and timeout failures, a URL no plugin
// handles, a request the site refused, missing tools and unclassified errors say
// nothing about the build, so
// they never count: an update must not be undone because the channel is offline
// or the network is down.
func (f Failure) BlamesStreamlink() bool {
	return f == FailureStreamlinkBroken
}

// MissingToolError reports a required executable that is not on PATH.
type MissingToolError struct {
	Tool string
	Err  error
}

func (e *MissingToolError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("player: %s not found: %v", e.Tool, e.Err)
	}
	return fmt.Sprintf("player: %s not found", e.Tool)
}

func (e *MissingToolError) Unwrap() error { return e.Err }

// failureForTool maps an executable name to its missing-tool class.
func failureForTool(tool string) Failure {
	base := filepath.Base(tool)
	if i := strings.LastIndex(base, `\`); i >= 0 {
		base = base[i+1:]
	}
	name := strings.TrimSuffix(strings.TrimSuffix(strings.ToLower(base), ".exe"), ".com")
	if name == "mpv" {
		return FailureMPVMissing
	}
	return FailureStreamlinkMissing
}

// FailureOf classifies an error returned by Manager.Start.
func FailureOf(err error) Failure {
	if err == nil {
		return FailureNone
	}
	var missing *MissingToolError
	if errors.As(err, &missing) {
		return failureForTool(missing.Tool)
	}
	var execErr *exec.Error
	if errors.As(err, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound) {
		return failureForTool(execErr.Name)
	}
	if f := ClassifyLine(err.Error()); f != FailureNone {
		return f
	}
	return FailureUnknown
}

// ClassifyLine maps one streamlink/mpv output line to a failure class, or
// FailureNone when the line proves nothing. Timeouts are checked before network
// errors because urllib3 reports a connect timeout inside a generic
// "Max retries exceeded" connection error.
func ClassifyLine(line string) Failure {
	l := strings.ToLower(line)
	// A 404 or 403 on one segment or on one playlist reload happens during
	// normal playback (a prefetch segment that expired, a reload racing the
	// end of an ad) and streamlink keeps going. Only the same status on the
	// channel or its playlist says something about the channel.
	perSegment := isPerSegmentLine(l)
	switch {
	case l == "":
		return FailureNone
	case strings.Contains(l, "no playable streams found"),
		strings.Contains(l, "this stream is offline"),
		strings.Contains(l, "channel is offline"),
		// The site answered that the channel or its playlist does not exist: a
		// channel the API still lists as live right after it ended, or a wrong
		// name. The connection worked, so this is not a network error.
		!perSegment && strings.Contains(l, "404 client error"):
		return FailureOffline
	case !perSegment && strings.Contains(l, "403 client error"):
		// Before the network cases: streamlink reports it inside "Unable to open URL".
		return FailureBlocked
	case strings.Contains(l, "failed to start player") && strings.Contains(l, "mpv"),
		strings.Contains(l, "mpv: command not found"),
		strings.Contains(l, "mpv: not found"):
		return FailureMPVMissing
	case strings.Contains(l, "streamlink: command not found"),
		strings.Contains(l, "streamlink: not found"):
		return FailureStreamlinkMissing
	case strings.Contains(l, "no plugin can handle url"):
		return FailureNoPlugin
	case strings.Contains(l, "timed out"),
		strings.Contains(l, "timeouterror"),
		strings.Contains(l, "timeout"):
		return FailureTimeout
	case strings.Contains(l, "unable to open url"),
		strings.Contains(l, "connectionerror"),
		strings.Contains(l, "connection refused"),
		strings.Contains(l, "connection reset"),
		strings.Contains(l, "failed to establish a new connection"),
		strings.Contains(l, "max retries exceeded"),
		strings.Contains(l, "name or service not known"),
		strings.Contains(l, "temporary failure in name resolution"),
		strings.Contains(l, "failed to resolve"),
		strings.Contains(l, "network is unreachable"),
		strings.Contains(l, "proxyerror"),
		strings.Contains(l, "sslerror"):
		return FailureNetwork
	case strings.Contains(l, "pluginerror"),
		strings.Contains(l, "plugin error"),
		strings.Contains(l, "unable to validate"),
		strings.Contains(l, "modulenotfounderror"),
		strings.Contains(l, "importerror"),
		strings.Contains(l, "syntaxerror"),
		strings.Contains(l, "attributeerror"),
		strings.Contains(l, "typeerror"),
		strings.Contains(l, "keyerror"),
		strings.Contains(l, "fatal python error"),
		strings.Contains(l, "error while loading shared libraries"),
		strings.Contains(l, "exec format error"):
		// Last on purpose: a plugin failure caused by the network or an
		// offline channel is reported as such by the cases above.
		return FailureStreamlinkBroken
	default:
		return FailureNone
	}
}

// isPerSegmentLine reports the streamlink HLS lines about a single segment,
// its map or one playlist reload, which the stream survives
// (stream/hls/hls.py in streamlink 8.6).
func isPerSegmentLine(l string) bool {
	return strings.Contains(l, "failed to fetch segment") ||
		strings.Contains(l, "failed to fetch map for segment") ||
		strings.Contains(l, "download of segment") ||
		strings.Contains(l, "reloading failed")
}

// isFatalLine reports lines after which streamlink cannot recover: playback
// will not start, so the session ends right away instead of waiting for the
// process or the IPC socket deadline.
func isFatalLine(f Failure) bool {
	switch f {
	case FailureOffline, FailureMPVMissing, FailureStreamlinkMissing, FailureNoPlugin:
		return true
	default:
		return false
	}
}

// classifyTerminal picks the failure for an error transition: the line-based
// class when one is recognized, otherwise fallback.
func classifyTerminal(line string, fallback Failure) Failure {
	if f := ClassifyLine(line); f != FailureNone {
		return f
	}
	if fallback != FailureNone {
		return fallback
	}
	return FailureUnknown
}
