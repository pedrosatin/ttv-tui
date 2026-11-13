package fixture

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
)

// Channel é um canal ao vivo do catálogo de fixture.
type Channel struct {
	Login, Name, Title, Language string
	Viewers                      int
}

// Category é uma categoria do catálogo de fixture, com os canais ao vivo nela.
type Category struct {
	ID, Name string
	Viewers  int
	Channels []Channel
}

// Catalog é o que o GQL de fixture responde.
type Catalog struct {
	Categories []Category
}

// DefaultCatalog tem 60 categorias, mais que uma tela de lista. A primeira tem
// três canais e as demais, um canal cada.
func DefaultCatalog() Catalog {
	cats := make([]Category, 60)
	for i := range cats {
		n := i + 1
		cats[i] = Category{
			ID:      fmt.Sprintf("%d", 1000+n),
			Name:    fmt.Sprintf("Categoria %02d", n),
			Viewers: 90000 - i*1000,
			Channels: []Channel{{
				Login: fmt.Sprintf("canal_%02d", n), Name: fmt.Sprintf("Canal%02d", n),
				Title: fmt.Sprintf("Live da categoria %02d", n), Language: "pt", Viewers: 500 - i,
			}},
		}
	}
	cats[0].Channels = []Channel{
		{Login: "fixture_um", Name: "FixtureUm", Title: "Primeira live de fixture", Language: "pt", Viewers: 3000},
		{Login: "fixture_dois", Name: "FixtureDois", Title: "Segunda live de fixture", Language: "pt", Viewers: 2000},
		{Login: "fixture_tres", Name: "FixtureTres", Title: "Terceira live de fixture", Language: "en", Viewers: 1000},
	}
	return Catalog{Categories: cats}
}

// GQLHandler responde às consultas que internal/twitch faz, no formato da Twitch.
// Uma consulta que ele não conhece recebe 400, para o teste notar a mudança.
func GQLHandler(cat Catalog) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&req) != nil {
			http.Error(w, "pedido inválido", http.StatusBadRequest)
			return
		}
		data, ok := cat.answer(req.Query, req.Variables)
		if !ok {
			http.Error(w, "consulta desconhecida", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	})
}

// NewGQLServer sobe o GQL de fixture em 127.0.0.1. O endereço para o produto é
// URL + "/gql".
func NewGQLServer(cat Catalog) *httptest.Server {
	return httptest.NewServer(GQLHandler(cat))
}

func (c Catalog) answer(query string, vars map[string]any) (any, bool) {
	str := func(k string) string { s, _ := vars[k].(string); return s }
	switch {
	case strings.Contains(query, "games(first:"):
		return map[string]any{"games": c.gamesPage()}, true
	case strings.Contains(query, "game(id:"):
		cat, ok := c.find(str("id"), str("name"))
		if !ok {
			return map[string]any{"game": nil}, true
		}
		return map[string]any{"game": cat.streamsPage()}, true
	case strings.Contains(query, "searchFor") && strings.Contains(query, "channels {"):
		return map[string]any{"searchFor": map[string]any{"channels": c.searchChannels(str("q"))}}, true
	case strings.Contains(query, "searchFor") && strings.Contains(query, "games {"):
		return map[string]any{"searchFor": map[string]any{"games": c.searchGames(str("q"))}}, true
	case strings.Contains(query, "users(logins:"):
		var logins []string
		if raw, ok := vars["logins"].([]any); ok {
			for _, v := range raw {
				if s, ok := v.(string); ok {
					logins = append(logins, s)
				}
			}
		}
		return map[string]any{"users": c.users(logins)}, true
	}
	return nil, false
}

func (c Catalog) find(id, name string) (Category, bool) {
	for _, cat := range c.Categories {
		if (id != "" && cat.ID == id) || (name != "" && strings.EqualFold(cat.Name, name)) {
			return cat, true
		}
	}
	return Category{}, false
}

func (c Catalog) gamesPage() map[string]any {
	edges := make([]any, 0, len(c.Categories))
	for i, cat := range c.Categories {
		edges = append(edges, map[string]any{"cursor": fmt.Sprint(i + 1), "node": cat.node()})
	}
	return map[string]any{"edges": edges, "pageInfo": map[string]any{"hasNextPage": false}}
}

func (cat Category) node() map[string]any {
	return map[string]any{"id": cat.ID, "name": cat.Name, "viewersCount": cat.Viewers}
}

func (cat Category) streamsPage() map[string]any {
	edges := make([]any, 0, len(cat.Channels))
	for i, ch := range cat.Channels {
		edges = append(edges, map[string]any{
			"cursor": fmt.Sprint(i + 1),
			"node": map[string]any{
				"id":           "s-" + ch.Login,
				"broadcaster":  map[string]any{"id": "u-" + ch.Login, "login": ch.Login, "displayName": ch.Name},
				"language":     ch.Language,
				"viewersCount": ch.Viewers,
				"title":        ch.Title,
			},
		})
	}
	return map[string]any{
		"id": cat.ID, "name": cat.Name,
		"streams": map[string]any{"edges": edges, "pageInfo": map[string]any{"hasNextPage": false}},
	}
}

func (cat Category) user(ch Channel) map[string]any {
	return map[string]any{
		"id": "u-" + ch.Login, "login": ch.Login, "displayName": ch.Name,
		"stream": map[string]any{
			"id": "s-" + ch.Login, "language": ch.Language, "viewersCount": ch.Viewers, "title": ch.Title,
			"game": map[string]any{"id": cat.ID, "name": cat.Name},
		},
	}
}

func (c Catalog) searchChannels(q string) map[string]any {
	q = strings.ToLower(strings.TrimSpace(q))
	edges := []any{}
	for _, cat := range c.Categories {
		for _, ch := range cat.Channels {
			if q != "" && (strings.Contains(ch.Login, q) || strings.Contains(strings.ToLower(ch.Name), q)) {
				edges = append(edges, map[string]any{"item": cat.user(ch)})
			}
		}
	}
	return map[string]any{"cursor": nil, "edges": edges}
}

func (c Catalog) searchGames(q string) map[string]any {
	q = strings.ToLower(strings.TrimSpace(q))
	edges := []any{}
	for _, cat := range c.Categories {
		if q != "" && strings.Contains(strings.ToLower(cat.Name), q) {
			edges = append(edges, map[string]any{"item": cat.node()})
		}
	}
	return map[string]any{"edges": edges}
}

// users segue a Twitch: um login que não existe vira null na mesma posição.
func (c Catalog) users(logins []string) []any {
	out := make([]any, len(logins))
	for i, login := range logins {
		for _, cat := range c.Categories {
			if j := slices.IndexFunc(cat.Channels, func(ch Channel) bool { return strings.EqualFold(ch.Login, login) }); j >= 0 {
				out[i] = cat.user(cat.Channels[j])
			}
		}
	}
	return out
}
