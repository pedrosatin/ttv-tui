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

// Os invisíveis de formatação que o seg-21 adiou (ticket 26) saem da tela, da
// cópia e do filtro, como as marcas bidi e o CGJ. Na base, "an\u200btes"
// aparecia como "antes" e não casava com o filtro "antes", e o U+00AD media 0
// célula no layout e 1 no wcwidth da glibc.
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

// Os preenchimentos Hangul viram espaço: a tela os desenha como célula vazia,
// mas o layout e o wcwidth da glibc discordam da largura (U+3164: 2 contra 0;
// U+FFA0: 1 contra 0). O espaço ASCII mede 1 em todo lugar.
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

// Um nome feito só de U+3164 não aparece vazio: o chat e a cópia usam o login,
// o canal mostra o login e a categoria mostra "(sem nome)".
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
	tagsGBENG          = "\U000E0067\U000E0062\U000E0065\U000E006E\U000E0067"
)

// As bandeiras de subdivisão (U+1F3F4, tags e U+E007F) passam inteiras; antes
// a faixa U+E0000–E007F saía toda e 🏴󠁧󠁢󠁥󠁮󠁧󠁿 virava 🏴.
func TestSanitizeChatTextPreservaBandeiraDeSubdivisao(t *testing.T) {
	for _, texto := range []string{
		bandeiraInglaterra,
		"go " + bandeiraEscocia + "!",
		bandeiraInglaterra + bandeiraEscocia,
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

// Fora de uma bandeira válida, as tags continuam saindo.
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeChatText(tc.in); got != tc.want {
				t.Errorf("sanitizeChatText(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
