package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

func TestTruncateMiddleVisual(t *testing.T) {
	got := TruncateMiddleVisual("The Legend of Zelda: Ocarina of Time 3D", 24)
	if ansi.StringWidth(got) > 24 {
		t.Fatalf("passou de 24: %q (%d)", got, ansi.StringWidth(got))
	}
	if !strings.Contains(got, "…") {
		t.Fatalf("sem reticências: %q", got)
	}
	if !strings.HasPrefix(got, "The ") || !strings.HasSuffix(got, "3D") {
		t.Fatalf("não preservou começo e fim: %q", got)
	}
	if TruncateMiddleVisual("curto", 20) != "curto" {
		t.Fatal("cortou o que cabia")
	}
}

// Dois nomes com o mesmo prefixo, cortados só no fim, ficam iguais; com a
// distinção, cada linha mostra um fim diferente e nenhuma passa da largura.
func TestNomesTruncadosDistinguiveisEm80E120(t *testing.T) {
	names := []string{
		"The Legend of Zelda: Ocarina of Time",
		"The Legend of Zelda: Ocarina of Time 3D",
	}
	for _, width := range []int{80, 120} {
		t.Run(fmt.Sprintf("%d", width), func(t *testing.T) {
			m := newClassicModel(mustCatalog(t, i18n.LangPTBR))
			// Chat aberto força o split: na tela de categorias sem painel a lista
			// usa a largura inteira e esses nomes cabem sem cortar.
			m.chat.panel.channel = "gaules"
			m.list.loading = false
			m.list.mode = ModeCategories
			m.list.categories = []catalog.Category{
				{ID: "zelda-oot", Name: names[0], Viewers: catalog.IntPtr(1200)},
				{ID: "zelda-oot-3d", Name: names[1], Viewers: catalog.IntPtr(800)},
			}
			m.list.highlightedID = "zelda-oot"
			updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
			m = updated.(Model)
			listW, _ := m.panelWidths()
			if listW >= ansi.StringWidth(names[1])+listRowPrefixCells {
				t.Fatalf("preparação: listW=%d já cabe o nome inteiro; o cenário precisa truncar", listW)
			}
			view := ansi.Strip(m.View())
			rows := strings.Split(view, "\n")
			var shown []string
			for _, row := range rows {
				if strings.Contains(row, "Zelda") {
					shown = append(shown, strings.TrimRight(row, " "))
				}
			}
			if len(shown) != 2 {
				t.Fatalf("queria 2 linhas Zelda, veio %d:\n%s", len(shown), view)
			}
			if shown[0] == shown[1] {
				t.Fatalf("linhas iguais depois do truncamento:\n1: %q\n2: %q", shown[0], shown[1])
			}
			for i, row := range rows {
				if w := ansi.StringWidth(row); w > width {
					t.Fatalf("linha %d tem %d colunas em %d: %q", i+1, w, width, row)
				}
			}
			for i, row := range shown {
				if !strings.Contains(row, "…") && !strings.Contains(row, "...") {
					t.Fatalf("linha %d sem reticências: %q", i+1, row)
				}
			}
			if !strings.Contains(shown[0], "Time") || !strings.Contains(shown[1], "3D") {
				t.Fatalf("o fim distintivo sumiu:\n1: %q\n2: %q", shown[0], shown[1])
			}
		})
	}
}

// Caracteres largos (CJK e emoji) medem em células: a distinção e o teto de
// largura valem na mesma regra do TruncateVisual.
func TestNomesTruncadosDistinguiveisComCelulasLargas(t *testing.T) {
	names := []string{
		"日本語カテゴリ名がとても長いので切れるああああ",
		"日本語カテゴリ名がとても長いので切れるいいいい",
	}
	for _, width := range []int{80, 120} {
		t.Run(fmt.Sprintf("cjk/%d", width), func(t *testing.T) {
			m := newClassicModel(mustCatalog(t, i18n.LangPTBR))
			m.chat.panel.channel = "gaules"
			m.list.loading = false
			m.list.mode = ModeCategories
			m.list.categories = []catalog.Category{
				{ID: "cjk-a", Name: names[0], Viewers: catalog.IntPtr(10)},
				{ID: "cjk-b", Name: names[1], Viewers: catalog.IntPtr(9)},
			}
			m.list.highlightedID = "cjk-a"
			updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
			m = updated.(Model)
			view := ansi.Strip(m.View())
			var shown []string
			for _, row := range strings.Split(view, "\n") {
				if strings.Contains(row, "日本語") {
					shown = append(shown, strings.TrimRight(row, " "))
				}
				if w := ansi.StringWidth(row); w > width {
					t.Fatalf("linha com %d colunas em %d: %q", w, width, row)
				}
			}
			if len(shown) != 2 {
				t.Fatalf("queria 2 linhas CJK, veio %d:\n%s", len(shown), view)
			}
			if shown[0] == shown[1] {
				t.Fatalf("linhas CJK iguais:\n1: %q\n2: %q", shown[0], shown[1])
			}
		})
	}

	t.Run("emoji/80", func(t *testing.T) {
		m := newClassicModel(mustCatalog(t, i18n.LangPTBR))
		m.chat.panel.channel = "gaules"
		m.list.loading = false
		m.list.mode = ModeCategories
		m.list.categories = []catalog.Category{
			{ID: "em-a", Name: "🔥 categoria com prefixo longo igual aaa", Viewers: catalog.IntPtr(3)},
			{ID: "em-b", Name: "🔥 categoria com prefixo longo igual bbb", Viewers: catalog.IntPtr(2)},
		}
		m.list.highlightedID = "em-a"
		updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		m = updated.(Model)
		view := ansi.Strip(m.View())
		var shown []string
		for _, row := range strings.Split(view, "\n") {
			if strings.Contains(row, "igual aaa") || strings.Contains(row, "igual bbb") {
				shown = append(shown, strings.TrimRight(row, " "))
			}
			if w := ansi.StringWidth(row); w > 80 {
				t.Fatalf("linha com %d colunas: %q", w, row)
			}
		}
		if len(shown) != 2 {
			t.Fatalf("queria 2 linhas emoji, veio %d:\n%s", len(shown), view)
		}
		if shown[0] == shown[1] {
			t.Fatalf("linhas emoji iguais:\n1: %q\n2: %q", shown[0], shown[1])
		}
	})
}

// Em listW de terminal 80 em split, [idioma?] não reserva coluna: o nome fica
// com o mesmo orçamento de uma linha sem o marcador. Em painel largo o marcador
// permanece.
func TestMarcadorIdiomaNaoApertaNomeEm80(t *testing.T) {
	m := newClassicModel(mustCatalog(t, i18n.LangPTBR))
	m.chat.panel.channel = "gaules"
	m.list.mode = ModeChannels
	m.list.loading = false
	m.list.prefs.languages = []string{"pt"}
	longName := "NomeBemLongoParaMedirOrcamentoDoPainelDaLista"
	m.list.channels = []catalog.Channel{
		{ID: "1", Login: "sem_lang", DisplayName: longName, Availability: catalog.AvailabilityLive, Viewers: catalog.IntPtr(10)},
	}
	m.list.highlightedID = "1"
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)
	listW, _ := m.panelWidths()
	if listW >= 80 {
		t.Fatalf("preparação: listW=%d; o teste assume split em 80", listW)
	}
	plain := ansi.Strip(m.View())
	langTag := m.t("channels.lang_unknown_tag")
	if strings.Contains(plain, langTag) {
		t.Fatalf("em listW=%d o marcador %q apertaria o nome:\n%s", listW, langTag, plain)
	}
	if !strings.Contains(plain, "NomeBemLongo") {
		t.Fatalf("nome sumiu sem o marcador:\n%s", plain)
	}

	// Painel list-only 80: o marcador volta.
	m.layout.showChat = false
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)
	plain = ansi.Strip(m.View())
	if !strings.Contains(plain, langTag) {
		t.Fatalf("em list-only 80 o marcador deveria aparecer:\n%s", plain)
	}
}
