//go:build cenarios && linux

package windowscenarios

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/fixture"
	"github.com/pedrosatin/ttv-tui/internal/ui"
)

// O espectador abre a primeira categoria e o primeiro canal: o vídeo aparece na
// coluna do meio e o chat ao lado. Ele põe o chat sobre o vídeo com Ctrl+O e
// deixa o fundo mais opaco com Alt+↑; o config guarda as duas escolhas.
func TestJanelaAbreCanalComVideoEChat(t *testing.T) {
	t.Parallel()
	w := openWindow(t, setup{irc: fixture.IRCConfig{Messages: 5}})
	w.openFirstChannel()

	expect(t, func() error {
		st, err := w.state()
		if err != nil {
			return err
		}
		if !st.Window.HoleOn || len(st.Window.Hole) != 4 {
			return fmt.Errorf("a janela não abriu o buraco do vídeo: %+v", st.Window)
		}
		col, row, cols, rows := st.Window.Hole[0], st.Window.Hole[1], st.Window.Hole[2], st.Window.Hole[3]
		x := (col-1+cols/2)*st.Window.CellW + st.Window.CellW/2
		y := (row-1+rows/2)*st.Window.CellH + st.Window.CellH/2
		r, g, b, err := w.fbpixel(x, y)
		if err != nil {
			return err
		}
		if !fixture.ClipLooksRight(r, g, b) {
			return fmt.Errorf("pixel (%d,%d) do meio do vídeo é %d,%d,%d, não o vermelho do clipe", x, y, r, g, b)
		}
		return nil
	})

	expect(t, func() error {
		if row := rowWith(w.screen(), fixture.MessageText(5)); row < 0 {
			return fmt.Errorf("a quinta mensagem de fixture não está na tela")
		}
		return nil
	})

	// O vídeo segue chegando: stats reset zera a contagem e ela volta a subir.
	w.must("stats", "reset")
	expect(t, func() error {
		var stats struct{ VideoFrames int }
		if err := json.Unmarshal([]byte(w.must("stats")), &stats); err != nil {
			return err
		}
		if stats.VideoFrames < 5 {
			return fmt.Errorf("%d quadros de vídeo desde o reset", stats.VideoFrames)
		}
		return nil
	})

	w.press("ctrl+o")
	w.waitState("o chat sobre o vídeo", 5*time.Second, func(s ui.StateReport) bool { return s.Config.ChatOverlay })
	w.press("alt+Up")
	w.waitState("a opacidade em 75%", 5*time.Second, func(s ui.StateReport) bool { return s.Config.ChatOverlayOpacity == 75 })

	expect(t, func() error {
		st, err := w.state()
		if err != nil {
			return err
		}
		if st.Window.Overlay == 0 || st.Window.OverlayOpacity != 75 {
			return fmt.Errorf("a janela desenha o chat com overlay=%d e opacidade %d", st.Window.Overlay, st.Window.OverlayOpacity)
		}
		cfg := w.savedConfig()
		for _, want := range []string{"chat_overlay = true", "chat_overlay_opacity = 75"} {
			if !strings.Contains(cfg, want) {
				return fmt.Errorf("config salvo sem %q:\n%s", want, cfg)
			}
		}
		return nil
	})
}

// W1: com o chat sobre o vídeo e só três mensagens, elas ficam embaixo, junto da
// entrada, como num chat comum. Hoje ficam no topo, com a coluna vazia abaixo.
func TestJanelaChatSobrepostoAncoradoEmbaixo(t *testing.T) {
	t.Parallel()
	w := openWindow(t, setup{config: "chat_overlay = true\n", irc: fixture.IRCConfig{Messages: 3}})
	st := w.openFirstChannel()
	w.waitState("as três mensagens no chat", 10*time.Second, func(s ui.StateReport) bool { return s.ChatMessages >= 3 })
	w.waitScreen(fixture.MessageText(3), 5*time.Second)
	rows := st.Window.Rows

	row := -1
	expectKnownBug(t, "W1", func() (bool, error) {
		if row = rowWith(w.screen(), fixture.MessageText(3)); row < 0 {
			return false, fmt.Errorf("a terceira mensagem sumiu da tela")
		}
		// Abaixo da última mensagem cabem só a entrada e a barra de status.
		return rows-1-row > 3, nil
	}, func() string {
		return fmt.Sprintf("a última mensagem está na linha %d de %d: %d linhas vazias até o fim", row+1, rows, rows-1-row)
	})
}

// W4: na busca, Ctrl+Backspace apaga a palavra anterior. A lista e o campo
// continuam à vista: a tecla não chega à TUI como Ctrl+H.
func TestJanelaCtrlBackspaceNaBusca(t *testing.T) {
	t.Parallel()
	w := openWindow(t, setup{irc: fixture.IRCConfig{Messages: 1}})
	w.openFirstChannel()
	w.press("ctrl+f", "f", "i", "x", "Space", "a", "b")
	w.waitState("o campo de busca com fix ab", 5*time.Second, func(s ui.StateReport) bool {
		return s.Search && s.SearchInput == "fix ab" && s.Layout == string(ui.LayoutSplit)
	})
	w.press("ctrl+Backspace")

	expect(t, func() error {
		st, err := w.state()
		if err != nil {
			return err
		}
		after := st.TUI
		if after.Layout != string(ui.LayoutSplit) || !after.Search || after.SearchInput != "fix " {
			return fmt.Errorf("depois do Ctrl+Backspace: layout %q, busca aberta %v, campo %q; quer split, aberta e \"fix \"", after.Layout, after.Search, after.SearchInput)
		}
		return nil
	})
}

// W5: PgDn na lista desce uma página. Hoje o destaque fica parado.
func TestJanelaPgDnNaLista(t *testing.T) {
	t.Parallel()
	w := openWindow(t, setup{})
	w.press("PageDown")

	var after ui.StateReport
	expectKnownBug(t, "W5", func() (bool, error) {
		st, err := w.state()
		if err != nil {
			return false, err
		}
		after = *st.TUI
		return after.HighlightIndex < 5, nil
	}, func() string {
		return fmt.Sprintf("depois do PgDn o destaque está no item %d (%s)", after.HighlightIndex, after.Highlighted)
	})
}

// W16: numa janela de 79 colunas, o chat sobre o vídeo continua lá, porque não
// ocupa colunas. Hoje ele some, como o chat lateral que não cabe.
func TestJanelaEstreitaMantemChatSobreposto(t *testing.T) {
	t.Parallel()
	w := openWindow(t, setup{config: "chat_overlay = true\n", irc: fixture.IRCConfig{Messages: 3}})
	w.resizeCols(79)
	w.openFirstChannel()
	w.waitState("as três mensagens no chat", 10*time.Second, func(s ui.StateReport) bool { return s.ChatMessages >= 3 })

	var after ui.StateReport
	expectKnownBug(t, "W16", func() (bool, error) {
		st, err := w.state()
		if err != nil {
			return false, err
		}
		after = *st.TUI
		if !after.ChatVisible {
			return true, nil
		}
		// A TUI diz que mostra o chat: a mensagem tem de chegar à grade.
		return false, eventually(2*time.Second, func() error {
			if rowWith(w.screen(), fixture.MessageText(3)) < 0 {
				return fmt.Errorf("o estado diz chat visível, e a terceira mensagem não está na tela")
			}
			return nil
		})
	}, func() string {
		return fmt.Sprintf("com 79 colunas o chat sobreposto não aparece (visível=%v, aviso %q)", after.ChatVisible, after.Notice)
	})
}

// rowWith devolve a linha da tela (base 0) que contém text, ou -1.
func rowWith(screen []string, text string) int {
	return slices.IndexFunc(screen, func(l string) bool { return strings.Contains(l, text) })
}
