package twitch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
)

// Login da GQL fora de ^[a-z0-9_]{1,25}$ não vira canal: seria alvo de JOIN e
// PRIVMSG na conexão autenticada. Os outros canais da resposta seguem.
func TestGQLDescartaLoginForaDaGramatica(t *testing.T) {
	const invalidos = `"a,#b\r\nX", "x y", "Gaules", "a#b"`
	for _, tc := range []struct {
		name string
		body string
		call func(*GQLSource) ([]catalog.Channel, error)
	}{
		{
			name: "canais da categoria",
			body: `{"data":{"game":{"id":"1","name":"Jogo","streams":{"edges":[
				{"cursor":"c1","node":{"id":"s1","broadcaster":{"id":"1","login":"a,#b\r\nX","displayName":"X"}}},
				{"cursor":"c2","node":{"id":"s2","broadcaster":{"id":"2","login":"x y","displayName":"Y"}}},
				{"cursor":"c3","node":{"id":"s3","broadcaster":{"id":"3","login":"Gaules","displayName":"G"}}},
				{"cursor":"c4","node":{"id":"s4","broadcaster":{"id":"4","login":"a#b","displayName":"Z"}}},
				{"cursor":"c5","node":{"id":"s5","broadcaster":{"id":"5","login":"gaules","displayName":"Gaules"}}}
			],"pageInfo":{"hasNextPage":false}}}}}`,
			call: func(s *GQLSource) ([]catalog.Channel, error) {
				p, err := s.Channels(context.Background(), "1", catalog.PageRequest{Limit: 10})
				return p.Items, err
			},
		},
		{
			name: "busca",
			body: `{"data":{"searchFor":{"channels":{"cursor":"","edges":[
				{"item":{"id":"1","login":"a,#b\r\nX","displayName":"X"}},
				{"item":{"id":"2","login":"x y","displayName":"Y"}},
				{"item":{"id":"3","login":"Gaules","displayName":"G"}},
				{"item":{"id":"4","login":"a#b","displayName":"Z"}},
				{"item":{"id":"5","login":"gaules","displayName":"Gaules"}}
			]}}}}`,
			call: func(s *GQLSource) ([]catalog.Channel, error) {
				p, err := s.Search(context.Background(), "gaules")
				return p.Items, err
			},
		},
		{
			name: "logins de favoritos",
			body: `{"data":{"users":[
				{"id":"1","login":"a,#b\r\nX","displayName":"X","stream":null},
				{"id":"2","login":"x y","displayName":"Y","stream":null},
				{"id":"3","login":"Gaules","displayName":"G","stream":null},
				{"id":"4","login":"a#b","displayName":"Z","stream":null},
				{"id":"5","login":"gaules","displayName":"Gaules","stream":null}
			]}}`,
			call: func(s *GQLSource) ([]catalog.Channel, error) {
				return s.ChannelsByLogins(context.Background(), []string{"gaules", "outro"})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			src := NewGQLSource(GQLConfig{Endpoint: srv.URL, HTTPClient: srv.Client()})
			got, err := tc.call(src)
			if err != nil {
				t.Fatalf("erro: %v", err)
			}
			if len(got) != 1 || got[0].Login != "gaules" {
				t.Fatalf("canais = %+v; want só gaules (descartados: %s)", got, invalidos)
			}
		})
	}
}
