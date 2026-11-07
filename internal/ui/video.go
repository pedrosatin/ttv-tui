package ui

import "strings"

// WithVideo enables the experimental layout only when both probes succeeded.
// reason is a localized catalog key; empty means supported.
func WithVideo(requested bool, reason string) Option {
	return func(m *Model) { m.videoRequested, m.videoReason = requested, reason }
}

func (m Model) videoRows(available int) int { return min(18, max(available-6, 0)) }

func (m Model) videoView(available int, overlay []string, status string) string {
	rows := m.videoRows(available)
	lines := make([]string, rows)
	if rows > 0 {
		lines[0] = m.t("video.placeholder")
	}
	lines = append(lines, m.renderChatLines(available-rows, m.width)...)
	for len(lines) < available {
		lines = append(lines, "")
	}
	lines = lines[:available]
	for i := range lines {
		lines[i] = padOrTruncateVisual(lines[i], m.width)
	}
	lines = m.overlayHelpLines(lines, overlay, m.width, available)
	return strings.Join(lines, "\n") + "\n" + status
}
