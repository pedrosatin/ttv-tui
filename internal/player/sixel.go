package player

import (
	"context"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// SixelProbe separates terminal transport and tool execution for deterministic tests.
type SixelProbe struct {
	Terminal func(context.Context) ([]byte, error)
	Command  func(context.Context, string, ...string) ([]byte, error)
}

// DetectSixel returns a catalog key on failure and an empty string on success.
// A missing or invalid DA1 reply is never evidence of support.
func DetectSixel(ctx context.Context, probe SixelProbe) string {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if probe.Terminal == nil {
		return "video.no_terminal"
	}
	reply, err := probe.Terminal(ctx)
	if err != nil || !terminalSixel(reply) {
		return "video.no_terminal"
	}
	run := probe.Command
	if run == nil {
		run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).Output()
		}
	}
	out, err := run(ctx, "mpv", "--no-config", "--vo=help")
	if err != nil {
		return "video.no_mpv"
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == "sixel" {
			return ""
		}
	}
	return "video.no_mpv"
}

var da1Reply = regexp.MustCompile(`\x1b\[\?([0-9]+(?:;[0-9]+)*)c`)

func terminalSixel(reply []byte) bool {
	for _, match := range da1Reply.FindAllSubmatch([]byte(strings.ReplaceAll(string(reply), "\x9b", "\x1b[")), -1) {
		// The first parameter identifies the terminal class, not a capability.
		fields := strings.Split(string(match[1]), ";")
		for _, capability := range fields[1:] {
			if capability == "4" {
				return true
			}
		}
	}
	return false
}
