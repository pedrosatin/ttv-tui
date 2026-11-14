//go:build cenarios && linux

package windowscenarios

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/fixture"
	"github.com/pedrosatin/ttv-tui/internal/ui"
)

// helpColumn é o trecho de cada linha da grade que fica na coluna do vídeo,
// onde a ajuda aparece na janela. A grade da ajuda não tem caractere largo.
func helpColumn(screen []string, col, cols int) []string {
	out := make([]string, len(screen))
	for i, row := range screen {
		r := []rune(row)
		from, to := min(col-1, len(r)), min(col-1+cols, len(r))
		out[i] = strings.TrimRight(string(r[from:to]), " ")
	}
	return out
}

// W13: com o vídeo tocando, o espectador abre a ajuda com F1 e passa pelas
// seções com →. A ajuda tem fundo opaco atrás do texto, não fala de "só TUI"
// e mostra Ctrl+O e Ctrl+P em linhas próprias na seção Janela e layout, longe
// de "Largura". Dentro da janela, essas teclas não levam a marca "só na
// janela". F1 de novo fecha a ajuda e devolve o vídeo inteiro.
func TestJanelaAjudaLegivelSobreOVideo(t *testing.T) {
	t.Parallel()
	w := openWindow(t, setup{irc: fixture.IRCConfig{Messages: 3}})
	w.openFirstChannel()
	w.press("F1")
	w.waitState("a ajuda aberta", 5*time.Second, func(s ui.StateReport) bool { return s.Help })
	w.waitScreen("===", 5*time.Second)

	expect(t, func() error {
		st, err := w.state()
		if err != nil {
			return err
		}
		if !st.Window.HoleOn || len(st.Window.Hole) != 4 || st.Window.Overlay != st.Window.Hole[2] {
			return fmt.Errorf("a ajuda não está sobre o vídeo: %+v", st.Window)
		}
		row := rowWith(w.screen(), "===")
		if row < 0 {
			return errors.New("o título da ajuda sumiu da grade")
		}
		// A primeira célula da coluna do vídeo, na linha do título, é o espaço
		// antes do texto: ali só aparece o fundo da ajuda. Com o fundo
		// translúcido, o vermelho do clipe atravessa.
		x := (st.Window.Hole[0]-1)*st.Window.CellW + st.Window.CellW/2
		y := row*st.Window.CellH + st.Window.CellH/2
		r, g, b, err := w.fbpixel(x, y)
		if err != nil {
			return err
		}
		if r > 40 || g > 40 || b > 40 {
			return fmt.Errorf("o vídeo atravessa o fundo da ajuda: pixel (%d,%d) é %d,%d,%d, quer quase preto", x, y, r, g, b)
		}
		return nil
	})

	// A ajuda rola: cada → leva à próxima seção, e as sete passam pela tela.
	st, err := w.state()
	if err != nil {
		t.Fatal(err)
	}
	col, cols := st.Window.Hole[0], st.Window.Hole[2]
	var pages [][]string
	for range 7 {
		pages = append(pages, helpColumn(w.screen(), col, cols))
		w.press("Right")
	}

	var problems []string
	sawWindowKeys := false
	for _, page := range pages {
		header := -1
		for i, line := range page {
			text := strings.TrimSpace(line)
			switch {
			case strings.Contains(text, "── Janela e layout ──"):
				header = i
			case strings.Contains(text, "TUI"):
				problems = append(problems, fmt.Sprintf("a ajuda fala da TUI na janela: %q", text))
			case strings.Contains(text, "Largura"):
				problems = append(problems, fmt.Sprintf("linha de largura na ajuda: %q", text))
			case strings.Contains(strings.ToLower(text), "só na janela"):
				problems = append(problems, fmt.Sprintf("a marca de só na janela aparece dentro dela: %q", text))
			}
			for _, key := range []string{"Ctrl+O", "Ctrl+P"} {
				if !strings.Contains(text, key) {
					continue
				}
				if !strings.HasPrefix(text, key+" ") || header < 0 || header > i {
					problems = append(problems, fmt.Sprintf("%s fora de uma linha própria na seção Janela e layout: %q", key, text))
				} else {
					sawWindowKeys = true
				}
			}
		}
	}
	if !sawWindowKeys {
		problems = append(problems, "nenhuma página mostrou Ctrl+O e Ctrl+P na seção Janela e layout")
	}
	if len(problems) > 0 {
		var dump strings.Builder
		for i, page := range pages {
			fmt.Fprintf(&dump, "--- página %d\n%s\n", i+1, strings.Join(page, "\n"))
		}
		t.Fatalf("%s\n%s", strings.Join(problems, "\n"), dump.String())
	}

	w.press("F1")
	w.waitState("a ajuda fechada", 5*time.Second, func(s ui.StateReport) bool { return !s.Help })
	expect(t, func() error {
		st, err := w.state()
		if err != nil {
			return err
		}
		if !st.Window.HoleOn || st.Window.Overlay != 0 {
			return fmt.Errorf("o vídeo não voltou inteiro ao fechar a ajuda: %+v", st.Window)
		}
		if rowWith(w.screen(), "===") >= 0 {
			return errors.New("a ajuda ficou na grade depois do F1")
		}
		return nil
	})
}
