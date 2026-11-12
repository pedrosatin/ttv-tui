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
	// FailureUnknown: an error without a recognized cause.
	FailureUnknown Failure = "unknown"
)

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
	switch {
	case l == "":
		return FailureNone
	case strings.Contains(l, "no playable streams found"),
		strings.Contains(l, "this stream is offline"),
		strings.Contains(l, "channel is offline"):
		return FailureOffline
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
	default:
		return FailureNone
	}
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
