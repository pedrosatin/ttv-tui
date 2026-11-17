package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
)

// overlayMinimo abre gaules na janela com o chat sobreposto, com o vídeo
// tocando e duas mensagens no chat, e baixa o overlay até a altura mínima.
func overlayMinimo(t *testing.T, width, height int, hasToken bool) Model {
	t.Helper()
	m, w := hostedScenarioClassic(t, width, height)
	w.chat.hasToken = hasToken
	m = play(t, m, "ctrl+f gaules enter enter ctrl+o")
	m = feed(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
	m = chatLines(t, m, [2]string{"Fulano", "salve, chat"}, [2]string{"Ciclana", "que jogada"})
	available := m.height - 1 - m.footerRows()
	for range available {
		m = play(t, m, "alt+down")
	}
	if got := m.overlayHeight(available); got != min(6, available) {
		t.Fatalf("o overlay parou em %d linhas, quero a mínima", got)
	}
	return m
}

// chatVisivel diz se a mensagem aparece inteira na tela.
func chatVisivel(m Model, texto string) bool {
	return strings.Contains(screen(m), texto)
}

// Na altura mínima, o overlay mostra o cabeçalho, as mensagens mais novas e a
// entrada.
func TestCenarioChatSobrepostoMinimo(t *testing.T) {
	eachSize(t, "chat-sobreposto-minimo", func(t *testing.T, width, height int) Model {
		m := overlayMinimo(t, width, height, true)
		if !chatVisivel(m, "Ciclana: que jogada") {
			t.Fatalf("a mensagem mais nova sumiu do overlay mínimo:\n%s", screen(m))
		}
		return m
	})
}

// Um espectador sem login aperta i com o overlay na altura mínima: o aviso de
// como entrar ocupa uma linha só, com reticências, e as mensagens continuam ali.
func TestCenarioChatSobrepostoMinimoComAviso(t *testing.T) {
	eachSize(t, "chat-sobreposto-minimo-aviso", func(t *testing.T, width, height int) Model {
		m := overlayMinimo(t, width, height, false)
		m = play(t, m, "i")
		if m.chat.panel.sendWarning != m.t("chat.no_token") {
			t.Fatalf("aviso = %q, quero o de login", m.chat.panel.sendWarning)
		}
		if !chatVisivel(m, "Ciclana: que jogada") {
			t.Fatalf("o aviso tomou o lugar das mensagens:\n%s", screen(m))
		}
		if !strings.Contains(screen(m), "! Para enviar") || !strings.Contains(screen(m), "…") {
			t.Fatalf("o aviso devia caber numa linha, com reticências:\n%s", screen(m))
		}
		return m
	})
}

// Escrevendo com um aviso longo (comando bloqueado), o overlay mínimo ainda
// mostra uma mensagem: o aviso vira uma linha e a dica de envio fica.
func TestChatSobrepostoMinimoEscrevendoComAvisoLongo(t *testing.T) {
	for _, size := range scenarioSizes {
		m := overlayMinimo(t, size.width, size.height, true)
		m = play(t, m, "i /ban enter")
		if m.chat.panel.sendWarning != m.t("chat.warn.command_blocked") {
			t.Fatalf("%dx%d: aviso = %q", size.width, size.height, m.chat.panel.sendWarning)
		}
		if !chatVisivel(m, "Ciclana: que jogada") {
			t.Fatalf("%dx%d: nenhuma mensagem com o aviso longo:\n%s", size.width, size.height, screen(m))
		}
		if !strings.Contains(screen(m), "4/500") {
			t.Fatalf("%dx%d: a dica com o contador sumiu:\n%s", size.width, size.height, screen(m))
		}
	}
}

// Num painel apertado demais até para o aviso em uma linha, o cabeçalho fica só
// com a primeira linha e a dica de envio sai antes da última mensagem.
func TestChatPainelCurtoCedeAntesDasMensagens(t *testing.T) {
	m := overlayMinimo(t, 80, 24, true)
	m = play(t, m, "i /ban enter")
	m = feed(t, m, ChatEventMsg{Generation: m.chat.panel.gen, Channel: m.chat.panel.channel, State: ChatStateError, ErrText: "falha longa o bastante para ocupar duas linhas"})
	_, chatW, _ := m.hostedWidths()
	lines := m.renderChatLines(4, chatW)
	got := ansi.Strip(strings.Join(lines, "\n"))
	if len(lines) != 4 || !strings.Contains(got, "Ciclana: que jogada") {
		t.Fatalf("painel de 4 linhas sem a mensagem mais nova:\n%s", got)
	}
	if strings.Contains(got, "4/500") || !strings.Contains(got, "! Não enviado") || !strings.Contains(got, "Chat — #gaules") {
		t.Fatalf("a dica devia ceder antes da mensagem, do aviso e do título:\n%s", got)
	}
	if strings.Contains(got, "falha longa") {
		t.Fatalf("a segunda linha do cabeçalho devia ceder antes da mensagem:\n%s", got)
	}
}

// O aviso inteiro volta quando o painel tem espaço: a linha com reticências é
// só a forma do overlay reduzido.
func TestChatAvisoInteiroNoPainelMaior(t *testing.T) {
	m := overlayMinimo(t, 80, 24, false)
	m = play(t, m, "i")
	_, chatW, _ := m.hostedWidths()
	short := ansi.Strip(strings.Join(m.renderChatLines(6, chatW), "\n"))
	tall := ansi.Strip(strings.Join(m.renderChatLines(20, chatW), "\n"))
	if strings.Contains(short, "twt)") {
		t.Fatalf("no painel mínimo o aviso devia caber numa linha:\n%s", short)
	}
	if !strings.Contains(tall, "twt)") || !strings.Contains(tall, "Ciclana: que jogada") {
		t.Fatalf("no painel maior o aviso devia aparecer inteiro:\n%s", tall)
	}
}

// timerCapturado guarda o temporizador agendado pelo modelo em vez de dispará-lo.
type timerCapturado struct {
	d  time.Duration
	fn func(time.Time) tea.Msg
}

func (c *timerCapturado) timer(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
	c.d, c.fn = d, fn
	return func() tea.Msg { return nil }
}

// Quem não pode enviar não tem o que fazer com o aviso: ele sai sozinho depois
// de sendWarningTTL pelo relógio do modelo, e a linha de entrada continua
// dizendo por que o envio está desligado.
func TestAvisoDeEnvioExpiraParaQuemNaoPodeEnviar(t *testing.T) {
	base := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	now := base
	m := overlayMinimo(t, 80, 24, false)
	m.chat.nowFunc = func() time.Time { return now }
	var capt timerCapturado
	m.timer = capt.timer

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = next.(Model)
	if cmd == nil || capt.fn == nil || capt.d != sendWarningTTL {
		t.Fatalf("o aviso de quem não pode enviar devia agendar a saída em %s (agendou %s)", sendWarningTTL, capt.d)
	}
	expira := capt.fn(now)

	// O temporizador que chega antes da hora, pelo relógio injetado, não apaga.
	next, _ = m.Update(expira)
	m = next.(Model)
	if m.chat.panel.sendWarning == "" {
		t.Fatal("o aviso saiu antes de sendWarningTTL")
	}

	now = base.Add(sendWarningTTL)
	next, _ = m.Update(expira)
	m = next.(Model)
	if m.chat.panel.sendWarning != "" {
		t.Fatalf("o aviso ficou depois de sendWarningTTL: %q", m.chat.panel.sendWarning)
	}
	if !strings.Contains(screen(m), "> Para enviar") {
		t.Fatalf("a entrada devia continuar dizendo como enviar:\n%s", screen(m))
	}
}

// Um aviso novo invalida o temporizador do anterior: o tique velho não apaga o
// aviso que chegou depois dele.
func TestAvisoDeEnvioTiqueAntigoNaoApagaONovo(t *testing.T) {
	base := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	now := base
	m := overlayMinimo(t, 80, 24, false)
	m.chat.nowFunc = func() time.Time { return now }
	var capt timerCapturado
	m.timer = capt.timer

	m.setSendWarning(m.t("chat.no_token"))
	velho := capt.fn(now)
	now = base.Add(sendWarningTTL / 2)
	m.setSendWarning(m.t("chat.warn.channel_switched"))
	novo := capt.fn(now)

	now = base.Add(sendWarningTTL)
	next, _ := m.Update(velho)
	m = next.(Model)
	if m.chat.panel.sendWarning != m.t("chat.warn.channel_switched") {
		t.Fatalf("o tique do aviso antigo apagou o novo: %q", m.chat.panel.sendWarning)
	}
	now = base.Add(sendWarningTTL/2 + sendWarningTTL)
	next, _ = m.Update(novo)
	if got := next.(Model).chat.panel.sendWarning; got != "" {
		t.Fatalf("o aviso novo devia sair no tempo dele: %q", got)
	}
}

// Para quem pode enviar, o aviso fala do rascunho ou do envio e continua até a
// próxima ação, como antes.
func TestAvisoDeEnvioFicaParaQuemPodeEnviar(t *testing.T) {
	m := overlayMinimo(t, 80, 24, true)
	var capt timerCapturado
	m.timer = capt.timer
	if cmd := m.setSendWarning(m.t("chat.warn.rate_limit")); cmd != nil || capt.fn != nil {
		t.Fatal("o aviso de quem pode enviar não deve expirar sozinho")
	}
	m = m.expireSendWarning(m.chat.warnGen)
	if m.chat.panel.sendWarning == "" {
		t.Fatal("um tique sem prazo apagou o aviso de quem pode enviar")
	}
}

// Um chat só leitura também não envia, com ou sem login: o aviso sai sozinho.
func TestAvisoDeEnvioExpiraEmChatSoLeitura(t *testing.T) {
	m := overlayMinimo(t, 80, 24, true)
	m.chat.panel.caps = catalog.ChatCapabilities{}
	var capt timerCapturado
	m.timer = capt.timer
	if cmd := m.setSendWarning(m.t("chat.warn.reconnected")); cmd == nil || capt.d != sendWarningTTL {
		t.Fatal("o aviso num chat só leitura devia agendar a saída")
	}
}
