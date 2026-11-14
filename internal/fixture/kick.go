package fixture

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

// DefaultKickCatalog são as categorias da Kick de fixture: cinco, com nomes que
// não se confundem com os da Twitch de fixture. Nenhuma tem live.
func DefaultKickCatalog() Catalog {
	cats := make([]Category, 5)
	for i := range cats {
		n := i + 1
		cats[i] = Category{ID: strconv.Itoa(5000 + n), Name: fmt.Sprintf("Kick Categoria %02d", n), Viewers: 50000 - i*1000}
	}
	return Catalog{Categories: cats}
}

// KickHandler responde às rotas do site da Kick que internal/kick lê no
// catálogo, no formato delas: as categorias de cat numa página só, as lives de
// uma categoria e as duas buscas, sempre vazias. Outra rota recebe 404.
func KickHandler(cat Catalog) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "método inválido", http.StatusMethodNotAllowed)
			return
		}
		var body any
		switch r.URL.Path {
		case "/api/v1/subcategories":
			body = cat.kickCategories()
		case "/api/v1/livestreams":
			body = map[string]any{"data": map[string]any{"livestreams": []any{}, "pagination": map[string]any{}}}
		case "/api/v1/channels/search", "/api/v1/categories/search":
			body = map[string]any{"data": []any{}}
		default:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	})
}

// kickCategories é a página das categorias: todas na primeira, sem próxima.
func (c Catalog) kickCategories() map[string]any {
	rows := make([]any, 0, len(c.Categories))
	for _, cat := range c.Categories {
		id, _ := strconv.ParseInt(cat.ID, 10, 64)
		rows = append(rows, map[string]any{"id": id, "name": cat.Name, "viewers": cat.Viewers})
	}
	return map[string]any{"current_page": 1, "data": rows, "next_page_url": nil}
}
