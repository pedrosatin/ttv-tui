package ui

import (
	"cmp"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

type footerMode int

const (
	footList footerMode = iota
	footSearch
	footFilter
	footChat
	footCompose
	footMenu
	footFocus
	footOverlay
)

type footerUse struct {
	Mode     footerMode
	Priority int
	Text     string
}

func (m Model) footerMode() footerMode {
	switch {
	case m.LayoutMode() == LayoutFocus:
		return footFocus
	case m.list.prefs.menuOpen:
		return footMenu
	case m.listVisible() && m.list.search.active:
		return footSearch
	case m.listVisible() && m.list.filtering:
		return footFilter
	case m.Focus() == FocusChat && m.chat.composing:
		return footCompose
	case m.hosted.host != nil && m.hosted.chatOverlay && m.chatColumnVisible():
		return footOverlay
	case m.Focus() == FocusChat:
		return footChat
	default:
		return footList
	}
}

// Footer reserva espaço para a ajuda antes de acrescentar as ações por prioridade.
// As teclas vêm do mapa efetivo, inclusive quando uma ação está desligada.
func (m Model) Footer() string {
	type hint struct {
		priority int
		text     string
	}
	var hints []hint
	for _, a := range m.keys.actions {
		for _, use := range a.Footer {
			if use.Mode != m.footerMode() || a.ID == actSwitchPlatform && len(m.catalogs) < 2 {
				continue
			}
			label := ""
			if keys := m.keys.keys[a.ID]; len(keys) > 0 {
				label = keyLabel(keys[0], m.catalog)
			}
			if label != "" {
				hints = append(hints, hint{use.Priority, label + " " + m.t(m.footerText(a.ID, use))})
			}
		}
	}
	slices.SortStableFunc(hints, func(a, b hint) int { return cmp.Compare(a.priority, b.priority) })
	help := m.t("status.help_hint")
	if m.footerMode() == footSearch || m.footerMode() == footFilter || m.footerMode() == footCompose || m.footerMode() == footMenu {
		help = m.t("footer.help_field")
	}
	parts := []string{}
	for _, h := range hints {
		candidate := strings.Join(append(append([]string{}, parts...), h.text, help), statusSep)
		if m.width > 0 && ansi.StringWidth(candidate) > m.width {
			continue
		}
		parts = append(parts, h.text)
		if len(parts) == 4 {
			break
		}
	}
	return padToWidth(strings.Join(append(parts, help), statusSep), m.width)
}

// footerText é o texto da dica. No mapa padrão, Ctrl+W h e Ctrl+W l seguem o
// lado do painel: com os lados trocados, Ctrl+W h traz o chat e Ctrl+W l, a
// lista, e a barra do modo foco diz isso.
func (m Model) footerText(id actionID, use footerUse) string {
	if m.classic || !m.hosted.panelsSwapped || use.Mode != footFocus {
		return use.Text
	}
	var other actionID
	switch id {
	case actToggleList:
		other = actToggleChat
	case actToggleChat:
		other = actToggleList
	default:
		return use.Text
	}
	for _, a := range m.keys.actions {
		for _, u := range a.Footer {
			if a.ID == other && u.Mode == footFocus {
				return u.Text
			}
		}
	}
	return use.Text
}

// A ajuda e o menu já têm dicas próprias; terminais mínimos preservam o conteúdo.
func (m Model) footerRows() int {
	if m.height > 2 && !m.helpOpen() && !m.list.prefs.menuOpen {
		return 1
	}
	return 0
}
