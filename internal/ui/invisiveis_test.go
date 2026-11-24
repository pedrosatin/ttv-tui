package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

// O ticket 26 trata os invisíveis de formatação que o seg-21 adiou, e eles saem
// da tela, da cópia e do filtro como as marcas bidi e o CGJ. Na base,
// "an\u200btes" aparecia como "antes" e não casava com o filtro "antes", e o
// U+00AD media 0 célula no layout e 1 no wcwidth da glibc.
func TestInvisiveisDeFormatacaoSaem(t *testing.T) {
	for _, r := range []rune{'\ufeff', '\u200b', '\u2060', '\u180e', '\u00ad'} {
		t.Run(fmt.Sprintf("U+%04X", r), func(t *testing.T) {
			texto := "an" + string(r) + "tes"
			if got := sanitizeScreenText(texto); got != "antes" {
				t.Errorf("tela = %q, want %q", got, "antes")
			}
			msg := ChatMessage{Kind: chat.MessageChat, User: "antes", DisplayName: texto, Body: texto}
			if got := formatChatCopyText(msg); got != "antes: antes" {
				t.Errorf("cópia = %q, want %q", got, "antes: antes")
			}
			if !filterContains(texto, filterQuery("antes")) {
				t.Errorf("filtro %q não casa com %q", "antes", texto)
			}
			if got := filterQuery(texto); got != "antes" {
				t.Errorf("filtro digitado = %q, want %q", got, "antes")
			}
		})
	}
}

// Os preenchimentos Hangul viram espaço porque a tela os desenha como célula
// vazia e o layout discorda do wcwidth da glibc na largura, que é 2 contra 0 em
// U+3164 e 1 contra 0 em U+FFA0. O espaço ASCII mede 1 nas duas contas.
func TestPreenchimentosHangulViramEspaco(t *testing.T) {
	for _, r := range []rune{'\u3164', '\u115f', '\uffa0'} {
		t.Run(fmt.Sprintf("U+%04X", r), func(t *testing.T) {
			texto := "an" + string(r) + "tes"
			got := sanitizeScreenText(texto)
			if got != "an tes" {
				t.Errorf("tela = %q, want %q", got, "an tes")
			}
			if w := ansi.StringWidth(got); w != 6 {
				t.Errorf("largura = %d, want 6", w)
			}
			if !filterContains(texto, filterQuery("an tes")) {
				t.Errorf("filtro %q não casa com %q", "an tes", texto)
			}
		})
	}
}

// Um nome feito só de U+3164 não aparece vazio. O chat, a cópia e o canal usam
// o login, e a categoria mostra "(sem nome)".
func TestNomeSoDePreenchimentoNaoFicaVazio(t *testing.T) {
	vazio := strings.Repeat("\u3164", 3)

	m, _, _ := setupTwoPanelModel(t)
	m = resize(m, 80, 24)
	msg := ChatMessage{Kind: chat.MessageChat, User: "fantasma", DisplayName: vazio, Body: "oi"}
	lead, content := m.formatChatMessageParts(msg, false)
	if got := ansi.Strip(lead + content); !strings.Contains(got, "fantasma: oi") {
		t.Errorf("chat = %q, want o login no lugar do nome", got)
	}
	if got := formatChatCopyText(msg); got != "fantasma: oi" {
		t.Errorf("cópia = %q, want %q", got, "fantasma: oi")
	}

	lm := newClassicModel(testCatalog(t, i18n.LangPTBR))
	ch := catalog.Channel{ID: "1", Login: "fantasma", DisplayName: vazio}
	if got := lm.channelIdentity(ch); got != "fantasma" {
		t.Errorf("canal = %q, want %q", got, "fantasma")
	}
	semLogin := catalog.Channel{ID: "2", DisplayName: vazio}
	if got := lm.channelIdentity(semLogin); got != lm.t("list.unnamed") {
		t.Errorf("canal sem login = %q, want %q", got, lm.t("list.unnamed"))
	}
	if got := lm.categoryLabel(catalog.Category{ID: "c", Name: vazio}); got != "(sem nome)" {
		t.Errorf("categoria = %q, want %q", got, "(sem nome)")
	}
	if got := lm.categoryLabel(catalog.Category{ID: "c", Name: "Just Chatting"}); got != "Just Chatting" {
		t.Errorf("categoria com nome = %q", got)
	}
}

const (
	bandeiraInglaterra = "\U0001F3F4\U000E0067\U000E0062\U000E0065\U000E006E\U000E0067\U000E007F"
	bandeiraEscocia    = "\U0001F3F4\U000E0067\U000E0062\U000E0073\U000E0063\U000E0074\U000E007F"
	bandeiraGales      = "\U0001F3F4\U000E0067\U000E0062\U000E0077\U000E006C\U000E0073\U000E007F"
	tagsGBENG          = "\U000E0067\U000E0062\U000E0065\U000E006E\U000E0067"
)

// Uma bandeira de subdivisão é U+1F3F4 seguido das tags do código e de U+E007F,
// e passa inteira. Antes a faixa de U+E0000 a U+E007F saía toda, e a bandeira
// da Inglaterra virava 🏴.
func TestSanitizeChatTextPreservaBandeiraDeSubdivisao(t *testing.T) {
	for _, texto := range []string{
		bandeiraInglaterra,
		"go " + bandeiraEscocia + "!",
		bandeiraInglaterra + bandeiraEscocia,
		bandeiraGales,
		"\U0001F3F4\U000E0075\U000E0073\U000E0074\U000E0078\U000E007F",
		"\U0001F3F4\U000E0061\U000E0062\U000E0031\U000E0032\U000E0033\U000E0034\U000E007F",
	} {
		if got := sanitizeChatText(texto); got != texto {
			t.Errorf("sanitizeChatText(%q) = %q", texto, got)
		}
		if got := sanitizeScreenText(texto); got != texto {
			t.Errorf("sanitizeScreenText(%q) = %q", texto, got)
		}
	}
	if !filterContains("go "+bandeiraInglaterra, filterQuery(bandeiraInglaterra)) {
		t.Error("o filtro com a bandeira não casa com o título que a mostra")
	}
}

// Fora de uma bandeira válida as tags continuam saindo, inclusive numa sequência
// longa demais para um código de subdivisão, que esconderia texto na cópia.
func TestSanitizeChatTextRemoveTagsSoltas(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"depois de letra", "a" + tagsGBENG + "\U000E007Fb", "ab"},
		{"depois de outro emoji", "\U0001F3F3" + tagsGBENG + "\U000E007F", "\U0001F3F3"},
		{"sem terminador", "\U0001F3F4" + tagsGBENG + " x", "\U0001F3F4 x"},
		{"terminador sozinho", "\U0001F3F4\U000E007F", "\U0001F3F4"},
		{"tag fora de 0-9 a-z", "\U0001F3F4\U000E0041\U000E007F", "\U0001F3F4"},
		{"tag de idioma", "\U0001F3F4\U000E0001" + tagsGBENG + "\U000E007F", "\U0001F3F4"},
		{"tag solta depois da bandeira", bandeiraInglaterra + "\U000E0067", bandeiraInglaterra},
		{"duplicador de mensagem", "oi \U000E0000", "oi "},
		{"200 tags", "\U0001F3F4" + strings.Repeat("\U000E0061", 200) + "\U000E007F", "\U0001F3F4"},
		{"8 tags", "\U0001F3F4" + strings.Repeat("\U000E0061", 8) + "\U000E007F", "\U0001F3F4"},
		{"7 tags", "\U0001F3F4" + strings.Repeat("\U000E0061", 7) + "\U000E007F", "\U0001F3F4"},
		{"região com dígito", "\U0001F3F4\U000E0067\U000E0031\U000E0065\U000E007F", "\U0001F3F4"},
		{"só a região", "\U0001F3F4\U000E0067\U000E0062\U000E007F", "\U0001F3F4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeChatText(tc.in); got != tc.want {
				t.Errorf("sanitizeChatText(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// Quando um ZWJ descarta a bandeira de subdivisão que vem depois dele, as tags
// dela saem junto e nenhuma fica solta na tela. As tags de uma bandeira mantida
// não interrompem a sequência ZWJ que começa nela.
func TestSanitizeScreenTextDescartaTagsDaBandeiraJuntada(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"depois de mulher", "\U0001F469\u200d" + bandeiraInglaterra, "\U0001F469"},
		{"depois de bandeira branca", "\U0001F3F3\ufe0f\u200d" + bandeiraInglaterra, "\U0001F3F3"},
		{"bandeira mantida antes de outra juntada", bandeiraEscocia + " \U0001F469\u200d" + bandeiraInglaterra, bandeiraEscocia + " \U0001F469"},
		{"ZWJ depois de bandeira mantida", bandeiraEscocia + "\u200d\U0001F469 x", bandeiraEscocia + " x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeScreenText(tc.in)
			if got != tc.want {
				t.Errorf("sanitizeScreenText(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if strings.Count(got, "\U000E007F") != strings.Count(got, "\U0001F3F4") {
				t.Errorf("tag solta em %q", got)
			}
		})
	}
}

// Uma categoria aberta de nome só de U+3164 aparece como "(sem nome)" no
// cabeçalho e no título da seção, e um título de canal só de U+3164 não deixa o
// separador do título seguido de espaços.
func TestCategoriaETituloSoDePreenchimentoNaLista(t *testing.T) {
	m := newClassicModel(testCatalog(t, i18n.LangPTBR))
	m.list.mode = ModeChannels
	m.list.selectedCategory = &catalog.Category{ID: "cat-vazia", Name: "\u3164\u3164"}
	want := fmt.Sprintf(m.t("channels.title_category"), "(sem nome)")
	if got := ansi.Strip(m.headerLine(80)); !strings.Contains(got, want) {
		t.Errorf("cabeçalho = %q, want %q", got, want)
	}
	if got, ok := m.listSectionTitle(ItemTypeChannel, false); !ok || got != want {
		t.Errorf("título da seção = %q, want %q", got, want)
	}

	ch := catalog.Channel{ID: "1", Login: "fantasma", DisplayName: "Fantasma", Title: catalog.StringPtr("\u3164\u3164"), Availability: catalog.AvailabilityLive}
	row := ansi.Strip(m.formatChannelRow(" ", ch, catalog.AvailabilityLive, 80, false))
	if strings.Contains(row, listRowEmDash) || strings.Contains(row, " - ") {
		t.Errorf("linha = %q, want sem separador de título", row)
	}
}

// A colisão de nomes truncados compara a categoria pelo rótulo que a tela
// mostra mesmo quando o nome vem vazio.
func TestColisaoDeCategoriaSemNomeUsaORotulo(t *testing.T) {
	m := newClassicModel(testCatalog(t, i18n.LangPTBR))
	got, ok := m.listItemCollisionLabel(ListItem{Type: ItemTypeCategory, Category: &catalog.Category{ID: "c"}})
	if !ok || got != "(sem nome)" {
		t.Errorf("rótulo = %q, %v, want %q", got, ok, "(sem nome)")
	}
}

// O aviso de sub e o de presente para a comunidade mostram o login de quem tem
// o nome só de U+3164, e a mensagem comum não deixa o autor em branco.
func TestAvisoDeSubComNomeSoDePreenchimento(t *testing.T) {
	m := deliver(t, joinChatFixture(t, i18n.LangPTBR), ChatMessage{
		Generation: 1, Kind: chat.MessageSystem, UserNotice: true, ID: "n-sub",
		User: "fantasma", DisplayName: "\u3164", MsgID: "sub",
	})
	if row := renderedRow(m); !strings.Contains(row, fmt.Sprintf(m.t("chat.usernotice.sub"), "fantasma")) {
		t.Errorf("aviso de sub = %q, want o login", row)
	}

	m = deliver(t, joinChatFixture(t, i18n.LangPTBR), ChatMessage{
		Generation: 1, Kind: chat.MessageSystem, UserNotice: true, ID: "n-gift",
		User: "fantasma", DisplayName: "\u3164", MsgID: "submysterygift", CommunityGiftID: "g1", GiftCount: 5,
	})
	if row := renderedRow(m); !strings.Contains(row, fmt.Sprintf(m.t("chat.usernotice.gift_community"), "fantasma", 5)) {
		t.Errorf("presente = %q, want o login", row)
	}

	cm := joinChatFixture(t, i18n.LangPTBR)
	_, content := cm.formatChatMessageParts(ChatMessage{Kind: chat.MessageChat, User: "\u3164", DisplayName: "\u3164", Body: "oi"}, false)
	if got := ansi.Strip(content); strings.Contains(got, " : oi") {
		t.Errorf("chat sem nome nem login visível = %q, want sem autor em branco", got)
	}
	if got := noticeAuthor("\u3164", "\u3164"); got != "?" {
		t.Errorf("sem nome e sem login = %q, want %q", got, "?")
	}
}
