package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/auth"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/ui"
)

func TestStartupTokenNoticeHTTP(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		mode   string
		warn   bool
	}{
		{name: "valid", status: 200, body: `{"login":"viewer"}`},
		{name: "refused", status: 401, body: `{"message":"invalid access token"}`, warn: true},
		{name: "offline", mode: "offline"},
		{name: "bad_gateway", status: 502},
		{name: "rate_limited", status: 429},
		{name: "timeout", mode: "timeout"},
		{name: "malformed_response", status: 200, body: `{`},
		{name: "missing_login", status: 200, body: `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.mode == "timeout" {
					<-r.Context().Done()
					return
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)
			client := srv.Client()
			if tc.mode == "offline" {
				srv.Close()
			}
			if tc.mode == "timeout" {
				client.Timeout = 20 * time.Millisecond
			}
			validate := auth.ValidateClient{URL: srv.URL, HTTPClient: client}
			for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
				t.Run(lang, func(t *testing.T) {
					cat, err := i18n.New(lang)
					if err != nil {
						t.Fatal(err)
					}
					m := ui.New(cat, ui.WithTokenCheck(tokenCheck(validate.Validate, "test-token")))
					next, _ := m.Update(m.Init()())
					got := next.(ui.Model).AuthNotice()
					want := ""
					if tc.warn {
						want = cat.T("auth.token_invalid_notice")
					}
					if got != want {
						t.Fatalf("startup notice = %q, want %q", got, want)
					}
				})
			}
		})
	}
}
