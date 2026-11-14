//go:build cenarios && linux

package windowscenarios

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/fixture"
	"github.com/pedrosatin/ttv-tui/internal/ui"
)

// emojiTitle é um título de live como os da Twitch, cheio de emojis.
const emojiTitle = "🔴🎮🔥😂🏆👀🎉✨💜🚀🤣😎🙏💯 maratona"

// emojiCatalog é o catálogo padrão com um canal a mais no topo da Categoria 01,
// de nome curto e título cheio de emojis.
func emojiCatalog() fixture.Catalog {
	cat := fixture.DefaultCatalog()
	first := &cat.Categories[0]
	first.Channels = append([]fixture.Channel{{Login: "emojis", Name: "emojis", Title: emojiTitle, Language: "pt", Viewers: 9000}}, first.Channels...)
	return cat
}

// W12: os emojis do título aparecem com a fonte de emoji do sistema, e a
// janela não conta glifo sem fonte. Antes cada emoji virava um quadrado vazio.
func TestJanelaEmojiDoTituloTemFonte(t *testing.T) {
	t.Parallel()
	cat := emojiCatalog()
	w := openWindow(t, setup{catalog: &cat, config: "list_columns = 44\n"})
	w.press("Return")
	w.waitState("os canais da Categoria 01", 10*time.Second, func(s ui.StateReport) bool {
		return s.Mode == string(ui.ModeChannels) && s.Highlighted == "emojis"
	})
	w.waitScreen("🔴", 5*time.Second)

	expect(t, func() error {
		var stats struct{ Tofu int }
		if err := json.Unmarshal([]byte(w.must("stats")), &stats); err != nil {
			return err
		}
		screen := w.screen()
		row := rowWith(screen, "🔴")
		if row < 0 {
			return fmt.Errorf("o título com emojis saiu da tela")
		}
		shown := 0
		for _, r := range emojiTitle {
			if r >= 0x2190 && strings.ContainsRune(screen[row], r) {
				shown++
			}
		}
		if shown < 5 {
			return fmt.Errorf("só %d emojis do título chegaram à tela: %q", shown, screen[row])
		}
		if stats.Tofu > 0 {
			return fmt.Errorf("%d glifos sem fonte com %d emojis na tela: %q", stats.Tofu, shown, screen[row])
		}
		return nil
	})

	// Com os emojis na lista, o vídeo segue chegando.
	w.press("Return")
	w.waitState("o player tocando emojis", 15*time.Second, func(s ui.StateReport) bool {
		return s.Player == string(ui.PlayerStatePlaying)
	})
	w.must("stats", "reset")
	expect(t, func() error {
		var stats struct{ VideoFrames, Tofu int }
		if err := json.Unmarshal([]byte(w.must("stats")), &stats); err != nil {
			return err
		}
		if stats.VideoFrames < 5 || stats.Tofu > 0 {
			return fmt.Errorf("%d quadros de vídeo desde o reset, tofu %d", stats.VideoFrames, stats.Tofu)
		}
		return nil
	})
}

// statusBar é a última linha da grade: a barra de status.
func statusBar(screen []string) string {
	if len(screen) == 0 {
		return ""
	}
	return screen[len(screen)-1]
}

// W11: m liga e desliga o mudo, e a barra diz "mudo" no lugar de "vol 100". A
// janela dos cenários abre o mpv já mudo (TTV_WINDOW_MUTE), como pede a bateria.
func TestJanelaTeclaMAlternaOMudo(t *testing.T) {
	t.Parallel()
	w := openWindow(t, setup{irc: fixture.IRCConfig{Messages: 1}})
	w.openFirstChannel()
	w.waitState("o player mudo desde a abertura", 5*time.Second, func(s ui.StateReport) bool { return s.Muted })
	barSays := func(want, not string) func() error {
		return func() error {
			bar := statusBar(w.screen())
			if !strings.Contains(bar, want) || strings.Contains(bar, not) {
				return fmt.Errorf("barra %q: quer %q e não %q", bar, want, not)
			}
			return nil
		}
	}
	expect(t, barSays("mudo", "vol 100"))

	w.press("m")
	w.waitState("o som de volta", 5*time.Second, func(s ui.StateReport) bool { return !s.Muted && s.Volume == 100 })
	expect(t, barSays("vol 100", "mudo"))

	w.press("m")
	w.waitState("o mudo de novo", 5*time.Second, func(s ui.StateReport) bool { return s.Muted })
	expect(t, barSays("mudo", "vol 100"))
}

// W10: a área do vídeo diz o que Enter faz com o item destacado: na tela
// inicial, com uma categoria, abre a categoria; dentro dela, com um canal,
// assiste ao canal.
func TestJanelaTelaInicialFalaDoDestacado(t *testing.T) {
	t.Parallel()
	w := openWindow(t, setup{})
	w.waitScreen("Enter abre a categoria", 5*time.Second)
	if row := rowWith(w.screen(), "selecionado"); row >= 0 {
		t.Fatalf("a tela inicial ainda fala do selecionado: %q", w.screen()[row])
	}
	w.press("Return")
	w.waitState("os canais da Categoria 01", 10*time.Second, func(s ui.StateReport) bool {
		return s.Mode == string(ui.ModeChannels) && s.Highlighted == "FixtureUm"
	})
	w.waitScreen("Enter assiste a FixtureUm", 5*time.Second)
}

// W9: enquanto o streamlink procura a live, a área do vídeo diz "Abrindo
// <canal>…" em vez de ficar preta: a janela só abre o buraco do vídeo com o
// primeiro quadro.
func TestJanelaCarregandoMostraOCanal(t *testing.T) {
	t.Parallel()
	w := openWindow(t, setup{env: []string{fixture.OpenDelayEnv + "=3s"}})
	w.press("Return")
	w.waitState("os canais da Categoria 01", 10*time.Second, func(s ui.StateReport) bool {
		return s.Mode == string(ui.ModeChannels) && s.Highlighted == "FixtureUm"
	})
	w.press("Return")
	w.waitState("o player abrindo", 5*time.Second, func(s ui.StateReport) bool {
		return s.Player == string(ui.PlayerStateStarting)
	})
	expect(t, func() error {
		st, err := w.state()
		if err != nil {
			return err
		}
		if st.TUI.Player != string(ui.PlayerStateStarting) {
			return fmt.Errorf("o player saiu de starting (%s) antes de a tela ser lida", st.TUI.Player)
		}
		if st.Window.HoleOn {
			return fmt.Errorf("a janela abriu o buraco do vídeo antes do primeiro quadro")
		}
		if rowWith(w.screen(), "Abrindo fixture_um…") < 0 {
			return fmt.Errorf("a área do vídeo não diz que está abrindo fixture_um:\n%s", strings.Join(w.screen(), "\n"))
		}
		return nil
	})

	w.waitState("o player tocando", 15*time.Second, func(s ui.StateReport) bool {
		return s.Player == string(ui.PlayerStatePlaying)
	})
	expect(t, func() error {
		st, err := w.state()
		if err != nil {
			return err
		}
		if !st.Window.HoleOn {
			return fmt.Errorf("o vídeo toca e a janela não abriu o buraco")
		}
		if row := rowWith(w.screen(), "Abrindo"); row >= 0 {
			return fmt.Errorf("o aviso de abertura ficou na grade com o vídeo tocando: %q", w.screen()[row])
		}
		return nil
	})
}

// cellBrightness lê o canto de uma célula do painel e soma R, G e B.
func (w *window) cellBrightness(col, row int) (int, error) {
	out, err := w.send("pixel", strconv.Itoa(col), strconv.Itoa(row))
	if err != nil {
		return 0, err
	}
	var b, g, r, a, top int
	if n, _ := fmt.Sscan(out, &b, &g, &r, &a, &top); n != 5 {
		return 0, fmt.Errorf("pixel respondeu %q", strings.TrimSpace(out))
	}
	return r + g + b, nil
}

// selectedListRow é a linha da grade do item destacado na lista: a primeira
// depois do título que começa com ▸.
func selectedListRow(screen []string) int {
	for i := 1; i < len(screen); i++ {
		if strings.HasPrefix(screen[i], "▸") {
			return i
		}
	}
	return -1
}

// W15: com a lista em 25 colunas, o título com foco não mostra "[fo…": o rótulo
// entra inteiro ou fica de fora. Com o foco no chat, a marca vai para o título
// do chat e a seleção da lista fica mais escura que com a lista em foco.
func TestJanelaFocoClaroEmColunaEstreita(t *testing.T) {
	t.Parallel()
	w := openWindow(t, setup{config: "list_columns = 25\n", irc: fixture.IRCConfig{Messages: 1}})
	w.openFirstChannel()
	w.press("ctrl+r")
	w.waitState("as categorias com o chat ao lado", 10*time.Second, func(s ui.StateReport) bool {
		return s.Mode == string(ui.ModeCategories) && s.ChatVisible && s.Focus == string(ui.FocusList)
	})

	var focused int
	expect(t, func() error {
		screen := w.screen()
		head := screen[0]
		if !strings.HasPrefix(head, "▸ ") {
			return fmt.Errorf("título da lista com foco sem ▸: %q", head)
		}
		if strings.Contains(head, "[") && !strings.Contains(head, "[foco]") {
			return fmt.Errorf("rótulo de foco cortado: %q", head)
		}
		row := selectedListRow(screen)
		if row < 0 {
			return fmt.Errorf("nenhuma linha destacada na lista:\n%s", strings.Join(screen, "\n"))
		}
		var err error
		focused, err = w.cellBrightness(3, row)
		return err
	})

	w.press("Tab")
	w.waitState("o foco no chat", 5*time.Second, func(s ui.StateReport) bool { return s.Focus == string(ui.FocusChat) })
	expect(t, func() error {
		screen := w.screen()
		if strings.HasPrefix(screen[0], "▸") {
			return fmt.Errorf("o título da lista segue marcado com o foco no chat: %q", screen[0])
		}
		if rowWith(screen, "▸ Chat — #fixture_um") < 0 {
			return fmt.Errorf("o título do chat com foco sem ▸:\n%s", strings.Join(screen, "\n"))
		}
		row := selectedListRow(screen)
		if row < 0 {
			return fmt.Errorf("a seleção da lista sumiu com o foco no chat")
		}
		dim, err := w.cellBrightness(3, row)
		if err != nil {
			return err
		}
		t.Logf("brilho da seleção: com foco %d, fora de foco %d", focused, dim)
		if dim+90 > focused {
			return fmt.Errorf("seleção fora de foco com brilho %d, com foco %d: quer bem mais escura", dim, focused)
		}
		return nil
	})
}
