//go:build cenarios && linux

package windowscenarios

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/fixture"
	"github.com/pedrosatin/ttv-tui/internal/ui"
)

// Sem vídeo principal, Espaço abre a prévia ocupando o painel de vídeo inteiro
// mesmo com TTV_WINDOW_MUTE desligado; Esc fecha e devolve o texto de espera da
// coluna; Enter com a prévia aberta fecha a prévia e abre a principal na coluna.
func TestJanelaPreviaSemPrincipal(t *testing.T) {
	t.Parallel()
	w := openWindow(t, setup{
		irc: fixture.IRCConfig{Messages: 2},
		env: []string{"TTV_WINDOW_MUTE=0"},
	})
	w.press("Return")
	w.waitState("os canais da Categoria 01", 10*time.Second, func(s ui.StateReport) bool {
		return s.Mode == string(ui.ModeChannels) && s.Highlighted == "FixtureUm"
	})

	// 1. Espaço abre a prévia ocupando o painel de vídeo inteiro.
	w.press("Space")
	w.waitState("a prévia tocando FixtureUm", 15*time.Second, func(s ui.StateReport) bool {
		return s.Preview == string(ui.PreviewStatePlaying) && s.Player == string(ui.PlayerStateNone)
	})
	st := w.waitFirstFrame()
	w.waitVideoFrames(3)

	var cx, cy int
	expect(t, func() error {
		cur, err := w.state()
		if err != nil {
			return err
		}
		if !cur.Window.HoleOn || len(cur.Window.Hole) != 4 {
			return fmt.Errorf("janela sem buraco da prévia: %+v", cur.Window)
		}
		col, row, cols, rows := cur.Window.Hole[0], cur.Window.Hole[1], cur.Window.Hole[2], cur.Window.Hole[3]
		if col+cols-1 != cur.Window.Cols {
			return fmt.Errorf("prévia sem principal não usou o painel inteiro: col=%d cols=%d de %d", col, cols, cur.Window.Cols)
		}
		cx = (col-1+cols/2)*cur.Window.CellW + cur.Window.CellW/2
		cy = (row-1+rows/2)*cur.Window.CellH + cur.Window.CellH/2
		r, g, b, err := w.fbpixel(cx, cy)
		if err != nil {
			return err
		}
		if !fixture.ClipLooksRight(r, g, b) {
			return fmt.Errorf("pixel (%d,%d) da prévia é %d,%d,%d, não o vermelho do clipe", cx, cy, r, g, b)
		}
		var stats struct {
			Active, MainActive, PreviewActive bool
			LastRole                          string
		}
		if err := json.Unmarshal([]byte(w.must("stats")), &stats); err != nil {
			return err
		}
		if !stats.Active || stats.MainActive || !stats.PreviewActive || stats.LastRole != "preview" {
			return fmt.Errorf("stats inesperados na prévia: %+v", stats)
		}
		return nil
	})
	_ = st

	// 2. Esc fecha a prévia, fecha o buraco e devolve o texto de espera da coluna.
	w.press("Escape")
	w.waitState("a prévia fechada", 10*time.Second, func(s ui.StateReport) bool {
		return s.Preview == string(ui.PreviewStateClosed)
	})
	expect(t, func() error {
		cur, err := w.state()
		if err != nil {
			return err
		}
		if cur.Window.HoleOn {
			return fmt.Errorf("buraco continuou aberto depois de fechar a prévia")
		}
		r, g, b, err := w.fbpixel(cx, cy)
		if err != nil {
			return err
		}
		if fixture.ClipLooksRight(r, g, b) {
			return fmt.Errorf("quadro da prévia ficou preso no pixel (%d,%d): %d,%d,%d", cx, cy, r, g, b)
		}
		var stats struct {
			Active, MainActive, PreviewActive bool
		}
		if err := json.Unmarshal([]byte(w.must("stats")), &stats); err != nil {
			return err
		}
		if stats.Active || stats.MainActive || stats.PreviewActive {
			return fmt.Errorf("sessão ainda ativa após fechar a prévia: %+v", stats)
		}
		return nil
	})
	w.waitScreen("Enter assiste a FixtureUm", 5*time.Second)

	// 3. Reabre a prévia e aperta Enter: fecha a prévia e abre a principal na coluna.
	w.press("Space")
	w.waitState("a prévia reaberta", 15*time.Second, func(s ui.StateReport) bool {
		return s.Preview == string(ui.PreviewStatePlaying)
	})
	w.waitFirstFrame()
	w.waitVideoFrames(3)

	w.press("Return")
	w.waitState("a prévia fechada e a principal tocando", 15*time.Second, func(s ui.StateReport) bool {
		return s.Preview == string(ui.PreviewStateClosed) &&
			s.Player == string(ui.PlayerStatePlaying) &&
			s.ChatChannel == "fixture_um"
	})
	w.waitFirstFrame()
	w.waitVideoFrames(3)

	expect(t, func() error {
		cur, err := w.state()
		if err != nil {
			return err
		}
		if !cur.Window.HoleOn || len(cur.Window.Hole) != 4 {
			return fmt.Errorf("principal não abriu o buraco: %+v", cur.Window)
		}
		col, row, cols, rows := cur.Window.Hole[0], cur.Window.Hole[1], cur.Window.Hole[2], cur.Window.Hole[3]
		x := (col-1+cols/2)*cur.Window.CellW + cur.Window.CellW/2
		y := (row-1+rows/2)*cur.Window.CellH + cur.Window.CellH/2
		r, g, b, err := w.fbpixel(x, y)
		if err != nil {
			return err
		}
		if !fixture.ClipLooksRight(r, g, b) {
			return fmt.Errorf("pixel (%d,%d) da principal é %d,%d,%d, não o vermelho do clipe", x, y, r, g, b)
		}
		if rowWith(w.screen(), fixture.MessageText(2)) < 0 {
			return fmt.Errorf("chat não apareceu depois de abrir a principal")
		}
		var stats struct {
			Active, MainActive, PreviewActive bool
			LastRole                          string
		}
		if err := json.Unmarshal([]byte(w.must("stats")), &stats); err != nil {
			return err
		}
		if !stats.Active || !stats.MainActive || stats.PreviewActive || stats.LastRole != "main" {
			return fmt.Errorf("stats inesperados na principal: %+v", stats)
		}
		return nil
	})
}
