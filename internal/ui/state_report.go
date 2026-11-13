package ui

import tea "github.com/charmbracelet/bubbletea"

// StateReport resume o que a TUI mostra, para os cenários da janela real
// conferirem o estado sem ler a tela. Só existe com WithStateReport, que
// internal/app liga apenas com a variável de teste. Vai em JSON com os nomes
// dos campos.
type StateReport struct {
	// Keys conta as teclas que a TUI já tratou (uma por caractere digitado), para
	// o cenário saber que a tecla que mandou chegou antes de olhar o estado.
	Keys      int
	Width     int
	Height    int
	Focus     string
	Mode      string
	Layout    string
	Help      bool
	Composing bool
	// Search é o campo do Ctrl+F aberto; SearchInput, o texto nele.
	Search      bool
	SearchInput string
	SearchQuery string
	Filtering   bool
	Filter      string
	// Highlighted é o nome do item destacado na lista, e HighlightIndex, a posição dele.
	Highlighted    string
	HighlightIndex int
	Items          int
	// Notice é o aviso da barra de status.
	Notice       string
	Player       string
	ChatChannel  string
	ChatMessages int
	// ChatVisible diz se o chat aparece, ao lado ou sobre o vídeo.
	ChatVisible bool
	Config      ReportConfig
}

// ReportConfig é o config que a TUI aplica agora e grava quando ele muda.
type ReportConfig struct {
	ChatOverlay        bool
	ChatOverlayOpacity int
	ListColumns        int
	ChatColumns        int
	PanelsSwapped      bool
}

// WithStateReport chama report depois de cada Update que muda o StateReport. A
// chamada acontece dentro do Update: report não pode bloquear.
func WithStateReport(report func(StateReport)) Option {
	return func(m *Model) {
		m.stateReport = report
	}
}

// reportState avisa o observador quando o resumo mudou. msg é a mensagem que o
// Update acabou de tratar.
func (m Model) reportState(msg tea.Msg) Model {
	if m.stateReport == nil {
		return m
	}
	if k, ok := msg.(tea.KeyMsg); ok {
		m.keysSeen += max(len(k.Runes), 1)
	}
	if r := m.stateSnapshot(); r != m.reported {
		m.stateReport(r)
		m.reported = r
	}
	return m
}

func (m Model) stateSnapshot() StateReport {
	r := StateReport{
		Keys:  m.keysSeen,
		Width: m.width, Height: m.height,
		Focus: string(m.Focus()), Mode: string(m.Mode()), Layout: string(m.LayoutMode()),
		Help: m.layout.helpVisible, Composing: m.chat.composing,
		Search: m.list.search.active, SearchInput: m.list.search.input, SearchQuery: m.list.search.query,
		Filtering: m.list.filtering, Filter: m.list.filter,
		HighlightIndex: -1,
		Notice:         m.notices.status,
		Player:         string(m.player.state),
		ChatChannel:    m.chat.panel.channel,
		ChatVisible:    m.chatColumnVisible(),
		Config: ReportConfig{
			ChatOverlay: m.hosted.chatOverlay, ChatOverlayOpacity: m.hosted.chatOpacity(),
			ListColumns: m.hosted.listCols, ChatColumns: m.hosted.chatCols, PanelsSwapped: m.hosted.panelsSwapped,
		},
	}
	if m.chat.panel.buf != nil {
		r.ChatMessages = m.chat.panel.buf.Len()
	}
	items := m.VisibleItems()
	r.Items = len(items)
	if m.list.highlightedID != "" && !m.list.hardErr() {
		for i := range items {
			if items[i].ID() == m.list.highlightedID {
				r.Highlighted, r.HighlightIndex = items[i].DisplayName(), i
				break
			}
		}
	}
	return r
}
