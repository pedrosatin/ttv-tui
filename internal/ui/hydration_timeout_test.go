package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

type httpStallingResolver struct {
	serverURL string
}

func (r *httpStallingResolver) ChannelsByIDs(ctx context.Context, _ []string) ([]catalog.Channel, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.serverURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	return nil, nil
}

type channelStallingResolver struct{}

func (r *channelStallingResolver) ChannelsByIDs(ctx context.Context, _ []string) ([]catalog.Channel, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestHydrationServidorQueNuncaRespondeTerminaNoPrazoEListaUsavel(t *testing.T) {
	// O prazo real é de segundos; o teste o encurta para não esperar o relógio.
	prev := hydrationTimeout
	hydrationTimeout = 200 * time.Millisecond
	t.Cleanup(func() { hydrationTimeout = prev })

	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	cases := []struct {
		name     string
		resolver catalog.Resolver
	}{
		{
			name:     "servidor httptest que nunca responde",
			resolver: &httpStallingResolver{serverURL: srv.URL},
		},
		{
			name:     "resolver bloqueante em canal",
			resolver: &channelStallingResolver{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			favChannel := catalog.Channel{
				ID:           "twitch:gaules",
				Platform:     platform.Twitch,
				Login:        "gaules",
				DisplayName:  "Gaules",
				Availability: catalog.AvailabilityUnknown,
			}

			m := New(mustCatalog(t, i18n.LangPTBR),
				WithResolver(tc.resolver),
				WithFavorites([]catalog.Channel{favChannel}),
			)

			cmd := m.fetchHydrateCmd(m.list.hydrateGen)
			if cmd == nil {
				t.Fatal("fetchHydrateCmd retornou nil com favoritos cadastrados")
			}

			start := time.Now()
			rawMsg := cmd()
			elapsed := time.Since(start)

			if elapsed < hydrationTimeout {
				t.Fatalf("hidratação terminou antes do prazo: %v (prazo = %v)", elapsed, hydrationTimeout)
			}
			if elapsed > hydrationTimeout+2*time.Second {
				t.Fatalf("hidratação ultrapassou o prazo tolerado: %v (prazo = %v)", elapsed, hydrationTimeout)
			}

			msg, ok := rawMsg.(SavedAvailabilityMsg)
			if !ok {
				t.Fatalf("mensagem retornada tem tipo %T, esperava SavedAvailabilityMsg", rawMsg)
			}
			if msg.Err == nil {
				t.Fatal("esperava erro de cancelamento ou timeout na hidratação, obteve nil")
			}

			updated, followUp := m.Update(msg)
			if followUp != nil {
				t.Fatalf("Update gerou comando seguinte inesperado: %v", followUp)
			}
			m = updated.(Model)

			if m.list.err != nil {
				t.Fatalf("erro de hidratação travou a lista com erro: %v", m.list.err)
			}

			items := m.VisibleItems()
			if len(items) == 0 {
				t.Fatal("lista de itens visíveis ficou vazia após timeout da hidratação")
			}

			foundFav := false
			for _, it := range items {
				if it.Channel != nil && it.Channel.Login == "gaules" {
					foundFav = true
					if it.Availability != catalog.AvailabilityUnknown {
						t.Fatalf("disponibilidade do favorito = %v, esperava AvailabilityUnknown", it.Availability)
					}
				}
			}
			if !foundFav {
				t.Fatal("canal favorito gaules sumiu da lista visível após a hidratação")
			}

			// Testa se a navegação por teclado continua funcionando normalmente.
			navUpdated, navCmd := m.Update(tea.KeyMsg{Type: tea.KeyDown})
			if navCmd != nil {
				t.Fatalf("KeyDown gerou comando inesperado: %v", navCmd)
			}
			navModel := navUpdated.(Model)
			if navModel.list.err != nil {
				t.Fatalf("KeyDown gerou erro de lista: %v", navModel.list.err)
			}
		})
	}
}
