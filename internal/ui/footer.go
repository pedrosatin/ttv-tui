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
	// footOverlay e footOverlayChat são o chat sobre o vídeo, com o foco na
	// lista e no chat.
	footOverlay
	footOverlayChat
	// footConfirm é a pergunta da barra antes de limpar os recentes.
	footConfirm
)

// footerUse é uma dica da ação no rodapé de um modo. Pair junta à tecla da ação
// a de outra, oposta (Alt+↑/↓ altura). Paused mostra a dica só com o chat fora
// do ao vivo.
type footerUse struct {
	Mode     footerMode
	Priority int
	Text     string
	Pair     actionID
	Paused   bool
	// ListHidden: a dica só aparece com a lista escondida.
	ListHidden bool
	// CatalogBelow: a dica só aparece na tela inicial com a primeira categoria
	// ao vivo abaixo da janela da lista.
	CatalogBelow bool
}

func (m Model) footerMode() footerMode {
	switch {
	case m.list.confirmClear:
		return footConfirm
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
		if m.Focus() == FocusChat {
			return footOverlayChat
		}
		return footOverlay
	case m.Focus() == FocusChat:
		return footChat
	default:
		return footList
	}
}

// Footer reserva espaço para a ajuda antes de acrescentar as ações por prioridade.
// As teclas vêm do mapa efetivo, inclusive quando uma ação está desligada.
//
// No chat sobreposto, a navegação vem antes do ajuste do overlay: com o chat
// pausado, a tecla do ao vivo; depois, a troca de foco; por último, altura e
// opacidade. As dicas entram nessa ordem enquanto couberem junto da ajuda: uma
// que não cabe fica de fora e a seguinte ainda entra se couber. Em 80 colunas
// cabem todas.
func (m Model) Footer() string {
	type hint struct {
		priority int
		text     string
	}
	var hints []hint
	for _, a := range m.keys.actions {
		for _, use := range a.Footer {
			if use.Mode != m.footerMode() || a.ID == actSwitchPlatform && len(m.catalogs) < 2 || use.Paused && m.ChatFollowLive() || m.overlayListHint(a.ID, use) || use.CatalogBelow && !m.liveCatalogBelow() {
				continue
			}
			label := m.footerLabel(a.ID, use.Pair)
			if label != "" {
				hints = append(hints, hint{use.Priority, label + " " + m.t(m.footerText(a.ID, use))})
			}
		}
	}
	slices.SortStableFunc(hints, func(a, b hint) int { return cmp.Compare(a.priority, b.priority) })
	// A pergunta da barra só aceita a resposta e a ajuda não abre com ela.
	var help []string
	switch m.footerMode() {
	case footConfirm:
	case footSearch, footFilter, footCompose, footMenu:
		if hint := m.statusHint("footer.help_field", actHelpAnywhere); hint != "" {
			help = []string{hint}
		}
	default:
		if hint := m.helpStatusHint(); hint != "" {
			help = []string{hint}
		}
	}
	parts := []string{}
	for _, h := range hints {
		candidate := strings.Join(append(append(slices.Clone(parts), h.text), help...), statusSep)
		if m.width > 0 && ansi.StringWidth(candidate) > m.width {
			continue
		}
		parts = append(parts, h.text)
		if len(parts) == 4 {
			break
		}
	}
	return padToWidth(strings.Join(append(parts, help...), statusSep), m.width)
}

// footerLabel é a tecla principal de id e, com pair, também a de pair, juntas
// como na ajuda: Alt+↑/↓. Uma ação sem tecla não entra.
func (m Model) footerLabel(id, pair actionID) string {
	var labels []string
	for _, a := range []actionID{id, pair} {
		if keys := m.keys.keys[a]; a != "" && len(keys) > 0 {
			labels = append(labels, keyLabel(keys[0], m.catalog))
		}
	}
	return joinKeyLabels(labels)
}

// overlayListHint diz se a dica sai do rodapé do chat sobreposto por causa da
// lista escondida. Com a lista escondida, Tab não leva o foco a ela: no lugar de
// "Tab lista" entra a tecla que mostra a lista de novo. No mapa padrão com os
// lados trocados, essa tecla é a do painel da direita (Ctrl+W l).
func (m Model) overlayListHint(id actionID, use footerUse) bool {
	if use.Mode != footOverlayChat {
		return false
	}
	if id == actSwitchFocus {
		return !m.listVisible()
	}
	if !use.ListHidden {
		return false
	}
	toggle := actToggleList
	if !m.classic && m.hosted.panelsSwapped {
		toggle = actToggleChat
	}
	return m.listVisible() || id != toggle
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
