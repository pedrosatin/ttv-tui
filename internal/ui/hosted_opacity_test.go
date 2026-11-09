package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHostedOpacityKeysAdjustTheOverlay(t *testing.T) {
	m := hostedModel(t, 212, 49, true)
	m.keys = defaultKeyMap()
	m.hosted.chatOverlay = true
	if handled, _ := m.handleHostedResizeKey(tea.KeyMsg{Type: tea.KeyUp, Alt: true}); !handled || m.hostedPanel().OverlayOpacity != 75 {
		t.Fatalf("Alt+↑: handled %v, %+v", handled, m.hostedPanel())
	}
	if handled, _ := m.handleHostedResizeKey(tea.KeyMsg{Type: tea.KeyDown, Alt: true}); !handled || m.hostedPanel().OverlayOpacity != 70 {
		t.Fatalf("Alt+↓: handled %v, %+v", handled, m.hostedPanel())
	}
}

func TestHostedOpacityStaysInRange(t *testing.T) {
	m := hostedModel(t, 212, 49, true)
	m.hosted.chatOverlay = true
	for range 40 {
		m.adjustChatOpacity(hostedOpacityStep)
	}
	if got := m.chatOpacity(); got != 100 {
		t.Fatalf("máximo: %d", got)
	}
	for range 40 {
		m.adjustChatOpacity(-hostedOpacityStep)
	}
	if got := m.chatOpacity(); got != hostedMinOpacity {
		t.Fatalf("mínimo: %d", got)
	}
}

func TestHostedOpacityNeedsTheChatOverTheVideo(t *testing.T) {
	m := hostedModel(t, 212, 49, true)
	if cmd := m.adjustChatOpacity(hostedOpacityStep); cmd != nil || !m.notices.statusWarns || m.hosted.chatOverlayOpacity != 0 {
		t.Fatalf("chat ao lado: notice %q, opacidade %d", m.notices.status, m.hosted.chatOverlayOpacity)
	}
}

func TestHostedOpacityIsSaved(t *testing.T) {
	var got int
	m := hostedModel(t, 212, 49, true)
	m.hosted.chatOverlay = true
	m.hosted.persist = func(_, _ int, _, _ bool, opacity int) error {
		got = opacity
		return nil
	}
	m.adjustChatOpacity(hostedOpacityStep)
	m.saveHostedColumns(m.hosted.saveGen)()
	if got != 75 {
		t.Fatalf("salvou %d", got)
	}
}
