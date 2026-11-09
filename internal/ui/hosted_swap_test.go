package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestHostedSwapMovesThePanels(t *testing.T) {
	m := hostedModel(t, 212, 49, true)
	m.togglePanelSides()
	if got, want := m.hostedPanel(), (HostPanel{Row: 1, Col: 48, Rows: 48, Cols: 122, Visible: true}); got != want {
		t.Fatalf("lados trocados: %+v, quero %+v", got, want)
	}
	m.togglePanelSides()
	if got, want := m.hostedPanel(), (HostPanel{Row: 1, Col: 44, Rows: 48, Cols: 122, Visible: true}); got != want {
		t.Fatalf("de volta: %+v, quero %+v", got, want)
	}
}

func TestHostedSwapPutsTheOverlayOnTheLeft(t *testing.T) {
	m := hostedModel(t, 212, 49, true)
	m.chatOverlay, m.panelsSwapped = true, true
	if got, want := m.hostedPanel(), (HostPanel{Row: 1, Col: 1, Rows: 48, Cols: 169, Visible: true, OverlayCols: 46, OverlayLeft: true, OverlayOpacity: 70}); got != want {
		t.Fatalf("sobreposto à esquerda: %+v, quero %+v", got, want)
	}
}

func TestHostedSwapKeepsTheArrowOnTheDivider(t *testing.T) {
	m := hostedModel(t, 212, 49, true)
	m.keys = defaultKeyMap()
	m.panelsSwapped = true
	if handled, _ := m.handleHostedResizeKey(tea.KeyMsg{Type: tea.KeyCtrlLeft}); !handled || m.hostedListCols != 44 {
		t.Fatalf("lista à direita: Ctrl+← alarga, cols %d", m.hostedListCols)
	}
	if handled, _ := m.handleHostedResizeKey(tea.KeyMsg{Type: tea.KeyRight, Alt: true}); !handled || m.hostedChatCols != 48 {
		t.Fatalf("chat à esquerda: Alt+→ alarga, cols %d", m.hostedChatCols)
	}
}

func TestHostedSwapKeepsEveryRowInsideTheWindow(t *testing.T) {
	for _, overlay := range []bool{false, true} {
		for _, width := range []int{80, 120, 212} {
			m := hostedModel(t, width, 30, true)
			m.chatOverlay, m.panelsSwapped = overlay, true
			for i, line := range strings.Split(m.frame(), "\n") {
				if got := ansi.StringWidth(line); got > width {
					t.Fatalf("overlay %v, largura %d, linha %d com %d células", overlay, width, i, got)
				}
			}
		}
	}
}

func TestHostedSwapIsSaved(t *testing.T) {
	var got [2]bool
	m := hostedModel(t, 212, 49, true)
	m.hostedPersist = func(_, _ int, overlay, swapped bool, _ int) error {
		got = [2]bool{overlay, swapped}
		return nil
	}
	m.togglePanelSides()
	m.saveHostedColumns(m.hostedSaveGen)()
	if got != [2]bool{false, true} {
		t.Fatalf("salvou %v", got)
	}
}
