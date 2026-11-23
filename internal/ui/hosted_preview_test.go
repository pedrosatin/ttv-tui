package ui

import "testing"

func TestPreviewOverlayRectOppositeToOverlayChat(t *testing.T) {
	base := HostPanel{Row: 1, Col: 44, Rows: 40, Cols: 100, Visible: true}

	right := base
	right.OverlayCols = 30
	right.OverlayLeft = false
	r, c, rows, cols, ok := previewOverlayRect(right)
	if !ok {
		t.Fatal("overlay à direita: retângulo da prévia rejeitado")
	}
	if c != right.Col+hostedPreviewPad {
		t.Fatalf("overlay à direita: col=%d, quero a esquerda do vídeo (%d)", c, right.Col+hostedPreviewPad)
	}
	if r != right.Row+hostedPreviewPad {
		t.Fatalf("overlay à direita: row=%d, quero o topo", r)
	}
	if cols < hostedPreviewMinCols || rows < hostedPreviewMinRows {
		t.Fatalf("overlay à direita: tamanho %dx%d abaixo do mínimo", cols, rows)
	}
	if c+cols > right.Col+right.Cols {
		t.Fatalf("overlay à direita: quadro sai do painel: col=%d cols=%d", c, cols)
	}

	left := base
	left.OverlayCols = 30
	left.OverlayLeft = true
	rowL, colL, rowsL, colsL, okL := previewOverlayRect(left)
	if !okL {
		t.Fatal("overlay à esquerda: retângulo da prévia rejeitado")
	}
	wantCol := left.Col + left.Cols - hostedPreviewPad - colsL
	if colL != wantCol {
		t.Fatalf("overlay à esquerda: col=%d, quero a direita do vídeo (%d)", colL, wantCol)
	}
	if rowL != left.Row+hostedPreviewPad {
		t.Fatalf("overlay à esquerda: row=%d, quero o topo", rowL)
	}
	if colsL < hostedPreviewMinCols || rowsL < hostedPreviewMinRows {
		t.Fatalf("overlay à esquerda: tamanho %dx%d abaixo do mínimo", colsL, rowsL)
	}
}

func TestPreviewOverlayRectWithoutOverlayGoesTopRight(t *testing.T) {
	panel := HostPanel{Row: 1, Col: 44, Rows: 40, Cols: 100, Visible: true}
	_, c, rows, cols, ok := previewOverlayRect(panel)
	if !ok {
		t.Fatal("sem overlay: retângulo rejeitado")
	}
	wantCol := panel.Col + panel.Cols - hostedPreviewPad - cols
	if c != wantCol {
		t.Fatalf("sem overlay: col=%d, quero canto superior direito (%d)", c, wantCol)
	}
	if rows < hostedPreviewMinRows || cols < hostedPreviewMinCols {
		t.Fatalf("sem overlay: tamanho %dx%d", cols, rows)
	}
}

func TestPreviewOverlayRectRejectsNarrowPanel(t *testing.T) {
	for _, panel := range []HostPanel{
		{Row: 1, Col: 1, Rows: 20, Cols: 12, Visible: true}, // cols curtas
		{Row: 1, Col: 1, Rows: 5, Cols: 80, Visible: true},  // rows curtas
		// Chat sobreposto come a área útil: freeCols = 40-24 = 16, pad deixa 14.
		{Row: 1, Col: 1, Rows: 20, Cols: 40, Visible: true, OverlayCols: 24},
	} {
		if _, _, _, _, ok := previewOverlayRect(panel); ok {
			t.Fatalf("painel estreito aceitou o quadro da prévia: %+v", panel)
		}
	}
}

// Terminal estreito (34–41) sem lista e com chat sobreposto: o espaço livre
// horizontal fica abaixo do mínimo do PiP + padding.
func TestHostedPanelPreviewRectRejectedOnNarrowOverlayWithoutList(t *testing.T) {
	for width := 34; width <= 41; width++ {
		m := hostedModel(t, width, 30, true)
		m.layout.showList = false
		m.hosted.chatOverlay = true
		m.player.state = PlayerStatePlaying
		m.preview.state = PreviewStatePlaying
		panel := m.hostedPanel()
		if panel.PreviewCols != 0 || panel.PreviewRows != 0 {
			t.Fatalf("largura %d sem lista: Preview* deveria ser rejeitado: %+v", width, panel)
		}
		if _, _, _, _, ok := previewOverlayRect(panel); ok {
			t.Fatalf("largura %d: previewOverlayRect aceitou com overlay=%d cols=%d", width, panel.OverlayCols, panel.Cols)
		}
	}
}

func TestHostedPanelPublishesPreviewRectWithMainAndHelpKeepsIt(t *testing.T) {
	m := hostedModel(t, 212, 49, true)
	m.hosted.chatOverlay = true
	m.player.state = PlayerStatePlaying
	m.player.login = "gaules"
	m.preview.state = PreviewStatePlaying
	m.preview.login = "alanzoka"

	panel := m.hostedPanel()
	if panel.PreviewCols == 0 || panel.PreviewRows == 0 {
		t.Fatalf("com principal e prévia: HostPanel sem Preview*: %+v", panel)
	}
	if panel.PreviewCol < panel.Col || panel.PreviewCol+panel.PreviewCols > panel.Col+panel.Cols {
		t.Fatalf("Preview* fora do painel: %+v", panel)
	}
	// Chat sobreposto à direita → prévia à esquerda.
	if panel.PreviewCol != panel.Col+hostedPreviewPad {
		t.Fatalf("prévia deveria ficar à esquerda com overlay à direita: %+v", panel)
	}

	m.layout.helpVisible = true
	withHelp := m.hostedPanel()
	if withHelp.PreviewCols == 0 || withHelp.OverlayCols != withHelp.Cols {
		t.Fatalf("ajuda aberta deveria cobrir o vídeo e manter o quadro da prévia: %+v", withHelp)
	}
	if withHelp.PreviewRow != panel.PreviewRow || withHelp.PreviewCol != panel.PreviewCol {
		t.Fatalf("ajuda mudou a posição do quadro: antes %+v depois %+v", panel, withHelp)
	}

	m.layout.helpVisible = false
	m.preview.state = PreviewStateClosed
	without := m.hostedPanel()
	if without.PreviewCols != 0 || without.PreviewRows != 0 {
		t.Fatalf("sem prévia ativa ainda publicou Preview*: %+v", without)
	}
}

func TestHostedPanelPreviewRectWithSwappedOverlay(t *testing.T) {
	m := hostedModel(t, 212, 49, true)
	m.hosted.chatOverlay = true
	m.hosted.panelsSwapped = true
	m.player.state = PlayerStatePlaying
	m.preview.state = PreviewStatePlaying
	panel := m.hostedPanel()
	if !panel.OverlayLeft {
		t.Fatalf("lados trocados: OverlayLeft=%v", panel.OverlayLeft)
	}
	wantCol := panel.Col + panel.Cols - hostedPreviewPad - panel.PreviewCols
	if panel.PreviewCol != wantCol {
		t.Fatalf("prévia deveria ficar à direita com overlay à esquerda: %+v", panel)
	}

	m.layout.helpVisible = true
	withHelp := m.hostedPanel()
	if withHelp.PreviewCol != panel.PreviewCol || withHelp.PreviewRow != panel.PreviewRow {
		t.Fatalf("ajuda com lados trocados moveu o PiP: antes %+v depois %+v", panel, withHelp)
	}
}

func TestHostedPanelPreviewRectWithoutChatOverlayKeepsCornerUnderHelp(t *testing.T) {
	m := hostedModel(t, 212, 49, true)
	m.hosted.chatOverlay = false
	m.player.state = PlayerStatePlaying
	m.preview.state = PreviewStatePlaying
	panel := m.hostedPanel()
	if panel.PreviewCols == 0 {
		t.Fatalf("sem overlay de chat: HostPanel sem Preview*: %+v", panel)
	}
	wantCol := panel.Col + panel.Cols - hostedPreviewPad - panel.PreviewCols
	if panel.PreviewCol != wantCol {
		t.Fatalf("sem overlay: prévia deveria ficar no canto superior direito: %+v", panel)
	}

	m.layout.helpVisible = true
	withHelp := m.hostedPanel()
	if withHelp.PreviewCol != panel.PreviewCol || withHelp.PreviewRow != panel.PreviewRow {
		t.Fatalf("ajuda sem chat overlay moveu o PiP: antes %+v depois %+v", panel, withHelp)
	}
	if withHelp.OverlayCols != withHelp.Cols {
		t.Fatalf("ajuda deveria cobrir o vídeo: %+v", withHelp)
	}
}
