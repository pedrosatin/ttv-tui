package ui

import (
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/store"
)

// O filtro compara o texto como a tela o mostra (ticket 26). A tela tira o VS16
// de "❤️ live" e mostra "❤ live"; quem copia da tela e cola no filtro precisa
// achar o canal. O mesmo com a sequência ZWJ de "👩‍💻 dev", que a tela mostra
// como "👩 dev".
func TestFilterCasaComTextoCopiadoDaTela(t *testing.T) {
	for _, tc := range []struct{ name, title, filter string }{
		{"VS16 copiado da tela", "❤️ live", "❤ live"},
		{"VS16 digitado", "❤️ live", "❤️ live"},
		{"ZWJ copiado da tela", "👩‍💻 dev", "👩 dev"},
		{"ZWJ digitado", "👩‍💻 dev", "👩‍💻 dev"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newClassicModel(testCatalog(t, i18n.LangEN))
			m.list.mode = ModeChannels
			m.list.channels = []catalog.Channel{
				{ID: "1", Login: "streamer", DisplayName: "Streamer", Title: catalog.StringPtr(tc.title), Availability: catalog.AvailabilityLive},
				{ID: "2", Login: "other", DisplayName: "Other", Title: catalog.StringPtr("just chatting"), Availability: catalog.AvailabilityLive},
			}
			m.list.filter = tc.filter

			if vis := m.VisibleChannels(); len(vis) != 1 || vis[0].Login != "streamer" {
				t.Fatalf("VisibleChannels = %v, want só streamer", loginsOf(vis))
			}
			items := m.VisibleItems()
			if len(items) != 1 || items[0].Channel == nil || items[0].Channel.Login != "streamer" {
				t.Fatalf("VisibleItems = %+v, want só streamer", items)
			}
		})
	}
}

// O caminho de categorias de VisibleItems (appendCategories e as fixadas)
// filtra pelo texto da tela, mantém a ordem e não repete a fixada no catálogo.
func TestVisibleItemsCaminhoDeCategorias(t *testing.T) {
	cats := []catalog.Category{
		{ID: "c-jc", Name: "Just Chatting"},
		{ID: "c-bidi", Name: "an\u200ftes"},
		{ID: "c-music", Name: "❤️ Music"},
		{ID: "c-dev", Name: "👩‍💻 Software"},
	}
	newModel := func(t *testing.T) Model {
		t.Helper()
		m := newClassicModel(testCatalog(t, i18n.LangEN))
		m.list.mode = ModeCategories
		m.list.categories = cats
		m.list.favoriteCategories = []store.Category{{Platform: m.ListPlatform(), ID: "c-music", Name: "❤️ Music"}}
		return m
	}
	type row struct {
		id  string
		typ ListItemType
	}
	rows := func(items []ListItem) []row {
		out := make([]row, 0, len(items))
		for _, it := range items {
			if it.Category != nil {
				out = append(out, row{it.Category.ID, it.Type})
			}
		}
		return out
	}
	for _, tc := range []struct {
		filter string
		want   []row
	}{
		{"", []row{{"c-music", ItemTypePinnedCategory}, {"c-jc", ItemTypeCategory}, {"c-bidi", ItemTypeCategory}, {"c-dev", ItemTypeCategory}}},
		{"antes", []row{{"c-bidi", ItemTypeCategory}}},
		{"❤ mu", []row{{"c-music", ItemTypePinnedCategory}}},
		{"👩 soft", []row{{"c-dev", ItemTypeCategory}}},
		{"CHATTING", []row{{"c-jc", ItemTypeCategory}}},
		{"nada", []row{}},
	} {
		t.Run(tc.filter, func(t *testing.T) {
			m := newModel(t)
			m.list.filter = tc.filter
			got := rows(m.VisibleItems())
			if len(got) != len(tc.want) {
				t.Fatalf("VisibleItems = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("VisibleItems = %v, want %v", got, tc.want)
				}
			}
		})
	}

	t.Run("busca", func(t *testing.T) {
		m := newModel(t)
		m.list.mode = ModeSearch
		m.list.search.query = "music"
		m.list.search.categories = cats
		m.list.filter = "❤ music"
		items := m.VisibleItems()
		if len(items) != 1 || items[0].Category == nil || items[0].Category.ID != "c-music" || !items[0].Favorite {
			t.Fatalf("VisibleItems = %+v, want só c-music marcada como fixada", items)
		}
	})
}

// Com texto ASCII imprimível, o atalho de filterText dá o mesmo resultado que o
// caminho completo (sanitizeScreenText). Bytes de controle e não ASCII saem do
// atalho.
func TestFilterAtalhoASCIIEquivaleAoCaminhoCompleto(t *testing.T) {
	check := func(s string) {
		t.Helper()
		if !plainASCII(s) {
			t.Fatalf("plainASCII(%q) = false", s)
		}
		if fast, full := filterText(s), sanitizeScreenText(s); fast != full {
			t.Fatalf("filterText(%q) = %q, caminho completo = %q", s, fast, full)
		}
		if got, want := filterQuery(s), strings.ToLower(strings.TrimSpace(sanitizeScreenText(s))); got != want {
			t.Fatalf("filterQuery(%q) = %q, want %q", s, got, want)
		}
	}
	for a := byte(0x20); a < 0x7f; a++ {
		for b := byte(0x20); b < 0x7f; b++ {
			check(string([]byte{a, b}))
		}
	}
	rng := rand.New(rand.NewPCG(26, 26))
	for range 2000 {
		buf := make([]byte, rng.IntN(40))
		for i := range buf {
			buf[i] = byte(0x20 + rng.IntN(0x7f-0x20))
		}
		check(string(buf))
	}
	for _, s := range []string{"[31mred[0m", "]8;;http://x\\link", "Gaules  GAMEPLAY ", "~!@#$%^&*()_+{}|:\"<>?"} {
		check(s)
	}

	for c := range 0x20 {
		if plainASCII("a" + string(rune(c)) + "b") {
			t.Errorf("plainASCII aceitou o controle 0x%02x", c)
		}
	}
	for _, s := range []string{"a\x7fb", "\x1b[31mred", "café", "❤ live", "\u200b"} {
		if plainASCII(s) {
			t.Errorf("plainASCII(%q) = true", s)
		}
	}
	if got := filterQuery("\x1b[31mRed"); got != "red" {
		t.Errorf("filterQuery com ESC = %q, want %q", got, "red")
	}
}
