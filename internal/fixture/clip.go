package fixture

import (
	"fmt"
	"io"
	"time"
)

// O clipe de fixture é um quadro de cor única em YUV4MPEG2, que o mpv lê de um
// pipe sem decodificador nem rede. A cor é um vermelho de BT.601 em faixa
// limitada: na tela vira um vermelho forte, longe do fundo escuro da janela e
// do texto da TUI.
const (
	ClipWidth  = 160
	ClipHeight = 90
	ClipFPS    = 30

	clipY, clipU, clipV = 81, 90, 240
)

// ClipLooksRight diz se um pixel RGB da tela é do clipe, com folga para a
// conversão de cor e a escala.
func ClipLooksRight(r, g, b int) bool {
	return r >= 170 && g <= 90 && b <= 90
}

// OpenDelayEnv é a variável com que um cenário pede ao streamlink falso que
// espere antes de abrir o player, no formato de time.ParseDuration ("3s").
const OpenDelayEnv = "TTV_FIXTURE_OPEN_DELAY"

// WriteClip escreve o cabeçalho e frames quadros (0 = até a escrita falhar), no
// ritmo de ClipFPS depois de um segundo adiantado. O ritmo poupa o pipe: o mpv
// lê adiantado tudo o que recebe.
func WriteClip(w io.Writer, frames int) error {
	if _, err := fmt.Fprintf(w, "YUV4MPEG2 W%d H%d F%d:1 Ip A1:1 C420mpeg2\n", ClipWidth, ClipHeight, ClipFPS); err != nil {
		return fmt.Errorf("escrever o cabeçalho do clipe: %w", err)
	}
	frame := clipFrame()
	start := time.Now()
	for n := 0; frames == 0 || n < frames; n++ {
		if _, err := w.Write(frame); err != nil {
			return fmt.Errorf("escrever o quadro %d do clipe: %w", n, err)
		}
		if due := start.Add(time.Duration(n-ClipFPS) * time.Second / ClipFPS); time.Until(due) > 0 {
			time.Sleep(time.Until(due))
		}
	}
	return nil
}

func clipFrame() []byte {
	luma := ClipWidth * ClipHeight
	chroma := luma / 4
	b := []byte("FRAME\n")
	for range luma {
		b = append(b, clipY)
	}
	for range chroma {
		b = append(b, clipU)
	}
	for range chroma {
		b = append(b, clipV)
	}
	return b
}
