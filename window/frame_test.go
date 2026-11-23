package main

import "testing"

func TestFrameDrawnOnlyWhenActuallyDrawn(t *testing.T) {
	if frameDrawn(false, true, false) {
		t.Fatal("sem vídeo não conta quadro")
	}
	if frameDrawn(true, false, false) {
		t.Fatal("aviso sem desenho de painel inteiro nem PiP não conta")
	}
	if !frameDrawn(true, true, false) {
		t.Fatal("painel inteiro com aviso tem de contar")
	}
	if !frameDrawn(true, false, true) {
		t.Fatal("PiP composto tem de contar")
	}
	if !frameDrawn(false, false, true) {
		t.Fatal("PiP composto conta mesmo sem sessão no painel inteiro")
	}
}

func TestClearPreviewDirtyPreviewAloneAndMainWithoutPIP(t *testing.T) {
	// Prévia sozinha: limpa sempre (desenha o painel inteiro).
	alone := &app{preview: &session{role: rolePreview}, previewDirty: true}
	if !alone.clearPreviewDirtyAfterPresent() {
		t.Fatal("prévia sozinha não limpou previewDirty")
	}

	// Principal + prévia, ainda no host de antes da abertura: espera o PiP.
	waiting := &app{
		sess:             &session{role: roleMain},
		preview:          &session{role: rolePreview},
		hostSeq:          3,
		previewAtHostSeq: 3,
		host:             hostPanel{Visible: true, PreviewCols: 0},
		previewDirty:     true,
	}
	if waiting.clearPreviewDirtyAfterPresent() {
		t.Fatal("antes do HostPanel com PiP limpou o aviso (perderia o 1º quadro)")
	}

	// Host republicou depois da abertura sem PiP: não prende o flag.
	stale := &app{
		sess:             &session{role: roleMain},
		preview:          &session{role: rolePreview},
		hostSeq:          5,
		previewAtHostSeq: 3,
		host:             hostPanel{Visible: true, PreviewCols: 0},
		previewDirty:     true,
	}
	if !stale.clearPreviewDirtyAfterPresent() {
		t.Fatal("com principal e sem PiP após republicar o host, previewDirty ficou preso")
	}

	// PiP ativo: limpa.
	pip := &app{
		sess:    &session{role: roleMain},
		preview: &session{role: rolePreview},
		host:    hostPanel{PreviewCols: 20, PreviewRows: 8},
	}
	if !pip.clearPreviewDirtyAfterPresent() {
		t.Fatal("com PiP ativo não limpou previewDirty")
	}

	// Sem prévia: limpa.
	none := &app{sess: &session{role: roleMain}}
	if !none.clearPreviewDirtyAfterPresent() {
		t.Fatal("sem prévia não limpou previewDirty")
	}
}

func TestClearPreviewDirtyDoesNotCountFramesWithoutNewNotice(t *testing.T) {
	// Contrato do contador: prévia sozinha sem aviso novo (fullFrame=false,
	// previewRendered=false) não incrementa; com principal e sem PiP o aviso
	// da prévia sozinho também não conta (só contaria se previewRendered).
	if frameDrawn(true, false, false) {
		t.Fatal("prévia sozinha sem aviso novo contou quadro")
	}
	// Simula present(false, true) com principal ativa e sem PiP: v=main,
	// fullFrame=mainVideo=false, previewRendered=false.
	if frameDrawn(true, false, false) {
		t.Fatal("com principal e sem PiP a prévia contou quadro sem compor")
	}
}
