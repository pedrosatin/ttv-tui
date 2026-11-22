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

// Com a principal tocando, Espaço abre a prévia num quadro menor sobre o
// vídeo; a principal continua ativa (sem reiniciar o buraco) e Esc fecha só a
// prévia. Redimensionar a janela reposiciona o quadro.
func TestJanelaPreviaSobrePrincipal(t *testing.T) {
	t.Parallel()
	w := openWindow(t, setup{
		config: "chat_overlay = true\n",
		irc:    fixture.IRCConfig{Messages: 2},
		env:    []string{"TTV_WINDOW_MUTE=0"},
	})
	w.openFirstChannel()
	w.waitVideoFrames(3)

	var sessionsBefore int64
	expect(t, func() error {
		var stats struct {
			Sessions                          int64
			Active, MainActive, PreviewActive bool
		}
		if err := json.Unmarshal([]byte(w.must("stats")), &stats); err != nil {
			return err
		}
		if !stats.MainActive || stats.PreviewActive {
			return fmt.Errorf("antes da prévia: %+v", stats)
		}
		sessionsBefore = stats.Sessions
		return nil
	})

	// Destaque em outro canal e abre a prévia sobre a principal.
	w.press("j")
	w.waitState("destaque em FixtureDois", 5*time.Second, func(s ui.StateReport) bool {
		return s.Highlighted == "FixtureDois"
	})
	w.press("Space")
	w.waitState("prévia tocando com principal ativa", 15*time.Second, func(s ui.StateReport) bool {
		return s.Preview == string(ui.PreviewStatePlaying) && s.Player == string(ui.PlayerStatePlaying)
	})
	w.waitVideoFrames(3)

	var previewHole []int
	expect(t, func() error {
		cur, err := w.state()
		if err != nil {
			return err
		}
		if !cur.Window.HoleOn {
			return fmt.Errorf("buraco da principal fechou ao abrir a prévia: %+v", cur.Window)
		}
		if len(cur.Window.PreviewHole) != 4 || cur.Window.PreviewHole[2] == 0 || cur.Window.PreviewHole[3] == 0 {
			return fmt.Errorf("janela sem quadro da prévia: %+v", cur.Window)
		}
		previewHole = append([]int(nil), cur.Window.PreviewHole...)
		col, row, cols, rows := previewHole[0], previewHole[1], previewHole[2], previewHole[3]
		// Quadro dentro do painel de vídeo e menor que ele.
		hole := cur.Window.Hole
		if col < hole[0] || row < hole[1] || col+cols > hole[0]+hole[2] || row+rows > hole[1]+hole[3] {
			return fmt.Errorf("quadro da prévia fora do painel: preview=%v hole=%v", previewHole, hole)
		}
		if cols >= hole[2] && rows >= hole[3] {
			return fmt.Errorf("quadro da prévia não é menor que o painel: preview=%v hole=%v", previewHole, hole)
		}
		x := (col-1+cols/2)*cur.Window.CellW + cur.Window.CellW/2
		y := (row-1+rows/2)*cur.Window.CellH + cur.Window.CellH/2
		r, g, b, err := w.fbpixel(x, y)
		if err != nil {
			return err
		}
		if !fixture.ClipLooksRight(r, g, b) {
			return fmt.Errorf("pixel (%d,%d) do quadro da prévia é %d,%d,%d", x, y, r, g, b)
		}
		var stats struct {
			Sessions                          int64
			Active, MainActive, PreviewActive bool
		}
		if err := json.Unmarshal([]byte(w.must("stats")), &stats); err != nil {
			return err
		}
		if !stats.Active || !stats.MainActive || !stats.PreviewActive {
			return fmt.Errorf("stats com as duas sessões: %+v", stats)
		}
		if stats.Sessions != sessionsBefore+1 {
			return fmt.Errorf("abrir a prévia reiniciou a principal: sessions %d→%d", sessionsBefore, stats.Sessions)
		}
		return nil
	})

	// Redimensionar reposiciona o quadro.
	w.resizeCols(curCols(t, w) - 10)
	w.waitVideoFrames(2)
	expect(t, func() error {
		cur, err := w.state()
		if err != nil {
			return err
		}
		if len(cur.Window.PreviewHole) != 4 || cur.Window.PreviewHole[2] == 0 {
			return fmt.Errorf("após resize sem quadro da prévia: %+v", cur.Window)
		}
		if sameInts(cur.Window.PreviewHole, previewHole) {
			return fmt.Errorf("resize não reposicionou o quadro: %v", cur.Window.PreviewHole)
		}
		return nil
	})

	w.press("Escape")
	w.waitState("prévia fechada, principal tocando", 10*time.Second, func(s ui.StateReport) bool {
		return s.Preview == string(ui.PreviewStateClosed) && s.Player == string(ui.PlayerStatePlaying)
	})
	expect(t, func() error {
		cur, err := w.state()
		if err != nil {
			return err
		}
		if !cur.Window.HoleOn {
			return fmt.Errorf("fechar a prévia apagou o buraco da principal")
		}
		if len(cur.Window.PreviewHole) == 4 && (cur.Window.PreviewHole[2] != 0 || cur.Window.PreviewHole[3] != 0) {
			return fmt.Errorf("quadro da prévia ficou publicado: %v", cur.Window.PreviewHole)
		}
		var stats struct {
			MainActive, PreviewActive bool
		}
		if err := json.Unmarshal([]byte(w.must("stats")), &stats); err != nil {
			return err
		}
		if !stats.MainActive || stats.PreviewActive {
			return fmt.Errorf("após fechar a prévia: %+v", stats)
		}
		return nil
	})
}

func curCols(t *testing.T, w *window) int {
	t.Helper()
	st, err := w.state()
	if err != nil {
		t.Fatal(err)
	}
	return st.Window.Cols
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
