package ui

import (
	"slices"
	"strconv"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

// actionID nomeia uma ação do registro de teclas. O valor é também a chave da
// descrição da ação no catálogo de i18n, e é por ele que um texto do catálogo
// cita a tecla: "{action.quit} sair" vira "Ctrl+Q sair".
type actionID string

const (
	actFavorites      actionID = "action.favorites"
	actRecents        actionID = "action.recents"
	actWindowList     actionID = "action.window_list"
	actQuit           actionID = "action.quit"
	actCtrlC          actionID = "action.ctrl_c"
	actCloseHelp      actionID = "action.close_help"
	actHelp           actionID = "action.help"
	actHelpAnywhere   actionID = "action.help_anywhere"
	actHelpNext       actionID = "action.help_next_section"
	actHelpPrev       actionID = "action.help_prev_section"
	actCategories     actionID = "action.categories"
	actStopVideo      actionID = "action.stop_video"
	actReconnectChat  actionID = "action.reconnect_chat"
	actCycleLayout    actionID = "action.cycle_layout"
	actToggleList     actionID = "action.toggle_list"
	actToggleChat     actionID = "action.toggle_chat"
	actChatOverlay    actionID = "action.chat_overlay"
	actSwapSides      actionID = "action.swap_sides"
	actChatTaller     actionID = "action.chat_taller"
	actChatShorter    actionID = "action.chat_shorter"
	actChatMoreOpaque actionID = "action.chat_more_opaque"
	actChatLessOpaque actionID = "action.chat_less_opaque"
	actListWider      actionID = "action.list_wider"
	actListNarrower   actionID = "action.list_narrower"
	actChatWider      actionID = "action.chat_wider"
	actChatNarrower   actionID = "action.chat_narrower"
	actUp             actionID = "action.up"
	actDown           actionID = "action.down"
	actPageUp         actionID = "action.page_up"
	actPageDown       actionID = "action.page_down"
	actHome           actionID = "action.home"
	actEnd            actionID = "action.end"
	actSelect         actionID = "action.select"
	actSwitchFocus    actionID = "action.switch_focus"
	actFilter         actionID = "action.filter"
	actSearch         actionID = "action.search"
	actChannelFilter  actionID = "action.channel_filter"
	actSwitchPlatform actionID = "action.switch_platform"
	actRetry          actionID = "action.retry"
	actBack           actionID = "action.back"
	actPause          actionID = "action.pause"
	actVolumeUp       actionID = "action.volume_up"
	actVolumeDown     actionID = "action.volume_down"
	actMute           actionID = "action.mute"
	actCompose        actionID = "action.compose"
	actCopyMessage    actionID = "action.copy_message"
	actFavorite       actionID = "action.favorite"
	actForgetRecent   actionID = "action.forget_recent"
	actClearRecents   actionID = "action.clear_recents"
	actConfirm        actionID = "action.confirm"
	actCancelConfirm  actionID = "action.cancel_confirm"
	actQuickOpen      actionID = "action.quick_open"
)

// keyMode é um contexto de despacho. Uma tecla pode disparar ações diferentes em
// modos diferentes, mas nunca duas no mesmo contexto (veja keyContexts).
type keyMode int

const (
	// modeApp vem antes de tudo, até da ajuda: sair e o aviso do Ctrl+C.
	modeApp keyMode = iota
	// modeHelp é a ajuda aberta, que consome toda tecla.
	modeHelp
	// modeConfirm é uma pergunta na barra (limpar os recentes), que também
	// consome toda tecla até a resposta.
	modeConfirm
	// modeGlobal vale em qualquer modo, até digitando.
	modeGlobal
	// modeWindow vale em qualquer modo, só na janela única.
	modeWindow
	// modeSearch é o prompt de busca.
	modeSearch
	// modeFilter é o filtro / da lista.
	modeFilter
	// modeMenu é o menu de filtros de canal.
	modeMenu
	// modeCompose é a escrita da mensagem no chat.
	modeCompose
	// modeQuickOpen é o seletor rápido (Ctrl+K).
	modeQuickOpen
	// modeNormal é a lista e o chat fora de qualquer campo de texto.
	modeNormal
)

// keyContexts lista, para cada situação da TUI, os modos que o despacho
// consulta. Dentro de um contexto, cada tecla dispara no máximo uma ação.
var keyContexts = [][]keyMode{
	{modeApp, modeHelp},
	{modeApp, modeConfirm},
	{modeApp, modeGlobal, modeWindow, modeSearch},
	{modeApp, modeGlobal, modeWindow, modeFilter},
	{modeApp, modeGlobal, modeWindow, modeMenu},
	{modeApp, modeGlobal, modeWindow, modeCompose},
	{modeApp, modeGlobal, modeWindow, modeQuickOpen},
	{modeApp, modeGlobal, modeWindow, modeNormal},
}

// helpSection agrupa as ações na ajuda, na ordem em que ela as mostra.
// sectionHidden fica fora dela.
type helpSection int

const (
	sectionHidden helpSection = iota
	sectionGlobal
	sectionNav
	sectionSearch
	sectionChat
	sectionVideo
	sectionWindow
	sectionSettings
)

// helpSections são as seções da ajuda, na ordem da tela.
var helpSections = []helpSection{
	sectionGlobal, sectionNav, sectionSearch, sectionChat, sectionVideo, sectionWindow, sectionSettings,
}

// helpContext é a situação em que uma linha da ajuda vale dentro da seção: no
// prompt de busca, no filtro, escrevendo. ctxAny é a seção inteira.
type helpContext int

const (
	ctxAny helpContext = iota
	ctxHelp
	ctxSearchPrompt
	ctxFilter
	ctxMenu
	ctxChatPanel
	ctxCompose
	ctxField
	// ctxConfirm é a pergunta da barra antes de limpar os recentes.
	ctxConfirm
	// ctxQuickOpen é o seletor rápido aberto com Ctrl+K.
	ctxQuickOpen
	// ctxWindowOnly agrupa as ações que só a janela única tem. A ajuda o deduz
	// dos modos da ação; nenhuma o declara.
	ctxWindowOnly
)

// helpUse é uma linha da ajuda: a seção, a situação e a chave do texto no
// catálogo. Linhas da mesma seção e situação com o mesmo texto se juntam numa
// só, com as teclas de todas: "q Esc  Fecha a ajuda".
type helpUse struct {
	Section helpSection
	Context helpContext
	Text    string
}

// action é uma entrada do registro: em que modos a ação vale, em que seção da
// ajuda aparece e as teclas padrão, no formato de tea.KeyMsg.String(). A
// primeira tecla é a principal, a que os textos curtos citam.
//
// A linha principal da ação fica em Section e Context, com o texto de ID. Also
// lista o que a mesma tecla faz em outras situações, como Enter no filtro ou
// Esc na escrita, para a ajuda mostrar cada uso na seção dele.
type action struct {
	ID      actionID
	Modes   []keyMode
	Section helpSection
	Context helpContext
	Default []string
	Also    []helpUse
	Footer  []footerUse
	// Text troca a chave do texto da linha principal, que sem ele é o ID: no
	// mapa padrão, Ctrl+W h e Ctrl+W l falam do painel da esquerda e da
	// direita, porque seguem o lado depois de trocar os lados.
	Text string
}

// text é a chave do catálogo da linha principal da ação.
func (a action) text() string {
	if a.Text != "" {
		return a.Text
	}
	return string(a.ID)
}

// helpUses são as linhas da ajuda da ação: a principal e as de Also.
func (a action) helpUses() []helpUse {
	if a.Section == sectionHidden {
		return nil
	}
	return append([]helpUse{{Section: a.Section, Context: a.Context, Text: a.text()}}, a.Also...)
}

// windowOnly diz se a ação só vale na janela única.
func (a action) windowOnly() bool {
	return len(a.Modes) > 0 && !slices.ContainsFunc(a.Modes, func(m keyMode) bool { return m != modeWindow })
}

// classicActions é o mapa anterior à migração, na ordem em que a ajuda mostra as
// linhas de cada seção. As primitivas de edição dos campos de texto ficam fora
// dele, em field_keys.go.
func classicActions() []action {
	nav := []keyMode{modeNormal, modeHelp, modeMenu}
	// O movimento rola a ajuda e o chat e anda pelo menu de filtros.
	also := func(help, menu, chat string) []helpUse {
		uses := []helpUse{{sectionGlobal, ctxHelp, help}, {sectionSearch, ctxMenu, menu}}
		if chat != "" {
			uses = append(uses, helpUse{sectionChat, ctxChatPanel, chat})
		}
		return uses
	}
	actions := []action{
		// Ctrl+Q é a única tecla que sai. Ctrl+C chega como tecla (o terminal
		// está em modo raw, sem SIGINT) e só explica como sair.
		{ID: actQuit, Modes: []keyMode{modeApp}, Section: sectionGlobal, Default: []string{"ctrl+q"}},
		{ID: actCtrlC, Modes: []keyMode{modeApp}, Section: sectionGlobal, Default: []string{"ctrl+c"}},
		{ID: actHelp, Modes: []keyMode{modeNormal, modeHelp}, Section: sectionGlobal, Default: []string{"?"}},
		{ID: actHelpAnywhere, Modes: []keyMode{modeGlobal, modeHelp}, Section: sectionGlobal, Default: []string{"f1"}},

		// q só fecha a ajuda; fora dela não faz nada. Tab e as setas laterais
		// pulam de seção, porque ↑ e ↓ rolam linha a linha.
		{ID: actCloseHelp, Modes: []keyMode{modeHelp}, Section: sectionGlobal, Context: ctxHelp, Default: []string{"q"}},
		{ID: actHelpNext, Modes: []keyMode{modeHelp}, Section: sectionGlobal, Context: ctxHelp, Default: []string{"tab", "right"}},
		{ID: actHelpPrev, Modes: []keyMode{modeHelp}, Section: sectionGlobal, Context: ctxHelp, Default: []string{"shift+tab", "left"}},

		// O movimento vale na lista, no chat, na ajuda e no menu. No filtro, só
		// as setas andam, e elas são primitivas do campo.
		{ID: actUp, Modes: nav, Section: sectionNav, Default: []string{"up", "k"},
			Also: also("help.use.scroll_line", "help.use.menu_move", "help.use.chat_scroll")},
		{ID: actDown, Modes: nav, Section: sectionNav, Default: []string{"down", "j"},
			Also: also("help.use.scroll_line", "help.use.menu_move", "help.use.chat_scroll")},
		{ID: actPageUp, Modes: []keyMode{modeNormal, modeHelp, modeMenu, modeFilter}, Section: sectionNav, Default: []string{"pgup"},
			Also: append(also("help.use.scroll_page", "help.use.menu_move", "help.use.chat_page"), helpUse{sectionSearch, ctxFilter, "help.use.filter_move"})},
		{ID: actPageDown, Modes: []keyMode{modeNormal, modeHelp, modeMenu, modeFilter}, Section: sectionNav, Default: []string{"pgdown"},
			Also: append(also("help.use.scroll_page", "help.use.menu_move", "help.use.chat_page"), helpUse{sectionSearch, ctxFilter, "help.use.filter_move"})},
		{ID: actHome, Modes: nav, Section: sectionNav, Default: []string{"home", "g"},
			Also: also("help.use.scroll_ends", "help.use.menu_move", "help.use.chat_top")},
		// End/G vão ao fim da lista ou retomam o ao vivo no chat.
		{ID: actEnd, Modes: nav, Section: sectionNav, Default: []string{"end", "G"},
			Also: also("help.use.scroll_ends", "help.use.menu_move", "help.use.chat_live")},
		{ID: actSelect, Modes: []keyMode{modeNormal, modeSearch, modeFilter, modeMenu, modeCompose, modeQuickOpen}, Section: sectionNav, Default: []string{"enter"},
			Also: []helpUse{
				{sectionSearch, ctxSearchPrompt, "help.use.search_confirm"},
				{sectionSearch, ctxFilter, "help.use.filter_enter"},
				{sectionSearch, ctxMenu, "help.use.menu_toggle"},
				{sectionSearch, ctxQuickOpen, "help.use.quick_open_select"},
				{sectionChat, ctxChatPanel, "help.use.chat_enter"},
				{sectionChat, ctxCompose, "help.use.compose_send"},
			}},
		{ID: actSwitchFocus, Modes: []keyMode{modeNormal, modeCompose}, Section: sectionNav, Default: []string{"tab"},
			Also: []helpUse{{sectionChat, ctxCompose, "help.use.compose_tab"}}},
		{ID: actBack, Modes: []keyMode{modeNormal, modeHelp, modeSearch, modeFilter, modeMenu, modeCompose, modeQuickOpen}, Section: sectionNav, Default: []string{"esc"},
			Also: []helpUse{
				{sectionGlobal, ctxHelp, string(actCloseHelp)},
				{sectionSearch, ctxSearchPrompt, "help.use.search_cancel"},
				{sectionSearch, ctxFilter, "help.use.filter_clear"},
				{sectionSearch, ctxMenu, "help.use.menu_close"},
				{sectionSearch, ctxQuickOpen, "help.use.quick_open_cancel"},
				{sectionChat, ctxChatPanel, "help.use.chat_leave"},
				{sectionChat, ctxCompose, "help.use.compose_leave"},
				{sectionWindow, ctxAny, "help.use.back_layout"},
			}},
		{ID: actCategories, Modes: []keyMode{modeGlobal}, Section: sectionNav, Default: []string{"ctrl+r"}},
		// p troca a lista para a próxima plataforma; o vídeo e o chat ficam.
		{ID: actSwitchPlatform, Modes: []keyMode{modeNormal}, Section: sectionNav, Default: []string{"p"}},
		{ID: actFavorite, Modes: []keyMode{modeNormal}, Section: sectionNav, Default: []string{"f"}},
		// x tira dos recentes o canal destacado na seção Recentes.
		{ID: actForgetRecent, Modes: []keyMode{modeNormal}, Section: sectionNav, Default: []string{"x"}},
		// X limpa os recentes da lista depois de uma pergunta na barra, que só y
		// confirma; Esc ou n a desfazem sem mudar nada.
		{ID: actClearRecents, Modes: []keyMode{modeNormal}, Section: sectionNav, Default: []string{"X"}},
		{ID: actConfirm, Modes: []keyMode{modeConfirm}, Section: sectionNav, Context: ctxConfirm, Default: []string{"y"}},
		{ID: actCancelConfirm, Modes: []keyMode{modeConfirm}, Section: sectionNav, Context: ctxConfirm, Default: []string{"esc", "n"}},

		{ID: actFilter, Modes: []keyMode{modeNormal}, Section: sectionSearch, Default: []string{"/"}},
		// Ctrl+F é a tecla da busca; Ctrl+S, um apelido. O modo raw desliga o
		// controle de fluxo, e o terminal entrega o Ctrl+S em vez de pausar a saída.
		{ID: actSearch, Modes: []keyMode{modeNormal, modeSearch, modeFilter}, Section: sectionSearch, Default: []string{"ctrl+f", "ctrl+s"},
			Also: []helpUse{
				{sectionSearch, ctxSearchPrompt, "help.use.search_confirm"},
				{sectionSearch, ctxFilter, "help.use.filter_search"},
			}},
		// Ctrl+K abre o seletor rápido em qualquer tela de lista ou no chat,
		// fora de um campo de texto. No seletor aberto, a mesma tecla fecha.
		{ID: actQuickOpen, Modes: []keyMode{modeNormal, modeQuickOpen}, Section: sectionSearch, Default: []string{"ctrl+k"},
			Also: []helpUse{{sectionSearch, ctxQuickOpen, "help.use.quick_open_cancel"}}},
		{ID: actChannelFilter, Modes: []keyMode{modeNormal, modeMenu}, Section: sectionSearch, Default: []string{"l"},
			Also: []helpUse{{sectionSearch, ctxMenu, "help.use.menu_close"}}},

		{ID: actCompose, Modes: []keyMode{modeNormal}, Section: sectionChat, Default: []string{"i"}},
		// y copia a mensagem marcada (ou a última, ao vivo) pelo wl-copy, xclip ou
		// OSC 52 (veja writeClipboard).
		{ID: actCopyMessage, Modes: []keyMode{modeNormal}, Section: sectionChat, Default: []string{"y"}},
		{ID: actReconnectChat, Modes: []keyMode{modeGlobal}, Section: sectionChat, Default: []string{"ctrl+t"}},

		{ID: actPause, Modes: []keyMode{modeNormal, modeMenu}, Section: sectionVideo, Default: []string{" ", "space"},
			Also: []helpUse{{sectionSearch, ctxMenu, "help.use.menu_toggle"}}},
		{ID: actVolumeUp, Modes: []keyMode{modeNormal}, Section: sectionVideo, Default: []string{"+", "="}},
		{ID: actVolumeDown, Modes: []keyMode{modeNormal}, Section: sectionVideo, Default: []string{"-", "_"}},
		// m liga e desliga o mudo, como no mpv e no player da Twitch.
		{ID: actMute, Modes: []keyMode{modeNormal}, Section: sectionVideo, Default: []string{"m"}},
		{ID: actStopVideo, Modes: []keyMode{modeGlobal}, Section: sectionVideo, Default: []string{"ctrl+x"}},
		// r tenta de novo o vídeo que falhou; sem ele, a lista.
		{ID: actRetry, Modes: []keyMode{modeNormal}, Section: sectionVideo, Default: []string{"r"}},

		// Ctrl+W cicla os layouts; Ctrl+H e Ctrl+L ligam a lista e o chat. O
		// bubbletea entrega Ctrl+H (BS) e Backspace (DEL) como teclas distintas.
		{ID: actCycleLayout, Modes: []keyMode{modeGlobal}, Section: sectionWindow, Default: []string{"ctrl+w"}},
		{ID: actToggleList, Modes: []keyMode{modeGlobal}, Section: sectionWindow, Default: []string{"ctrl+h"}},
		{ID: actToggleChat, Modes: []keyMode{modeGlobal}, Section: sectionWindow, Default: []string{"ctrl+l"}},

		// Só a janela única tem vídeo ao lado dos painéis. A seta move a divisa:
		// a da lista fica à direita dela, a do chat à esquerda, e cada painel
		// cresce na direção do vídeo.
		{ID: actChatOverlay, Modes: []keyMode{modeWindow}, Section: sectionWindow, Default: []string{"ctrl+o"}},
		{ID: actSwapSides, Modes: []keyMode{modeWindow}, Section: sectionWindow, Default: []string{"ctrl+p"}},
		{ID: actChatTaller, Modes: []keyMode{modeWindow}, Section: sectionWindow, Default: []string{"alt+up"}},
		{ID: actChatShorter, Modes: []keyMode{modeWindow}, Section: sectionWindow, Default: []string{"alt+down"}},
		{ID: actChatMoreOpaque, Modes: []keyMode{modeWindow}, Section: sectionWindow, Default: []string{"alt+pgup"}},
		{ID: actChatLessOpaque, Modes: []keyMode{modeWindow}, Section: sectionWindow, Default: []string{"alt+pgdown"}},
		{ID: actListWider, Modes: []keyMode{modeWindow}, Section: sectionWindow, Default: []string{"ctrl+right"}},
		{ID: actListNarrower, Modes: []keyMode{modeWindow}, Section: sectionWindow, Default: []string{"ctrl+left"}},
		{ID: actChatWider, Modes: []keyMode{modeWindow}, Section: sectionWindow, Default: []string{"alt+left"}},
		{ID: actChatNarrower, Modes: []keyMode{modeWindow}, Section: sectionWindow, Default: []string{"alt+right"}},
	}
	for i := range actions {
		a := &actions[i]
		switch a.ID {
		// No chat sobreposto, a navegação vem antes da altura e da opacidade
		// (veja Footer); cada par de ações opostas ocupa uma dica só.
		case actChatTaller:
			a.Footer = []footerUse{
				{Mode: footOverlay, Priority: 3, Text: "footer.height", Pair: actChatShorter},
				{Mode: footOverlayChat, Priority: 3, Text: "footer.height", Pair: actChatShorter},
			}
		case actChatMoreOpaque:
			a.Footer = []footerUse{
				{Mode: footOverlay, Priority: 4, Text: "footer.opacity", Pair: actChatLessOpaque},
				{Mode: footOverlayChat, Priority: 4, Text: "footer.opacity", Pair: actChatLessOpaque},
			}
		case actEnd:
			a.Footer = []footerUse{{Mode: footOverlayChat, Priority: 1, Text: "footer.live", Paused: true}}
		case actCycleLayout:
			a.Footer = []footerUse{{Mode: footFocus, Priority: 1, Text: "footer.layouts"}}
		case actToggleList:
			a.Footer = []footerUse{{Mode: footFocus, Priority: 2, Text: "footer.list"}, {Mode: footOverlayChat, Priority: 2, Text: "footer.list", ListHidden: true}}
		case actToggleChat:
			a.Footer = []footerUse{{Mode: footFocus, Priority: 3, Text: "layout.mode.chat"}, {Mode: footOverlayChat, Priority: 2, Text: "footer.list", ListHidden: true}}
		case actSelect:
			a.Footer = []footerUse{{Mode: footList, Priority: 1, Text: "footer.open"}, {Mode: footSearch, Priority: 1, Text: "footer.search"}, {Mode: footFilter, Priority: 1, Text: "footer.open"}, {Mode: footCompose, Priority: 1, Text: "footer.send"}, {Mode: footMenu, Priority: 1, Text: "footer.choose"}, {Mode: footQuickOpen, Priority: 1, Text: "footer.open"}}
		case actFavorite:
			a.Footer = []footerUse{{Mode: footList, Priority: 2, Text: "footer.favorite"}}
		case actCategories:
			// Só quando os itens salvos empurraram o catálogo ao vivo para baixo
			// da janela da lista. Entra antes do filtro e tira a busca do
			// rodapé, que o filtro ainda cita ao ser aberto.
			a.Footer = []footerUse{{Mode: footList, Priority: 3, Text: "footer.categories", CatalogBelow: true}}
		case actFilter:
			a.Footer = []footerUse{{Mode: footList, Priority: 3, Text: "footer.filter"}}
		case actSearch:
			a.Footer = []footerUse{{Mode: footList, Priority: 4, Text: "footer.search"}, {Mode: footFilter, Priority: 2, Text: "footer.search"}}
		case actQuickOpen:
			a.Footer = []footerUse{
				{Mode: footList, Priority: 6, Text: "footer.quick_open"},
				{Mode: footChat, Priority: 4, Text: "footer.quick_open"},
			}
		case actSwitchPlatform:
			a.Footer = []footerUse{{Mode: footList, Priority: 5, Text: "footer.platform"}}
		case actBack:
			a.Footer = []footerUse{{Mode: footSearch, Priority: 2, Text: "footer.cancel"}, {Mode: footFilter, Priority: 3, Text: "footer.clear"}, {Mode: footCompose, Priority: 2, Text: "footer.cancel"}, {Mode: footMenu, Priority: 2, Text: "footer.close"}, {Mode: footQuickOpen, Priority: 2, Text: "footer.cancel"}}
		case actCompose:
			a.Footer = []footerUse{{Mode: footChat, Priority: 1, Text: "footer.compose"}}
		case actCopyMessage:
			a.Footer = []footerUse{{Mode: footChat, Priority: 2, Text: "footer.copy"}}
		case actConfirm:
			a.Footer = []footerUse{{Mode: footConfirm, Priority: 1, Text: "footer.clear"}}
		case actCancelConfirm:
			a.Footer = []footerUse{{Mode: footConfirm, Priority: 2, Text: "footer.cancel"}}
		case actSwitchFocus:
			a.Footer = []footerUse{
				{Mode: footChat, Priority: 3, Text: "footer.list"}, {Mode: footCompose, Priority: 3, Text: "footer.list"},
				{Mode: footOverlay, Priority: 2, Text: "footer.chat"}, {Mode: footOverlayChat, Priority: 2, Text: "footer.list"},
			}
		}
	}
	return actions
}

// defaultActions é o mapa padrão: o clássico com Ctrl+W como prefixo das
// janelas, como no vim, g como prefixo de ir a um lugar da lista e L no filtro de
// idioma. Ctrl+H, Ctrl+L, Ctrl+P, Ctrl+S e o g sozinho do Home deixam de ser
// padrão (o topo fica em g g, como no vim); o Ctrl+O do chat sobreposto fica
// como apelido de Ctrl+W o.
func defaultActions() []action {
	actions := classicActions()
	bindings := map[actionID][]string{
		actCycleLayout: {"ctrl+w w"}, actToggleList: {"ctrl+w h"}, actToggleChat: {"ctrl+w l"},
		actChatOverlay: {"ctrl+w o", "ctrl+o"}, actSwapSides: {"ctrl+w x"},
		actChatTaller: {"alt+up", "ctrl+w +"}, actChatShorter: {"alt+down", "ctrl+w -"},
		actChatWider: {"alt+left", "ctrl+w <"}, actChatNarrower: {"alt+right", "ctrl+w >"},
		actChannelFilter: {"L"}, actCategories: {"g c", "ctrl+r"}, actSearch: {"ctrl+f"}, actHome: {"home", "g g"},
	}
	for i := range actions {
		if keys, ok := bindings[actions[i].ID]; ok {
			actions[i].Default = keys
		}
		switch actions[i].ID {
		case actToggleList:
			actions[i].Text = "action.left_panel"
		case actToggleChat:
			actions[i].Text = "action.right_panel"
		}
	}
	actions = append(actions,
		action{ID: actFavorites, Modes: []keyMode{modeNormal}, Section: sectionNav, Default: []string{"g f"}},
		action{ID: actRecents, Modes: []keyMode{modeNormal}, Section: sectionNav, Default: []string{"g r"}},
		action{ID: actWindowList, Modes: []keyMode{modeWindow}, Section: sectionWindow, Default: []string{"ctrl+b"}},
	)
	return actions
}

// keyRegistry responde que ação uma tecla dispara num modo e que teclas uma ação
// tem. As teclas efetivas são as padrão, salvo as que overrides troca.
type keyRegistry struct {
	actions      []action
	keys         map[actionID][]string
	byMode       map[keyMode]map[string]actionID
	livePrefixes map[string]bool
}

// newKeyRegistry monta o registro. overrides troca as teclas de uma ação inteira;
// nil deixa o mapa padrão. Se duas ações dividem uma tecla num modo, vale a que
// vem primeiro na lista. Quem aceita teclas de fora confere conflitos antes.
func newKeyRegistry(actions []action, overrides map[actionID][]string) keyRegistry {
	r := keyRegistry{
		actions:      actions,
		keys:         make(map[actionID][]string, len(actions)),
		byMode:       make(map[keyMode]map[string]actionID),
		livePrefixes: map[string]bool{},
	}
	for _, a := range actions {
		keys := a.Default
		if o, ok := overrides[a.ID]; ok {
			keys = o
		}
		r.keys[a.ID] = slices.Clone(keys)
		markLivePrefixes(keys, r.livePrefixes)
		for _, mode := range a.Modes {
			if r.byMode[mode] == nil {
				r.byMode[mode] = make(map[string]actionID)
			}
			for _, k := range keys {
				name := indexName(k)
				if _, taken := r.byMode[mode][name]; !taken {
					r.byMode[mode][name] = a.ID
				}
			}
		}
	}
	return r
}

// bindsKey diz se algum modo do registro indexa a tecla ou sequência name.
func (r keyRegistry) bindsKey(name string) bool {
	name = indexName(name)
	for _, slot := range r.byMode {
		if _, ok := slot[name]; ok {
			return true
		}
	}
	return false
}

// prefixLiveIn diz se há sequência ativa começando por prefix nos modos dados.
func (r keyRegistry) prefixLiveIn(prefix string, modes ...keyMode) bool {
	if !r.livePrefixes[prefix] {
		return false
	}
	needle := prefix + " "
	for _, mode := range modes {
		for name := range r.byMode[mode] {
			if strings.HasPrefix(name, needle) {
				return true
			}
		}
	}
	return false
}

// indexName é o nome com que o terminal entrega a tecla: "space" chega como " ".
// Um nome que não é tecla fica como está e nunca dispara.
func indexName(name string) string {
	if msg, ok := parseKeyName(name); ok {
		return keyName(msg)
	}
	return name
}

// defaultKeys é o registro com o mapa padrão.
func defaultKeys() keyRegistry {
	return newKeyRegistry(defaultActions(), nil)
}

// actionFor responde que ação msg dispara em mode, ou "" se nenhuma.
func (r keyRegistry) actionFor(mode keyMode, msg tea.KeyMsg) actionID {
	return r.byMode[mode][keyName(msg)]
}

// expandKeys troca, num texto do catálogo, cada referência entre chaves pela
// tecla efetiva:
//
//	{action.home}               todas as teclas da ação: Home/g
//	{action.search:2}           só a segunda: Ctrl+S
//	{action.list_narrower/action.list_wider}  as de várias ações juntas: Ctrl+←/→
//
// Uma referência a ação desconhecida fica como está, para o teste do catálogo
// apontar o erro em vez de a tela esconder a tecla.
func (r keyRegistry) expandKeys(text string, cat *i18n.Catalog) string {
	if strings.IndexByte(text, '{') < 0 {
		return text
	}
	var b strings.Builder
	for {
		open := strings.IndexByte(text, '{')
		if open < 0 {
			break
		}
		size := strings.IndexByte(text[open:], '}')
		if size < 0 {
			break
		}
		b.WriteString(text[:open])
		if label, ok := r.refLabel(text[open+1:open+size], cat); ok {
			b.WriteString(label)
		} else {
			b.WriteString(text[open : open+size+1])
		}
		text = text[open+size+1:]
	}
	b.WriteString(text)
	return b.String()
}

// refLabel resolve o conteúdo de uma referência: ações separadas por /, cada uma
// com um índice opcional (1 é a tecla principal).
func (r keyRegistry) refLabel(ref string, cat *i18n.Catalog) (string, bool) {
	var labels []string
	for _, part := range strings.Split(ref, "/") {
		id, index := part, 0
		if at := strings.IndexByte(part, ':'); at >= 0 {
			n, err := strconv.Atoi(part[at+1:])
			if err != nil || n < 1 {
				return "", false
			}
			id, index = part[:at], n
		}
		keys, ok := r.keys[actionID(id)]
		if !ok {
			return "", false
		}
		if index > 0 {
			if index > len(keys) {
				continue
			}
			keys = keys[index-1 : index]
		}
		for _, k := range keys {
			labels = append(labels, keyLabel(k, cat))
		}
	}
	return joinKeyLabels(labels), true
}

// joinKeyLabels junta rótulos de tecla com /, sem repetir nenhum, e tira de cada
// um o começo que ele divide com o primeiro: Ctrl+←/→, Alt+↑/↓, PgUp/Dn.
func joinKeyLabels(labels []string) string {
	var unique []string
	for _, l := range labels {
		if l != "" && !slices.Contains(unique, l) {
			unique = append(unique, l)
		}
	}
	if len(unique) == 0 {
		return ""
	}
	parts := []string{unique[0]}
	for _, l := range unique[1:] {
		parts = append(parts, trimSharedLabel(unique[0], l))
	}
	return strings.Join(parts, "/")
}

// trimSharedLabel é o rótulo l sem o começo que ele divide com first. Uma
// sequência (Ctrl+W h, g c) só perde o prefixo inteiro, e só para outra do mesmo
// prefixo: Ctrl+W h/l/w, g c/f/r. Entre uma sequência e uma tecla só, nada sai,
// para Ctrl+W o e Ctrl+O não virarem Ctrl+W o/O.
func trimSharedLabel(first, l string) string {
	firstPrefix, _, firstSeq := strings.Cut(first, " ")
	prefix, rest, seq := strings.Cut(l, " ")
	switch {
	case firstSeq && seq && firstPrefix == prefix:
		return rest
	case firstSeq || seq:
		return l
	}
	return l[sharedLabelPrefix(first, l):]
}

// sharedLabelPrefix é o tamanho, em bytes de b, do maior começo comum de a e b
// que termina numa fronteira de nome: logo depois de um + (Ctrl+) ou antes de uma
// maiúscula nos dois (Pg|Up, Pg|Dn). Sobra sempre algo de b.
func sharedLabelPrefix(a, b string) int {
	ar, br := []rune(a), []rune(b)
	common := 0
	for common < len(ar) && common < len(br) && ar[common] == br[common] {
		common++
	}
	for i := min(common, len(ar)-1, len(br)-1); i > 0; i-- {
		if ar[i-1] == '+' || unicode.IsUpper(ar[i]) && unicode.IsUpper(br[i]) {
			return len(string(br[:i]))
		}
	}
	return 0
}

// CatalogText é o texto key do catálogo com as teclas do mapa padrão, como a TUI
// o mostra. Serve a quem confere a tela de fora do pacote.
func CatalogText(cat *i18n.Catalog, key string) string {
	return defaultKeys().expandKeys(cat.T(key), cat)
}
