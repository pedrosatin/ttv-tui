package ui

// Custo de uma tecla na janela única: o Update e a tela inteira que a TUI
// redesenha em seguida, com uma lista longa de títulos com emoji e o chat cheio,
// no tamanho de uma janela de monitor grande.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

const benchWidth, benchHeight = 182, 48

// benchChannels são 60 canais ao vivo com emoji e acento no título, como numa
// categoria grande da Twitch.
func benchChannels() []catalog.Channel {
	lang := "pt"
	titles := []string{
		"🔴 AO VIVO — ranked até o topo 🏆🔥 !sorteio",
		"Just chatting ☕ conversa e react 😂👀",
		"CAMPEONATO 🇧🇷 final hoje ⚽ !pix",
		"speedrun any% 🎮 tentativa nº 4012 😤",
	}
	out := make([]catalog.Channel, 60)
	for i := range out {
		title := titles[i%len(titles)]
		out[i] = catalog.Channel{
			ID: fmt.Sprintf("ch-%d", i), Login: fmt.Sprintf("canal%02d", i), DisplayName: fmt.Sprintf("Canal%02d", i),
			Language: &lang, Viewers: catalog.IntPtr(90000 - i*1000), Title: &title,
			Availability: catalog.AvailabilityLive,
		}
	}
	return out
}

// benchRun roda cmd e o que ele gerar, como o settle dos cenários, mas para um
// benchmark.
func benchRun(tb testing.TB, m Model, cmd tea.Cmd) Model {
	tb.Helper()
	queue := []tea.Cmd{cmd}
	for n := 0; len(queue) > 0; n++ {
		if n > maxScenarioMsgs {
			tb.Fatalf("mais de %d mensagens", maxScenarioMsgs)
		}
		next := queue[0]
		queue = queue[1:]
		if next == nil {
			continue
		}
		done := make(chan tea.Msg, 1)
		go func() { done <- next() }()
		var msg tea.Msg
		select {
		case msg = <-done:
		case <-time.After(cmdDeadline):
			tb.Fatalf("um comando não voltou em %s", cmdDeadline)
		}
		if cmds, ok := batchedCmds(msg); ok {
			queue = append(queue, cmds...)
			continue
		}
		switch msg.(type) {
		case nil, timerFiredMsg:
			continue
		case tea.QuitMsg:
			return m
		}
		var more tea.Cmd
		m, more = pressKeyMsg(m, msg)
		queue = append(queue, more)
	}
	return m
}

func benchKey(tb testing.TB, m Model, name string) Model {
	tb.Helper()
	keys, err := parseKeys(name)
	if err != nil {
		tb.Fatal(err)
	}
	for _, k := range keys {
		var cmd tea.Cmd
		m, cmd = pressKey(m, k)
		m = benchRun(tb, m, cmd)
	}
	return m
}

// benchHostedModel é a janela única com um canal tocando, a lista de 60 canais
// e 300 mensagens no chat.
func benchHostedModel(tb testing.TB) Model {
	tb.Helper()
	exactCellWidths = true
	tb.Cleanup(func() { exactCellWidths = false })
	cat, err := i18n.New(i18n.LangPTBR)
	if err != nil {
		tb.Fatal(err)
	}
	src := &fakeSource{
		categoriesFn: func(ctx context.Context, page catalog.PageRequest) (catalog.Page[catalog.Category], error) {
			return catalog.Page[catalog.Category]{Items: scenarioCategories()}, nil
		},
		channelsFn: func(ctx context.Context, categoryID string, page catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
			return catalog.Page[catalog.Channel]{Items: benchChannels()}, nil
		},
	}
	m := New(cat, WithSource(src), WithPlayback(&fakePlayback{}),
		WithChat(&fakeChat{hasToken: true, username: "espectador"}), WithVideoHost(&recordingHost{}))
	m.timer = scenarioTimer
	m = benchRun(tb, m, func() tea.Msg { return tea.WindowSizeMsg{Width: benchWidth, Height: benchHeight} })
	m = benchRun(tb, m, m.Init())
	m = benchKey(tb, m, "enter enter")
	login := m.chat.panel.channel
	m = benchRun(tb, m, func() tea.Msg {
		return PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: login, Volume: 100}
	})
	gen := m.chat.panel.gen
	m = benchRun(tb, m, func() tea.Msg { return ChatEventMsg{Generation: gen, Channel: login, State: ChatStateConnected} })
	bodies := []string{"KEKW que jogada 😂😂😂", "boa noite chat 👋", "🔥🔥🔥 GG", "alguém sabe a música? 🎵 tá muito boa essa", "PogChamp"}
	msgs := make([]ChatMessage, 300)
	for i := range msgs {
		msgs[i] = ChatMessage{
			Generation: gen, Kind: chat.MessageChat, ID: fmt.Sprintf("m%d", i),
			User: fmt.Sprintf("user%d", i), DisplayName: fmt.Sprintf("User%d", i), Body: bodies[i%len(bodies)],
		}
	}
	m = benchRun(tb, m, func() tea.Msg { return ChatEventMsg{Generation: gen, Channel: login, Messages: msgs} })
	if !strings.Contains(m.View(), "User299") || len(m.VisibleItems()) < 60 {
		tb.Fatalf("o modelo do benchmark não montou a cena:\n%s", m.View())
	}
	return m
}

// BenchmarkHostedView é a tela inteira redesenhada sem mudança no modelo.
func BenchmarkHostedView(b *testing.B) {
	for _, focus := range []string{"lista", "chat"} {
		b.Run(focus, func(b *testing.B) {
			m := benchHostedModel(b)
			if focus == "chat" {
				m = benchKey(b, m, "tab")
			}
			for b.Loop() {
				_ = m.View()
			}
		})
	}
}

// BenchmarkHostedKey é uma tecla e a tela que vem dela, em ida e volta para o
// modelo terminar onde começou.
func BenchmarkHostedKey(b *testing.B) {
	for _, pair := range [][2]string{{"down", "up"}, {"ctrl+right", "ctrl+left"}} {
		b.Run(pair[0], func(b *testing.B) {
			m := benchHostedModel(b)
			i := 0
			for b.Loop() {
				m = benchKey(b, m, pair[i%2])
				_ = m.View()
				i++
			}
		})
	}
}
