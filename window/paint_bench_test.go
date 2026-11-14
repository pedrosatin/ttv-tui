package main

// Custo de pintar a grade da TUI no painel, sem janela: a tela inteira de uma
// janela de monitor grande, com emoji nos títulos da lista e no chat, como
// depois de uma tecla que move as colunas (Ctrl+→ desloca todas as células).

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/vt"
)

const benchCols, benchRows = 182, 48

// benchScreen é uma tela de três colunas: lista, vídeo (vazio) e chat.
func benchScreen() string {
	titles := []string{
		"🔴 AO VIVO — ranked até o topo 🏆🔥 !sorteio",
		"Just chatting ☕ conversa e react 😂👀",
		"CAMPEONATO 🇧🇷 final hoje ⚽ !pix",
		"speedrun any% 🎮 tentativa nº 4012 😤",
	}
	chat := []string{"KEKW que jogada 😂😂😂", "boa noite chat 👋", "🔥🔥🔥 GG", "alguém sabe a música? 🎵", "PogChamp"}
	var sb strings.Builder
	sb.WriteString("\x1b[H")
	for y := range benchRows - 1 {
		fmt.Fprintf(&sb, "\x1b[%d;1H\x1b[1;36mCanal%02d\x1b[0m %s", y+1, y, titles[y%len(titles)])
		fmt.Fprintf(&sb, "\x1b[%d;44H│", y+1)
		fmt.Fprintf(&sb, "\x1b[%d;134H│\x1b[35mUser%d\x1b[0m: %s", y+1, y, chat[y%len(chat)])
	}
	fmt.Fprintf(&sb, "\x1b[%d;1H\x1b[7m ttv-tui | Lista: 42 colunas · vol 100 · Conectado a #canal00 \x1b[0m", benchRows)
	return sb.String()
}

func benchPaintApp(b *testing.B) *app {
	b.Helper()
	fs, err := scaledFontSet(9, 2)
	if err != nil {
		b.Skip("sem fonte do sistema:", err)
	}
	a := &app{fs: fs, term: vt.NewSafeEmulator(benchCols, benchRows), cols: benchCols, rows: benchRows}
	a.pw, a.ph = benchCols*fs.cellW, benchRows*fs.cellH
	a.buf = make([]byte, a.pw*a.ph*4)
	a.prev = make([]cellKey, benchCols*benchRows)
	if _, err := a.term.Write([]byte(benchScreen())); err != nil {
		b.Fatal(err)
	}
	a.paint() // o primeiro quadro enche o cache de glifos
	return a
}

// BenchmarkPaintFull repinta todas as células, com o cache de glifos quente.
func BenchmarkPaintFull(b *testing.B) {
	a := benchPaintApp(b)
	for b.Loop() {
		clear(a.prev)
		a.paint()
	}
}

// BenchmarkPaintUnchanged é a passada por uma grade que não mudou.
func BenchmarkPaintUnchanged(b *testing.B) {
	a := benchPaintApp(b)
	for b.Loop() {
		a.paint()
	}
}

// BenchmarkEmojiGlyphCold é o primeiro desenho de um emoji colorido: o PNG da
// fonte decodificado e escalado para a célula. Depois dele o glifo fica no cache.
func BenchmarkEmojiGlyphCold(b *testing.B) {
	fs, err := scaledFontSet(9, 2)
	if err != nil {
		b.Skip("sem fonte do sistema:", err)
	}
	if fs.get('😂', false, 2) == nil {
		b.Skip("sem fonte de emoji colorida")
	}
	for b.Loop() {
		clear(fs.cache)
		fs.get('😂', false, 2)
	}
}
