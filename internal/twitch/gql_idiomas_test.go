package twitch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
)

// O cache separa idiomas e a consulta sem filtro omite a variável.
func TestIdiomasNaConsultaENoCache(t *testing.T) {
	var got []any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		got = append(got, req.Variables["languages"])
		_, _ = w.Write([]byte(`{"data":{"game":{"id":"1","name":"G","streams":{"edges":[],"pageInfo":{"hasNextPage":false}}}}}`))
	}))
	defer srv.Close()
	src := NewGQLSource(GQLConfig{Endpoint: srv.URL, HTTPClient: srv.Client(), CacheTTL: time.Minute})
	for _, langs := range [][]string{nil, {"pt"}, {"en"}, {"pt"}, {"pt", "en"}, {"en", "pt"}} {
		if _, err := src.Channels(context.Background(), "1", catalog.PageRequest{Languages: langs}); err != nil {
			t.Fatal(err)
		}
	}
	want := []any{nil, []any{"pt"}, []any{"en"}, []any{"pt", "en"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("idiomas nas consultas=%v, quer %v", got, want)
	}
}
