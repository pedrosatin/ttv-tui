//go:build cenarios && linux

package windowscenarios

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/fixture"
	"github.com/pedrosatin/ttv-tui/internal/ui"
)

// emojiTitle é um título de live como os da Twitch, cheio de emojis.
const emojiTitle = "🔴🎮🔥😂🏆👀🎉✨💜🚀🤣😎🙏💯 maratona"

// emojiCatalog é o catálogo padrão com um canal a mais no topo da Categoria 01,
// de nome curto e título cheio de emojis.
func emojiCatalog() fixture.Catalog {
	cat := fixture.DefaultCatalog()
	first := &cat.Categories[0]
	first.Channels = append([]fixture.Channel{{Login: "emojis", Name: "emojis", Title: emojiTitle, Language: "pt", Viewers: 9000}}, first.Channels...)
	return cat
}

// W12: os emojis do título aparecem com a fonte de emoji do sistema, e a
// janela não conta glifo sem fonte. Antes cada emoji virava um quadrado vazio.
func TestJanelaEmojiDoTituloTemFonte(t *testing.T) {
	t.Parallel()
	cat := emojiCatalog()
	w := openWindow(t, setup{catalog: &cat, config: "list_columns = 44\n"})
	w.press("Return")
	w.waitState("os canais da Categoria 01", 10*time.Second, func(s ui.StateReport) bool {
		return s.Mode == string(ui.ModeChannels) && s.Highlighted == "emojis"
	})
	w.waitScreen("🔴", 5*time.Second)

	expect(t, func() error {
		var stats struct{ Tofu int }
		if err := json.Unmarshal([]byte(w.must("stats")), &stats); err != nil {
			return err
		}
		screen := w.screen()
		row := rowWith(screen, "🔴")
		if row < 0 {
			return fmt.Errorf("o título com emojis saiu da tela")
		}
		shown := 0
		for _, r := range emojiTitle {
			if r >= 0x2190 && strings.ContainsRune(screen[row], r) {
				shown++
			}
		}
		if shown < 5 {
			return fmt.Errorf("só %d emojis do título chegaram à tela: %q", shown, screen[row])
		}
		if stats.Tofu > 0 {
			return fmt.Errorf("%d glifos sem fonte com %d emojis na tela: %q", stats.Tofu, shown, screen[row])
		}
		return nil
	})

	// Com os emojis na lista, o vídeo segue chegando.
	w.press("Return")
	w.waitState("o player tocando emojis", 15*time.Second, func(s ui.StateReport) bool {
		return s.Player == string(ui.PlayerStatePlaying)
	})
	w.must("stats", "reset")
	expect(t, func() error {
		var stats struct{ VideoFrames, Tofu int }
		if err := json.Unmarshal([]byte(w.must("stats")), &stats); err != nil {
			return err
		}
		if stats.VideoFrames < 5 || stats.Tofu > 0 {
			return fmt.Errorf("%d quadros de vídeo desde o reset, tofu %d", stats.VideoFrames, stats.Tofu)
		}
		return nil
	})
}
