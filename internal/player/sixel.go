package player

import (
	"context"
	"os/exec"
	"regexp"
	"strconv"
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

var cellReply = regexp.MustCompile(`\x1b\[6;([0-9]{1,4});([0-9]{1,4})t`)

// ParseCellReply reads the answer to CSI 16 t, CSI 6 ; height ; width t. A terminal
// that answers with zeros knows no more than one that stays silent, so ok is false.
func ParseCellReply(reply []byte) (width, height int, ok bool) {
	match := cellReply.FindSubmatch([]byte(strings.ReplaceAll(string(reply), "\x9b", "\x1b[")))
	if match == nil {
		return 0, 0, false
	}
	height, _ = strconv.Atoi(string(match[1]))
	width, _ = strconv.Atoi(string(match[2]))
	if width == 0 || height == 0 {
		return 0, 0, false
	}
	return width, height, true
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
