package ui

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/platform"
	"github.com/pedrosatin/ttv-tui/internal/store"
	"github.com/rivo/uniseg"
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

func TestTruncateMiddleVisualLargurasPequenas(t *testing.T) {
	label := "abcdefghij"
	for w := 1; w <= 5; w++ {
		got := TruncateMiddleVisual(label, w)
		if got == "" {
			t.Fatalf("largura %d devolveu string vazia", w)
		}
		if ansi.StringWidth(got) > w {
			t.Fatalf("largura %d: %q tem %d células", w, got, ansi.StringWidth(got))
		}
	}
	// Orçamento menor ou igual à reticência. Devolve o começo sem reticência.
	got := truncateMiddleVisual("abcdef", 3, "...")
	if strings.Contains(got, ".") {
		t.Fatalf("com orçamento 3 e reticência '...' não deveria usar reticência: %q", got)
	}
	if ansi.StringWidth(got) > 3 || got == "" {
		t.Fatalf("orçamento 3: %q", got)
	}
}

func TestTruncateMiddleVisualGrafemas(t *testing.T) {
	cases := []struct {
		name  string
		label string
		width int
	}{
		{name: "bandeira", label: "prefixo🇧🇷sufixoXXXX", width: 12},
		{name: "keycap", label: "prefixo1⃣sufixoYYYY", width: 12},
		{name: "nfd", label: "prefixoe\u0301sufixoZZZZ", width: 12},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := TruncateMiddleVisual(tc.label, tc.width)
			if ansi.StringWidth(got) > tc.width {
				t.Fatalf("passou de %d: %q (%d)", tc.width, got, ansi.StringWidth(got))
			}
			// Nenhum grafema do resultado pode ser um cluster partido.
			state := -1
			rest := got
			for len(rest) > 0 {
				cluster, next, _, nextState := uniseg.FirstGraphemeClusterInString(rest, state)
				if cluster == "" {
					break
				}
				if !utf8.ValidString(cluster) {
					t.Fatalf("cluster inválido em %q", got)
				}
				rest = next
				state = nextState
			}
			switch tc.name {
			case "bandeira":
				if !strings.Contains(got, "🇧🇷") && (strings.Contains(got, "\U0001F1E7") || strings.Contains(got, "\U0001F1F7")) {
					t.Fatalf("bandeira partida: %q", got)
				}
			case "keycap":
				if strings.Contains(got, "1") && !strings.Contains(got, "1⃣") && strings.Contains(got, "⃣") {
					t.Fatalf("keycap partido: %q", got)
				}
			case "nfd":
				if strings.Contains(got, "\u0301") && !strings.Contains(got, "e\u0301") {
					t.Fatalf("acento NFD partido: %q", got)
				}
			}
		})
	}

	// CJK em larguras 4 a 7. Um grafema no começo e um no fim quando couberem.
	cjk := "日本語カテゴリ名"
	for w := 4; w <= 7; w++ {
		got := TruncateMiddleVisual(cjk, w)
		if ansi.StringWidth(got) > w {
			t.Fatalf("CJK largura %d: %q tem %d", w, got, ansi.StringWidth(got))
		}
		cells := graphemeCells(got)
		if w >= 5 && strings.Contains(got, "…") {
			if len(cells) < 3 {
				t.Fatalf("CJK largura %d deveria ter começo+…+fim: %q cells=%d", w, got, len(cells))
			}
		}
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
			// Chat aberto força o split. Na tela de categorias sem painel a lista
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

// Caracteres largos (CJK e emoji) medem em células. A distinção e o teto de
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

func zeldaChannelNames() []string {
	return []string{
		"The Legend of Zelda: Ocarina of Time",
		"The Legend of Zelda: Ocarina of Time 3D",
	}
}

func TestNomesTruncadosCanaisModeChannels(t *testing.T) {
	names := zeldaChannelNames()
	m := newClassicModel(mustCatalog(t, i18n.LangPTBR))
	m.chat.panel.channel = "gaules"
	m.list.loading = false
	m.list.mode = ModeChannels
	m.list.channels = []catalog.Channel{
		{ID: "ch-a", Login: names[0], DisplayName: names[0], Availability: catalog.AvailabilityLive, Viewers: catalog.IntPtr(10)},
		{ID: "ch-b", Login: names[1], DisplayName: names[1], Availability: catalog.AvailabilityLive, Viewers: catalog.IntPtr(9)},
	}
	m.list.highlightedID = "ch-a"
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)
	view := ansi.Strip(m.View())
	var shown []string
	for _, row := range strings.Split(view, "\n") {
		if strings.Contains(row, "Time") || strings.Contains(row, "3D") || strings.Contains(row, "Legen") {
			shown = append(shown, strings.TrimRight(row, " "))
		}
	}
	if len(shown) != 2 {
		t.Fatalf("queria 2 canais Zelda, veio %d:\n%s", len(shown), view)
	}
	if shown[0] == shown[1] {
		t.Fatalf("canais iguais:\n1: %q\n2: %q", shown[0], shown[1])
	}
	if !strings.Contains(shown[0], "Time") || !strings.Contains(shown[1], "3D") {
		t.Fatalf("sufixo distintivo sumiu:\n1: %q\n2: %q", shown[0], shown[1])
	}
}

func TestNomesTruncadosCanaisBusca(t *testing.T) {
	names := zeldaChannelNames()
	m := newClassicModel(mustCatalog(t, i18n.LangPTBR))
	m.chat.panel.channel = "gaules"
	m.list.loading = false
	m.list.mode = ModeSearch
	m.list.search.query = "zelda"
	m.list.search.active = true
	m.list.channels = []catalog.Channel{
		{ID: "ch-a", Login: names[0], DisplayName: names[0], Availability: catalog.AvailabilityLive, Viewers: catalog.IntPtr(10)},
		{ID: "ch-b", Login: names[1], DisplayName: names[1], Availability: catalog.AvailabilityLive, Viewers: catalog.IntPtr(9)},
	}
	m.list.highlightedID = "ch-a"
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)
	view := ansi.Strip(m.View())
	var shown []string
	for _, row := range strings.Split(view, "\n") {
		if strings.Contains(row, "Time") || strings.Contains(row, "3D") || strings.Contains(row, "Legen") {
			shown = append(shown, strings.TrimRight(row, " "))
		}
	}
	if len(shown) != 2 {
		t.Fatalf("queria 2 canais na busca, veio %d:\n%s", len(shown), view)
	}
	if shown[0] == shown[1] {
		t.Fatalf("busca com linhas iguais:\n1: %q\n2: %q", shown[0], shown[1])
	}
}

func TestNomesTruncadosCanaisComTituloEmoji(t *testing.T) {
	names := zeldaChannelNames()
	titleA := "🔥 live com emoji no título bem longo para ocupar espaço"
	titleB := "🔥 outra live também longa com texto demais na linha"
	m := newClassicModel(mustCatalog(t, i18n.LangPTBR))
	m.chat.panel.channel = "gaules"
	m.list.loading = false
	m.list.mode = ModeChannels
	// Login igual ao DisplayName evita o sufixo (login) na identidade.
	m.list.channels = []catalog.Channel{
		{ID: "ch-a", Login: names[0], DisplayName: names[0], Title: &titleA, Availability: catalog.AvailabilityLive, Viewers: catalog.IntPtr(10)},
		{ID: "ch-b", Login: names[1], DisplayName: names[1], Title: &titleB, Availability: catalog.AvailabilityLive, Viewers: catalog.IntPtr(9)},
	}
	m.list.highlightedID = "ch-a"
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)
	view := ansi.Strip(m.View())
	var shown []string
	for _, row := range strings.Split(view, "\n") {
		if strings.Contains(row, "Time") || strings.Contains(row, "3D") || strings.Contains(row, "Zelda") || strings.Contains(row, "Legen") {
			shown = append(shown, strings.TrimRight(row, " "))
		}
	}
	if len(shown) < 2 {
		t.Fatalf("queria linhas dos dois canais, veio %d:\n%s", len(shown), view)
	}
	if shown[0] == shown[1] {
		t.Fatalf("linhas com título iguais:\n1: %q\n2: %q", shown[0], shown[1])
	}
	joined := strings.Join(shown, "\n")
	if !strings.Contains(joined, "3D") {
		t.Fatalf("sufixo 3D da identidade sumiu com título largo:\n%s", view)
	}
	for _, row := range strings.Split(view, "\n") {
		if w := ansi.StringWidth(row); w > 80 {
			t.Fatalf("linha com %d colunas: %q", w, row)
		}
	}
}

func TestNomesTruncadosCanaisTagMista(t *testing.T) {
	names := zeldaChannelNames()
	m := newClassicModel(mustCatalog(t, i18n.LangPTBR))
	m.chat.panel.channel = "gaules"
	m.list.loading = false
	m.list.mode = ModeChannels
	m.list.channels = []catalog.Channel{
		{ID: "ch-a", Login: names[0], DisplayName: names[0], Availability: catalog.AvailabilityLive, Viewers: catalog.IntPtr(10)},
		{ID: "ch-b", Login: names[1], DisplayName: names[1], Availability: catalog.AvailabilityOffline},
	}
	m.list.highlightedID = "ch-a"
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)
	listW, _ := m.panelWidths()
	items := m.VisibleItems()
	tagW := m.listTagWidth(items, listW)
	liveBudget, _ := m.listItemNameBudget(items[0], listW, tagW, false)
	offBudget, _ := m.listItemNameBudget(items[1], listW, tagW, false)
	if liveBudget <= offBudget {
		t.Fatalf("canal ao vivo sem tag própria deveria ter orçamento maior: live=%d offline=%d tagW=%d listW=%d", liveBudget, offBudget, tagW, listW)
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, m.t("channels.offline_tag")) {
		t.Fatalf("tag offline sumiu:\n%s", view)
	}
	for _, row := range strings.Split(view, "\n") {
		if w := ansi.StringWidth(row); w > 80 {
			t.Fatalf("linha com %d colunas: %q", w, row)
		}
	}
}

func TestNomesTruncadosPinnedSemViewers(t *testing.T) {
	m := newClassicModel(mustCatalog(t, i18n.LangPTBR))
	m.chat.panel.channel = "gaules"
	m.list.loading = false
	m.list.mode = ModeCategories
	m.list.platform = platform.Twitch
	m.list.favoriteCategories = []store.Category{
		{Platform: platform.Twitch, ID: "pin-a", Name: "The Legend of Zelda: Ocarina of Time"},
		{Platform: platform.Twitch, ID: "pin-b", Name: "The Legend of Zelda: Ocarina of Time 3D"},
	}
	// Sem entradas em list.categories. Viewers fica nil e a linha usa a tag
	// [não verificado] via fitChannelRow.
	m.list.categories = nil
	m.list.highlightedID = "pin-a"
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)
	view := ansi.Strip(m.View())
	tag := m.t("channels.unverified_tag")
	var shown []string
	for _, row := range strings.Split(view, "\n") {
		if strings.Contains(row, "Legen") || strings.Contains(row, "Time") || strings.Contains(row, "3D") {
			shown = append(shown, strings.TrimRight(row, " "))
		}
	}
	if len(shown) != 2 {
		t.Fatalf("queria 2 fixadas, veio %d:\n%s", len(shown), view)
	}
	if shown[0] == shown[1] {
		t.Fatalf("fixadas iguais:\n1: %q\n2: %q", shown[0], shown[1])
	}
	for _, row := range shown {
		if !strings.Contains(row, tag) {
			t.Fatalf("fixada sem %q: %q", tag, row)
		}
	}
	if !strings.Contains(shown[0], "Time") || !strings.Contains(shown[1], "3D") {
		t.Fatalf("sufixo distintivo sumiu nas fixadas:\n1: %q\n2: %q", shown[0], shown[1])
	}
}

func TestNomesTruncadosTresColidindo(t *testing.T) {
	names := []string{
		"The Legend of Zelda: Ocarina of Time",
		"The Legend of Zelda: Ocarina of Time 3D",
		"The Legend of Zelda: Ocarina of Time HD",
	}
	m := newClassicModel(mustCatalog(t, i18n.LangPTBR))
	m.chat.panel.channel = "gaules"
	m.list.loading = false
	m.list.mode = ModeCategories
	m.list.categories = []catalog.Category{
		{ID: "a", Name: names[0], Viewers: catalog.IntPtr(3)},
		{ID: "b", Name: names[1], Viewers: catalog.IntPtr(2)},
		{ID: "c", Name: names[2], Viewers: catalog.IntPtr(1)},
	}
	m.list.highlightedID = "a"
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)
	view := ansi.Strip(m.View())
	var shown []string
	for _, row := range strings.Split(view, "\n") {
		if strings.Contains(row, "Zelda") {
			shown = append(shown, strings.TrimRight(row, " "))
		}
	}
	if len(shown) != 3 {
		t.Fatalf("queria 3 linhas, veio %d:\n%s", len(shown), view)
	}
	if shown[0] == shown[1] || shown[1] == shown[2] || shown[0] == shown[2] {
		t.Fatalf("alguma linha repetida:\n1: %q\n2: %q\n3: %q", shown[0], shown[1], shown[2])
	}
}

func TestNomesIdenticosNaoCortamNoMeio(t *testing.T) {
	name := "The Legend of Zelda: Ocarina of Time Extended Edition"
	m := newClassicModel(mustCatalog(t, i18n.LangPTBR))
	m.chat.panel.channel = "gaules"
	m.list.loading = false
	m.list.mode = ModeCategories
	m.list.categories = []catalog.Category{
		{ID: "a", Name: name, Viewers: catalog.IntPtr(3)},
		{ID: "b", Name: name, Viewers: catalog.IntPtr(2)},
	}
	m.list.highlightedID = "a"
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)
	items := m.VisibleItems()
	listW, _ := m.panelWidths()
	rows := m.listRowPositions(items, false, listW)
	distinct := m.listDistinctNames(items, rows, listW, m.listTagWidth(items, listW), false)
	if len(distinct) != 0 {
		t.Fatalf("nomes idênticos não deveriam marcar distinct: %v", distinct)
	}
	view := ansi.Strip(m.View())
	var shown []string
	for _, row := range strings.Split(view, "\n") {
		listPart := row
		if i := strings.Index(row, "│"); i >= 0 {
			listPart = row[:i]
		}
		if strings.Contains(listPart, "Zelda") || strings.Contains(listPart, "Legend") {
			shown = append(shown, strings.TrimRight(listPart, " "))
		}
	}
	if len(shown) != 2 {
		t.Fatalf("queria 2 linhas idênticas, veio %d:\n%s", len(shown), view)
	}
	// Ignora marcador e audiência. Compara só o trecho do nome.
	nameOf := func(row string) string {
		idx := strings.Index(row, "The Legend")
		if idx < 0 {
			return strings.TrimSpace(row)
		}
		return strings.TrimSpace(row[idx:])
	}
	if nameOf(shown[0]) != nameOf(shown[1]) {
		t.Fatalf("nomes idênticos deveriam continuar iguais no corte do fim:\n1: %q\n2: %q", nameOf(shown[0]), nameOf(shown[1]))
	}
	for _, row := range shown {
		trim := strings.TrimRight(row, " ")
		if strings.Contains(trim, "…") && !strings.HasSuffix(trim, "…") && !strings.HasSuffix(trim, "...") {
			t.Fatalf("nome idêntico parece cortado no meio: %q", row)
		}
	}
}

func TestNomesTruncadosIndiceNaoID(t *testing.T) {
	// Categoria e canal com o mesmo ID na busca. A chave distinct é o índice.
	m := newClassicModel(mustCatalog(t, i18n.LangPTBR))
	m.chat.panel.channel = "gaules"
	m.list.loading = false
	m.list.mode = ModeSearch
	m.list.search.query = "same"
	m.list.search.active = true
	m.list.search.categories = []catalog.Category{
		{ID: "shared", Name: "The Legend of Zelda: Ocarina of Time", Viewers: catalog.IntPtr(5)},
	}
	m.list.channels = []catalog.Channel{
		{ID: "shared", Login: "The Legend of Zelda: Ocarina of Time 3D", DisplayName: "The Legend of Zelda: Ocarina of Time 3D", Availability: catalog.AvailabilityLive, Viewers: catalog.IntPtr(4)},
	}
	m.list.highlightedID = "shared"
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)
	items := m.VisibleItems()
	listW, _ := m.panelWidths()
	rows := m.listRowPositions(items, true, listW)
	start, end := 0, len(rows)
	if m.height > 0 {
		start, end = m.list.rowWindow(rows, m.listRowBudget(3, true, listW))
	}
	distinct := m.listDistinctNames(items, rows[start:end], listW, m.listTagWidth(items, listW), true)
	if len(distinct) < 2 {
		t.Fatalf("esperava distinct por índice para ID repetido, got %v items=%d", distinct, len(items))
	}
	view := ansi.Strip(m.View())
	var shown []string
	for _, row := range strings.Split(view, "\n") {
		if strings.Contains(row, "Zelda") {
			shown = append(shown, strings.TrimRight(row, " "))
		}
	}
	if len(shown) >= 2 && shown[0] == shown[1] {
		t.Fatalf("mesmo ID mas linhas iguais:\n1: %q\n2: %q\n%s", shown[0], shown[1], view)
	}
}

func TestNomesTruncadosRolagemJanela(t *testing.T) {
	m := newClassicModel(mustCatalog(t, i18n.LangPTBR))
	m.chat.panel.channel = "gaules"
	m.list.loading = false
	m.list.mode = ModeCategories
	cats := make([]catalog.Category, 0, 30)
	for i := 0; i < 25; i++ {
		cats = append(cats, catalog.Category{
			ID:      fmt.Sprintf("pad-%d", i),
			Name:    fmt.Sprintf("Padding category number %02d unique", i),
			Viewers: catalog.IntPtr(100 - i),
		})
	}
	cats = append(cats,
		catalog.Category{ID: "z1", Name: "The Legend of Zelda: Ocarina of Time", Viewers: catalog.IntPtr(2)},
		catalog.Category{ID: "z2", Name: "The Legend of Zelda: Ocarina of Time 3D", Viewers: catalog.IntPtr(1)},
	)
	m.list.categories = cats
	m.list.highlightedID = "pad-0"
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	items := m.VisibleItems()
	listW, _ := m.panelWidths()
	rows := m.listRowPositions(items, false, listW)
	used := 3
	start, end := m.list.rowWindow(rows, m.listRowBudget(used, false, listW))
	distinctTop := m.listDistinctNames(items, rows[start:end], listW, m.listTagWidth(items, listW), false)
	if len(distinctTop) != 0 {
		t.Fatalf("no topo as Zelda estão fora da janela; distinct=%v start=%d end=%d", distinctTop, start, end)
	}

	m.list.highlightedID = "z1"
	start, end = m.list.rowWindow(rows, m.listRowBudget(used, false, listW))
	distinctBottom := m.listDistinctNames(items, rows[start:end], listW, m.listTagWidth(items, listW), false)
	if len(distinctBottom) < 2 {
		t.Fatalf("com Zelda na janela esperava distinct, got %v start=%d end=%d", distinctBottom, start, end)
	}
}

func TestOrcamentoUnicoDeteccaoEDesenho(t *testing.T) {
	m := newClassicModel(mustCatalog(t, i18n.LangPTBR))
	m.list.prefs.languages = []string{"pt"}
	maxWidth := 40

	t.Run("pinned_sem_viewers", func(t *testing.T) {
		it := ListItem{Type: ItemTypePinnedCategory, Category: &catalog.Category{
			ID: "p", Name: "The Legend of Zelda: Ocarina of Time Extra",
		}}
		budget, ok := m.listItemNameBudget(it, maxWidth, 0, false)
		if !ok {
			t.Fatal("sem budget")
		}
		tag := m.t("channels.unverified_tag")
		body := listLabelBodyWidth(maxWidth, ansi.StringWidth(tag))
		want := listLabelBudget(body, listRowPrefixCells, sanitizeScreenText(it.Category.Name))
		if budget != want {
			t.Fatalf("budget=%d want=%d body=%d", budget, want, body)
		}
	})

	t.Run("canal_sem_tag", func(t *testing.T) {
		ch := catalog.Channel{ID: "1", Login: "live", DisplayName: "CanalAoVivoComNomeBemLongo", Availability: catalog.AvailabilityLive, Viewers: catalog.IntPtr(1), Language: catalog.LanguagePtr("pt")}
		it := ListItem{Type: ItemTypeChannel, Channel: &ch, Availability: catalog.AvailabilityLive}
		listTag := ansi.StringWidth(m.t("channels.offline_tag"))
		budget, ok := m.listItemNameBudget(it, maxWidth, listTag, false)
		if !ok {
			t.Fatal("sem budget")
		}
		// Sem tag própria. Desenho usa maxWidth inteiro, não desconta listTag.
		want := listLabelBudget(maxWidth, listRowPrefixCells, m.channelIdentity(ch))
		if budget != want {
			t.Fatalf("budget=%d want=%d (não deveria descontar tag global)", budget, want)
		}
	})

	t.Run("canal_com_tag", func(t *testing.T) {
		ch := catalog.Channel{ID: "2", Login: "off", DisplayName: "CanalOfflineComNomeBemLongo", Availability: catalog.AvailabilityOffline}
		it := ListItem{Type: ItemTypeChannel, Channel: &ch, Availability: catalog.AvailabilityOffline}
		tag := m.channelRowTag(ch, catalog.AvailabilityOffline, maxWidth)
		listTag := ansi.StringWidth(tag)
		budget, ok := m.listItemNameBudget(it, maxWidth, listTag, false)
		if !ok {
			t.Fatal("sem budget")
		}
		body := listLabelBodyWidth(maxWidth, listTag)
		want := listLabelBudget(body, listRowPrefixCells, m.channelIdentity(ch))
		if budget != want {
			t.Fatalf("budget=%d want=%d", budget, want)
		}
	})
}

// Em listW de terminal 80 em split, [idioma?] some só na linha cujo nome inteiro
// não cabe com o marcador. Nome curto no mesmo painel mantém o marcador. Em 100
// colunas o nome curto também mantém.
func TestMarcadorIdiomaNaoApertaNomeEm80(t *testing.T) {
	langSetup := func(width int, longName, shortName string) (Model, string) {
		m := newClassicModel(mustCatalog(t, i18n.LangPTBR))
		m.chat.panel.channel = "gaules"
		m.list.mode = ModeChannels
		m.list.loading = false
		m.list.prefs.languages = []string{"pt"}
		m.list.channels = []catalog.Channel{
			{ID: "1", Login: "sem_lang", DisplayName: longName, Availability: catalog.AvailabilityLive, Viewers: catalog.IntPtr(10)},
			{ID: "2", Login: "curto", DisplayName: shortName, Availability: catalog.AvailabilityLive, Viewers: catalog.IntPtr(9)},
		}
		m.list.highlightedID = "1"
		updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		return updated.(Model), updated.(Model).t("channels.lang_unknown_tag")
	}

	longName := "NomeBemLongoParaMedirOrcamentoDoPainelDaListaEApertar"
	shortName := "Curto"

	m, langTag := langSetup(80, longName, shortName)
	listW, _ := m.panelWidths()
	if listW >= 80 {
		t.Fatalf("preparação: listW=%d; o teste assume split em 80", listW)
	}
	plain := ansi.Strip(m.View())
	var longRow, shortRow string
	for _, row := range strings.Split(plain, "\n") {
		if strings.Contains(row, "NomeBemLongo") {
			longRow = strings.TrimRight(row, " ")
		}
		if strings.Contains(row, shortName) {
			shortRow = strings.TrimRight(row, " ")
		}
	}
	if longRow == "" {
		t.Fatalf("nome longo sumiu:\n%s", plain)
	}
	if strings.Contains(longRow, langTag) {
		t.Fatalf("nome longo em listW=%d ainda tem %q: %q", listW, langTag, longRow)
	}
	if shortRow == "" {
		t.Fatalf("nome curto sumiu:\n%s", plain)
	}
	if !strings.Contains(shortRow, langTag) {
		t.Fatalf("nome curto no mesmo painel deveria manter %q: %q\n%s", langTag, shortRow, plain)
	}

	m100, langTag100 := langSetup(100, longName, shortName)
	plain100 := ansi.Strip(m100.View())
	var short100 string
	for _, row := range strings.Split(plain100, "\n") {
		if strings.Contains(row, shortName) {
			short100 = strings.TrimRight(row, " ")
		}
	}
	if short100 == "" || !strings.Contains(short100, langTag100) {
		t.Fatalf("em 100 colunas o nome curto deveria ter %q: %q\n%s", langTag100, short100, plain100)
	}
}

func TestEndTruncatedListLabelOrcamentoMenorQueReticencia(t *testing.T) {
	label := "🔥nome"
	for budget := 1; budget <= 3; budget++ {
		got := endTruncatedListLabel(label, budget)
		if got == "" {
			t.Fatalf("budget %d devolveu vazio", budget)
		}
		if ansi.StringWidth(got) > budget {
			t.Fatalf("budget %d: %q tem %d", budget, got, ansi.StringWidth(got))
		}
		if budget <= 3 && strings.HasPrefix(got, "...") {
			t.Fatalf("budget %d não deveria começar com reticência ASCII: %q", budget, got)
		}
	}
}
