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
	actQuit           actionID = "action.quit"
	actCtrlC          actionID = "action.ctrl_c"
	actCloseHelp      actionID = "action.close_help"
	actHelp           actionID = "action.help"
	actHelpAnywhere   actionID = "action.help_anywhere"
	actCategories     actionID = "action.categories"
	actStopVideo      actionID = "action.stop_video"
	actReconnectChat  actionID = "action.reconnect_chat"
	actCycleLayout    actionID = "action.cycle_layout"
	actToggleList     actionID = "action.toggle_list"
	actToggleChat     actionID = "action.toggle_chat"
	actChatOverlay    actionID = "action.chat_overlay"
	actSwapSides      actionID = "action.swap_sides"
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
	actCompose        actionID = "action.compose"
	actCopyMessage    actionID = "action.copy_message"
	actFavorite       actionID = "action.favorite"
)

// keyMode é um contexto de despacho. Uma tecla pode disparar ações diferentes em
// modos diferentes, mas nunca duas no mesmo contexto (veja keyContexts).
type keyMode int

const (
	// modeApp vem antes de tudo, até da ajuda: sair e o aviso do Ctrl+C.
	modeApp keyMode = iota
	// modeHelp é a ajuda aberta, que consome toda tecla.
	modeHelp
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
	// modeNormal é a lista e o chat fora de qualquer campo de texto.
	modeNormal
)

// keyContexts lista, para cada situação da TUI, os modos que o despacho
// consulta. Dentro de um contexto, cada tecla dispara no máximo uma ação.
var keyContexts = [][]keyMode{
	{modeApp, modeHelp},
	{modeApp, modeGlobal, modeWindow, modeSearch},
	{modeApp, modeGlobal, modeWindow, modeFilter},
	{modeApp, modeGlobal, modeWindow, modeMenu},
	{modeApp, modeGlobal, modeWindow, modeCompose},
	{modeApp, modeGlobal, modeWindow, modeNormal},
}

// helpSection agrupa as ações na ajuda. sectionHidden fica fora dela.
type helpSection int

const (
	sectionHidden helpSection = iota
	sectionHelp
	sectionNav
	sectionSearch
	sectionChannelFilter
	sectionLayout
	sectionWindow
	sectionPlayer
	sectionChat
	sectionApp
)

// action é uma entrada do registro: em que modos a ação vale, em que seção da
// ajuda aparece e as teclas padrão, no formato de tea.KeyMsg.String(). A
// primeira tecla é a principal, a que os textos curtos citam.
type action struct {
	ID      actionID
	Modes   []keyMode
	Section helpSection
	Default []string
}

// defaultActions é o mapa de teclas da TUI. As primitivas de edição dos campos
// de texto ficam fora dele, em field_keys.go.
func defaultActions() []action {
	nav := []keyMode{modeNormal, modeHelp, modeMenu}
	return []action{
		// Ctrl+Q é a única tecla que sai. Ctrl+C chega como tecla (o terminal
		// está em modo raw, sem SIGINT) e só explica como sair.
		{ID: actQuit, Modes: []keyMode{modeApp}, Section: sectionApp, Default: []string{"ctrl+q"}},
		{ID: actCtrlC, Modes: []keyMode{modeApp}, Section: sectionHidden, Default: []string{"ctrl+c"}},

		// q só fecha a ajuda; fora dela não faz nada.
		{ID: actCloseHelp, Modes: []keyMode{modeHelp}, Section: sectionHelp, Default: []string{"q"}},
		{ID: actHelp, Modes: []keyMode{modeNormal, modeHelp}, Section: sectionApp, Default: []string{"?"}},
		{ID: actHelpAnywhere, Modes: []keyMode{modeGlobal, modeHelp}, Section: sectionApp, Default: []string{"f1"}},

		{ID: actCategories, Modes: []keyMode{modeGlobal}, Section: sectionNav, Default: []string{"ctrl+r"}},
		{ID: actStopVideo, Modes: []keyMode{modeGlobal}, Section: sectionPlayer, Default: []string{"ctrl+x"}},
		{ID: actReconnectChat, Modes: []keyMode{modeGlobal}, Section: sectionChat, Default: []string{"ctrl+t"}},

		// Ctrl+W cicla os layouts; Ctrl+H e Ctrl+L ligam a lista e o chat. O
		// bubbletea entrega Ctrl+H (BS) e Backspace (DEL) como teclas distintas.
		{ID: actCycleLayout, Modes: []keyMode{modeGlobal}, Section: sectionLayout, Default: []string{"ctrl+w"}},
		{ID: actToggleList, Modes: []keyMode{modeGlobal}, Section: sectionLayout, Default: []string{"ctrl+h"}},
		{ID: actToggleChat, Modes: []keyMode{modeGlobal}, Section: sectionLayout, Default: []string{"ctrl+l"}},

		// Só a janela única tem vídeo ao lado dos painéis. A seta move a divisa:
		// a da lista fica à direita dela, a do chat à esquerda, e cada painel
		// cresce na direção do vídeo.
		{ID: actChatOverlay, Modes: []keyMode{modeWindow}, Section: sectionWindow, Default: []string{"ctrl+o"}},
		{ID: actSwapSides, Modes: []keyMode{modeWindow}, Section: sectionWindow, Default: []string{"ctrl+p"}},
		{ID: actChatMoreOpaque, Modes: []keyMode{modeWindow}, Section: sectionWindow, Default: []string{"alt+up"}},
		{ID: actChatLessOpaque, Modes: []keyMode{modeWindow}, Section: sectionWindow, Default: []string{"alt+down"}},
		{ID: actListWider, Modes: []keyMode{modeWindow}, Section: sectionWindow, Default: []string{"ctrl+right"}},
		{ID: actListNarrower, Modes: []keyMode{modeWindow}, Section: sectionWindow, Default: []string{"ctrl+left"}},
		{ID: actChatWider, Modes: []keyMode{modeWindow}, Section: sectionWindow, Default: []string{"alt+left"}},
		{ID: actChatNarrower, Modes: []keyMode{modeWindow}, Section: sectionWindow, Default: []string{"alt+right"}},

		// O movimento vale na lista, no chat, na ajuda e no menu. No filtro, só
		// as setas andam, e elas são primitivas do campo.
		{ID: actUp, Modes: nav, Section: sectionNav, Default: []string{"up", "k"}},
		{ID: actDown, Modes: nav, Section: sectionNav, Default: []string{"down", "j"}},
		{ID: actPageUp, Modes: []keyMode{modeNormal, modeHelp, modeMenu, modeFilter}, Section: sectionNav, Default: []string{"pgup"}},
		{ID: actPageDown, Modes: []keyMode{modeNormal, modeHelp, modeMenu, modeFilter}, Section: sectionNav, Default: []string{"pgdown"}},
		{ID: actHome, Modes: nav, Section: sectionNav, Default: []string{"home", "g"}},
		// End/G vão ao fim da lista ou retomam o ao vivo no chat.
		{ID: actEnd, Modes: nav, Section: sectionNav, Default: []string{"end", "G"}},
		{ID: actSelect, Modes: []keyMode{modeNormal, modeSearch, modeFilter, modeMenu, modeCompose}, Section: sectionNav, Default: []string{"enter"}},
		{ID: actSwitchFocus, Modes: []keyMode{modeNormal, modeCompose}, Section: sectionNav, Default: []string{"tab"}},

		{ID: actFilter, Modes: []keyMode{modeNormal}, Section: sectionSearch, Default: []string{"/"}},
		// Ctrl+F é a tecla da busca; Ctrl+S, um apelido. O modo raw desliga o
		// controle de fluxo, e o terminal entrega o Ctrl+S em vez de pausar a saída.
		{ID: actSearch, Modes: []keyMode{modeNormal, modeSearch, modeFilter}, Section: sectionSearch, Default: []string{"ctrl+f", "ctrl+s"}},
		{ID: actRetry, Modes: []keyMode{modeNormal}, Section: sectionSearch, Default: []string{"r"}},
		{ID: actChannelFilter, Modes: []keyMode{modeNormal, modeMenu}, Section: sectionChannelFilter, Default: []string{"l"}},
		// p troca a lista para a próxima plataforma; o vídeo e o chat ficam.
		{ID: actSwitchPlatform, Modes: []keyMode{modeNormal}, Section: sectionNav, Default: []string{"p"}},
		{ID: actFavorite, Modes: []keyMode{modeNormal}, Section: sectionNav, Default: []string{"f"}},
		{ID: actBack, Modes: []keyMode{modeNormal, modeHelp, modeSearch, modeFilter, modeMenu, modeCompose}, Section: sectionNav, Default: []string{"esc"}},

		{ID: actPause, Modes: []keyMode{modeNormal, modeMenu}, Section: sectionPlayer, Default: []string{" ", "space"}},
		{ID: actVolumeUp, Modes: []keyMode{modeNormal}, Section: sectionPlayer, Default: []string{"+", "="}},
		{ID: actVolumeDown, Modes: []keyMode{modeNormal}, Section: sectionPlayer, Default: []string{"-", "_"}},

		{ID: actCompose, Modes: []keyMode{modeNormal}, Section: sectionChat, Default: []string{"i"}},
		// y copia a mensagem marcada (ou a última, ao vivo) pelo wl-copy, xclip ou
		// OSC 52 (veja writeClipboard).
		{ID: actCopyMessage, Modes: []keyMode{modeNormal}, Section: sectionChat, Default: []string{"y"}},
	}
}

// keyRegistry responde que ação uma tecla dispara num modo e que teclas uma ação
// tem. As teclas efetivas são as padrão, salvo as que overrides troca.
type keyRegistry struct {
	actions []action
	keys    map[actionID][]string
	byMode  map[keyMode]map[string]actionID
}

// newKeyRegistry monta o registro. overrides troca as teclas de uma ação inteira;
// nil deixa o mapa padrão. Se duas ações dividem uma tecla num modo, vale a que
// vem primeiro na lista: quem aceita teclas de fora confere conflitos antes.
func newKeyRegistry(actions []action, overrides map[actionID][]string) keyRegistry {
	r := keyRegistry{
		actions: actions,
		keys:    make(map[actionID][]string, len(actions)),
		byMode:  make(map[keyMode]map[string]actionID),
	}
	for _, a := range actions {
		keys := a.Default
		if o, ok := overrides[a.ID]; ok {
			keys = o
		}
		r.keys[a.ID] = slices.Clone(keys)
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
		parts = append(parts, l[sharedLabelPrefix(unique[0], l):])
	}
	return strings.Join(parts, "/")
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
