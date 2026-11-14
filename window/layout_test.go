package main

import (
	"reflect"
	"testing"
)

func TestApplyMarginsQueuesLayout(t *testing.T) {
	a := &app{
		W: 1000, H: 720,
		fs:   &fontSet{cellW: 10, cellH: 20},
		host: hostPanel{Visible: true, Col: 11, Row: 3, Cols: 70, Rows: 30},
	}
	var got [][2]string
	a.sess = &session{setProperty: func(k, v string) { got = append(got, [2]string{k, v}) }}
	check := func(want [][2]string) {
		t.Helper()
		a.applyMargins()
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("propriedades = %v, want %v", got, want)
		}
		got = nil
	}
	check([][2]string{
		{"video-margin-ratio-left", "0.10000"}, {"video-margin-ratio-right", "0.20000"},
		{"video-margin-ratio-top", "0.05556"}, {"video-margin-ratio-bottom", "0.11111"},
		{"osd-align-x", "left"}, {"osd-margin-x", "116"},
	})
	// Redimensionamento mantém margens em pixels e recalcula as proporções.
	a.W, a.H = 2000, 1440
	check([][2]string{
		{"video-margin-ratio-left", "0.05000"}, {"video-margin-ratio-right", "0.60000"},
		{"video-margin-ratio-top", "0.02778"}, {"video-margin-ratio-bottom", "0.55556"},
		{"osd-align-x", "left"}, {"osd-margin-x", "58"},
	})
	a.W, a.H = 1000, 720
	a.host.OverlayLeft = true
	check([][2]string{
		{"video-margin-ratio-left", "0.10000"}, {"video-margin-ratio-right", "0.20000"},
		{"video-margin-ratio-top", "0.05556"}, {"video-margin-ratio-bottom", "0.11111"},
		{"osd-align-x", "right"}, {"osd-margin-x", "216"},
	})
	a.host.Col, a.host.Cols, a.host.OverlayLeft = 61, 30, false
	check([][2]string{
		{"video-margin-ratio-left", "0.60000"}, {"video-margin-ratio-right", "0.10000"},
		{"video-margin-ratio-top", "0.05556"}, {"video-margin-ratio-bottom", "0.11111"},
		{"osd-align-x", "center"}, {"osd-margin-x", "0"},
	})
	a.host.Visible = false
	check(nil)
	a.sess = nil
	check(nil)
}

// O buraco do vídeo só abre com o primeiro quadro da sessão: antes dele, as
// células mostram o que a TUI escreveu ali ("Abrindo canal…").
func TestHoleWaitsForTheFirstFrame(t *testing.T) {
	a := &app{haveHost: true, host: hostPanel{Visible: true, Col: 1, Row: 1, Cols: 10, Rows: 5}}
	steps := []struct {
		name string
		set  func()
		want bool
	}{
		{"sem sessão", func() {}, false},
		{"sessão sem quadro", func() { a.sess = &session{} }, false},
		{"primeiro quadro", func() { a.sess.gotFirst.Store(true) }, true},
		{"painel escondido", func() { a.host.Visible = false }, false},
		{"sessão nova", func() { a.host.Visible, a.sess = true, &session{} }, false},
	}
	for _, s := range steps {
		s.set()
		a.refreshHole()
		if a.holeOn != s.want {
			t.Fatalf("%s: holeOn = %v, quer %v", s.name, a.holeOn, s.want)
		}
	}
}
