package main

import (
	"slices"
	"testing"
)

func TestSessionOptionsPreviewMutesWithoutEnv(t *testing.T) {
	hasMute := func(opts [][2]string) bool {
		return slices.Contains(opts, [2]string{"mute", "yes"})
	}
	if !hasMute(sessionOptions(rolePreview, "/tmp/preview/i", "")) {
		t.Fatal("prévia sem TTV_WINDOW_MUTE não veio muda")
	}
	if !hasMute(sessionOptions(rolePreview, "/tmp/preview/i", "0")) {
		t.Fatal("prévia com TTV_WINDOW_MUTE=0 não veio muda")
	}
	if hasMute(sessionOptions(roleMain, "/tmp/main/i", "")) {
		t.Fatal("principal sem TTV_WINDOW_MUTE veio muda")
	}
	if hasMute(sessionOptions(roleMain, "/tmp/main/i", "0")) {
		t.Fatal("principal com TTV_WINDOW_MUTE=0 veio muda")
	}
	if !hasMute(sessionOptions(roleMain, "/tmp/main/i", "1")) {
		t.Fatal("principal com TTV_WINDOW_MUTE=1 não veio muda")
	}
}

func TestActiveVideoMainPrecedenceAndEndSession(t *testing.T) {
	a := &app{haveHost: true, host: hostPanel{Visible: true, Col: 1, Row: 1, Cols: 10, Rows: 5}}
	prev := &session{role: rolePreview, stopping: make(chan struct{})}
	main := &session{role: roleMain, stopping: make(chan struct{})}
	stale := &session{role: rolePreview, stopping: make(chan struct{})}

	a.preview = prev
	if a.activeVideo() != prev {
		t.Fatal("sem principal, activeVideo não devolveu a prévia")
	}

	a.sess = main
	if a.activeVideo() != main {
		t.Fatal("com as duas sessões, activeVideo não priorizou a principal")
	}

	// Encerrar sessão antiga ou de outro slot não derruba a sessão viva.
	a.endSession(stale)
	if a.sess != main || a.preview != prev {
		t.Fatal("endSession obsoleto fechou sessão ativa")
	}

	a.endSession(prev)
	if a.preview != nil {
		t.Fatal("endSession(prev) não limpou a prévia")
	}
	if a.sess != main {
		t.Fatal("endSession(prev) fechou a sessão principal")
	}

	a.preview = &session{role: rolePreview, stopping: make(chan struct{})}
	a.endSession(main)
	if a.sess != nil {
		t.Fatal("endSession(main) não limpou a principal")
	}
	if a.activeVideo() != a.preview {
		t.Fatal("após encerrar principal, activeVideo não voltou à prévia")
	}

	a.stopSession()
	if a.sess != nil || a.preview != nil || a.activeVideo() != nil {
		t.Fatal("stopSession não encerrou as duas sessões")
	}
}

func TestRefreshHoleMainOverPreviewAndMargins(t *testing.T) {
	var gotMain, gotPrev [][2]string
	a := &app{
		W: 1000, H: 720,
		fs:       &fontSet{cellW: 10, cellH: 20},
		haveHost: true,
		host:     hostPanel{Visible: true, Col: 11, Row: 3, Cols: 70, Rows: 30},
	}
	prev := &session{
		role:        rolePreview,
		stopping:    make(chan struct{}),
		setProperty: func(k, v string) { gotPrev = append(gotPrev, [2]string{k, v}) },
	}
	prev.gotFirst.Store(true)
	a.preview = prev
	a.refreshHole()
	if !a.holeOn {
		t.Fatal("prévia com primeiro quadro não abriu o buraco do vídeo")
	}

	// Ao iniciar a principal com a prévia ainda viva, o buraco fecha até o
	// primeiro quadro da principal, sem deixar o quadro da prévia exposto.
	main := &session{
		role:        roleMain,
		stopping:    make(chan struct{}),
		setProperty: func(k, v string) { gotMain = append(gotMain, [2]string{k, v}) },
	}
	a.sess = main
	a.refreshHole()
	if a.holeOn {
		t.Fatal("principal sem primeiro quadro manteve o buraco da prévia aberto")
	}

	a.applyMargins()
	if len(gotMain) == 0 || len(gotPrev) == 0 {
		t.Fatalf("applyMargins não aplicou margens às sessões ativas: main=%d prev=%d", len(gotMain), len(gotPrev))
	}

	a.endSession(main)
	if !a.holeOn {
		t.Fatal("prévia com primeiro quadro não reabriu o buraco após sair da principal")
	}
	a.endSession(prev)
	if a.holeOn {
		t.Fatal("fechar a prévia manteve o buraco do vídeo aberto")
	}
}
