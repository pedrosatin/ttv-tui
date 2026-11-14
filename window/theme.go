package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	defaultColor = uint32(0xff000000)
	// Fundo com cor própria (seleção) no chat sobre o vídeo nunca fica mais fraco que isto.
	glassSolidAlpha = uint32(225)
)

var (
	bgR, bgG, bgB byte = 0x1c, 0x1c, 0x1e
	fgR, fgG, fgB byte = 0xf5, 0xf5, 0xf7
	// palette é a paleta ANSI de 16 cores; o tema do foot sobrescreve em loadTheme.
	palette = [16]uint32{
		0x1c1c1e, 0xff5f4a, 0xff9f0a, 0xffc27a, 0x8fb8de, 0xff8fa3, 0xa6dcef, 0xf5f5f7,
		0x8e8e93, 0xff8373, 0xffb340, 0xffd8a8, 0xb5d0ea, 0xffb3c1, 0xcdeefa, 0xffffff,
	}
)

// loadTheme lê as cores do tema atual do foot; sem o arquivo, ficam os padrões.
func loadTheme() {
	b, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".local/state/omarchy/current/theme/foot.ini"))
	if err != nil {
		return
	}
	for _, l := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(l), "=")
		if !ok {
			continue
		}
		n, err := strconv.ParseUint(strings.TrimSpace(v), 16, 32)
		if err != nil {
			continue
		}
		c := uint32(n)
		switch {
		case k == "foreground":
			fgR, fgG, fgB = byte(c>>16), byte(c>>8), byte(c)
		case k == "background":
			bgR, bgG, bgB = byte(c>>16), byte(c>>8), byte(c)
		case strings.HasPrefix(k, "regular") && len(k) == 8 && k[7] >= '0' && k[7] <= '7':
			palette[k[7]-'0'] = c
		case strings.HasPrefix(k, "bright") && len(k) == 7 && k[6] >= '0' && k[6] <= '7':
			palette[8+k[6]-'0'] = c
		}
	}
}
