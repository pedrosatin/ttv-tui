//go:build cenarios && linux

package windowscenarios

import (
	"encoding/json"
	"errors"
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
	var sessionsWithPreview int64
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
		// Principal fora do PiP e fora do chat sobreposto: tem de ser o clipe,
		// não o fundo #1c1c1e que o scissor do mpv deixava ao apagar o FBO 0.
		mx, my, err := mainPixelOutsidePreview(cur, previewHole)
		if err != nil {
			return err
		}
		mr, mg, mb, err := w.fbpixel(mx, my)
		if err != nil {
			return err
		}
		if !fixture.ClipLooksRight(mr, mg, mb) {
			return fmt.Errorf("pixel (%d,%d) da principal com prévia aberta é %d,%d,%d (fundo ou apagado)", mx, my, mr, mg, mb)
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
		sessionsWithPreview = stats.Sessions
		return nil
	})

	// Ajuda (F1) cobre o quadro da prévia como cobre o vídeo (overlay na
	// coluna inteira; o PiP não some nem muda de canto).
	w.press("F1")
	w.waitState("ajuda aberta sobre a prévia", 5*time.Second, func(s ui.StateReport) bool {
		return s.Help
	})
	w.waitScreen("===", 5*time.Second)
	expect(t, func() error {
		cur, err := w.state()
		if err != nil {
			return err
		}
		if len(cur.Window.PreviewHole) != 4 || cur.Window.PreviewHole[2] == 0 {
			return fmt.Errorf("ajuda removeu o quadro da prévia: %+v", cur.Window)
		}
		if !sameInts(cur.Window.PreviewHole, previewHole) {
			return fmt.Errorf("ajuda moveu o quadro da prévia: antes %v depois %v", previewHole, cur.Window.PreviewHole)
		}
		if cur.Window.Overlay != cur.Window.Hole[2] {
			return fmt.Errorf("ajuda não cobre o vídeo/prévia: overlay=%d hole=%v", cur.Window.Overlay, cur.Window.Hole)
		}
		// Procura, dentro do retângulo do PiP, uma célula em branco no fundo da
		// ajuda (antes do fim do texto da linha): ali o PiP não pode aparecer.
		if rowWith(w.screen(), "===") < 0 {
			return errors.New("o título da ajuda sumiu da grade")
		}
		pCol, pRow, pCols, pRows := previewHole[0], previewHole[1], previewHole[2], previewHole[3]
		cx, cy, ok := helpBackdropCellInPreview(w.screen(), cur.Window.Hole[0], pCol, pRow, pCols, pRows)
		if !ok {
			return fmt.Errorf("nenhuma célula do fundo da ajuda cai no quadro da prévia: preview=%v", previewHole)
		}
		x := cx*cur.Window.CellW + cur.Window.CellW/2
		y := cy*cur.Window.CellH + cur.Window.CellH/2
		r, g, b, err := w.fbpixel(x, y)
		if err != nil {
			return err
		}
		if r > 40 || g > 40 || b > 40 {
			return fmt.Errorf("a prévia atravessa a ajuda: pixel (%d,%d) é %d,%d,%d", x, y, r, g, b)
		}
		return nil
	})
	w.press("F1")
	w.waitState("ajuda fechada, prévia ainda aberta", 5*time.Second, func(s ui.StateReport) bool {
		return !s.Help && s.Preview == string(ui.PreviewStatePlaying)
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
			Sessions                  int64
			MainActive, PreviewActive bool
		}
		if err := json.Unmarshal([]byte(w.must("stats")), &stats); err != nil {
			return err
		}
		if !stats.MainActive || stats.PreviewActive {
			return fmt.Errorf("após fechar a prévia: %+v", stats)
		}
		if stats.Sessions != sessionsWithPreview {
			return fmt.Errorf("fechar a prévia reiniciou a principal: sessions %d→%d", sessionsWithPreview, stats.Sessions)
		}
		return nil
	})
}

// mainPixelOutsidePreview escolhe um pixel do buraco da principal fora do
// quadro da prévia e fora da faixa do chat sobreposto.
func mainPixelOutsidePreview(cur windowState, previewHole []int) (x, y int, err error) {
	hole := cur.Window.Hole
	if len(hole) != 4 || len(previewHole) != 4 {
		return 0, 0, fmt.Errorf("buracos incompletos: hole=%v preview=%v", hole, previewHole)
	}
	cw, ch := cur.Window.CellW, cur.Window.CellH
	pCol, pRow, pCols, pRows := previewHole[0], previewHole[1], previewHole[2], previewHole[3]
	overlay := cur.Window.Overlay
	// Chat sobreposto à direita (padrão do cenário): amostra à esquerda do PiP
	// ou abaixo dele, ainda dentro do vídeo livre.
	freeRight := hole[0] + hole[2]
	if overlay > 0 {
		freeRight = hole[0] + hole[2] - overlay
	}
	// Ponto abaixo do PiP, centrado na faixa livre horizontal.
	cellCol := hole[0] + (freeRight-hole[0])/2
	cellRow := pRow + pRows + 1
	if cellRow >= hole[1]+hole[3] {
		// PiP encostado embaixo: amostra à direita do PiP, ainda na faixa livre.
		cellRow = pRow + pRows/2
		cellCol = pCol + pCols + 1
		if cellCol >= freeRight {
			cellCol = pCol - 2
		}
	}
	if cellCol < hole[0] || cellCol >= freeRight || cellRow < hole[1] || cellRow >= hole[1]+hole[3] {
		return 0, 0, fmt.Errorf("sem célula livre fora do PiP: hole=%v preview=%v overlay=%d tentativa=%d,%d", hole, previewHole, overlay, cellCol, cellRow)
	}
	if cellCol >= pCol && cellCol < pCol+pCols && cellRow >= pRow && cellRow < pRow+pRows {
		return 0, 0, fmt.Errorf("célula de amostra caiu dentro do PiP: %d,%d preview=%v", cellCol, cellRow, previewHole)
	}
	x = (cellCol-1)*cw + cw/2
	y = (cellRow-1)*ch + ch/2
	return x, y, nil
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
