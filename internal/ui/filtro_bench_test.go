package ui

import (
	"fmt"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

// filterBenchModel monta uma listagem grande. unicodeEvery > 0 põe emoji no
// título de um a cada unicodeEvery canais; o resto é ASCII.
func filterBenchModel(b *testing.B, n, unicodeEvery int) Model {
	b.Helper()
	cat, err := i18n.New(i18n.LangEN)
	if err != nil {
		b.Fatal(err)
	}
	m := newClassicModel(cat)
	m.list.mode = ModeChannels
	m.list.channels = make([]catalog.Channel, n)
	for i := range n {
		title := fmt.Sprintf("Ranked grind day %d with viewers, come hang out", i)
		if unicodeEvery > 0 && i%unicodeEvery == 0 {
			title = fmt.Sprintf("❤️ Ranked grind day %d 👩‍💻 come hang out", i)
		}
		m.list.channels[i] = catalog.Channel{
			ID:           fmt.Sprint(i),
			Login:        fmt.Sprintf("streamer_%d", i),
			DisplayName:  fmt.Sprintf("Streamer_%d", i),
			Title:        catalog.StringPtr(title),
			Availability: catalog.AvailabilityLive,
		}
	}
	m.list.filter = "gaules"
	return m
}

// Custo de uma tecla no filtro: VisibleItems percorre a listagem inteira.
func BenchmarkVisibleItemsFiltro(b *testing.B) {
	for _, tc := range []struct {
		name         string
		unicodeEvery int
	}{{"ascii", 0}, {"misto10pct", 10}} {
		b.Run(tc.name, func(b *testing.B) {
			m := filterBenchModel(b, 5000, tc.unicodeEvery)
			b.ReportAllocs()
			for b.Loop() {
				if len(m.VisibleItems()) != 0 {
					b.Fatal("o filtro não deveria casar com nenhum canal")
				}
			}
		})
	}
}
