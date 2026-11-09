package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

func assertAllLinesFit(t *testing.T, view string, maxWidth int) {
	t.Helper()
	lines := strings.Split(ansi.Strip(view), "\n")
	for i, line := range lines {
		if w := ansi.StringWidth(line); w > maxWidth {
			t.Fatalf("line %d has width %d > %d:\n%q\nfull view:\n%s", i, w, maxWidth, line, view)
		}
	}
}

func TestListErrorIn80And120Columns(t *testing.T) {
	langs := []string{i18n.LangPTBR, i18n.LangEN}
	widths := []int{80, 120}

	errorCases := []struct {
		name             string
		err              error
		wantAction       map[string]string
		wantDetailPrefix map[string]string
	}{
		{
			name: "network",
			err: &twitch.Error{
				Kind: twitch.Rede,
				Op:   "categories",
				Err:  errors.New("dial tcp: lookup gql.twitch.tv: no such host"),
			},
			wantAction: map[string]string{
				i18n.LangPTBR: "Erro de rede: r para tentar de novo",
				i18n.LangEN:   "Network error: press r to retry",
			},
			wantDetailPrefix: map[string]string{
				i18n.LangPTBR: "detalhe: dial tcp",
				i18n.LangEN:   "detail: dial tcp",
			},
		},
		{
			name: "contract",
			err: &twitch.Error{
				Kind: twitch.Contrato,
				Op:   "categories",
				Err:  errors.New("unexpected EOF in GraphQL JSON response body"),
			},
			wantAction: map[string]string{
				i18n.LangPTBR: "Resposta inesperada da API: r para tentar de novo",
				i18n.LangEN:   "Unexpected API response: press r to retry",
			},
			wantDetailPrefix: map[string]string{
				i18n.LangPTBR: "detalhe: unexpec",
				i18n.LangEN:   "detail: unexpect",
			},
		},
	}

	for _, lang := range langs {
		cat, err := i18n.New(lang)
		if err != nil {
			t.Fatalf("i18n.New(%s): %v", lang, err)
		}
		for _, tc := range errorCases {
			for _, width := range widths {
				name := fmt.Sprintf("%s/%s/%d_cols", lang, tc.name, width)
				t.Run(name, func(t *testing.T) {
					m, _, _ := setupTwoPanelModel(t)
					m.catalog = cat
					m = resize(m, width, 20)
					m.list.err = tc.err

					v := m.View()
					plain := ansi.Strip(v)
					assertAllLinesFit(t, v, width)

					action := tc.wantAction[lang]
					bar := ansi.Strip(m.StatusBar())
					if !strings.Contains(bar, action) {
						t.Fatalf("status bar at %d cols lacks full action %q:\n%q", width, action, bar)
					}

					// Verify detail behavior:
					// At 80 cols: action must never be cut with ellipsis; detail is dropped or truncated first.
					// At 120 cols: action is whole, and detail is visible and truncated first with ellipsis.
					if strings.Contains(bar, action+"…") {
						t.Fatalf("action itself was truncated with ellipsis in status bar:\n%q", bar)
					}

					if width == 120 {
						prefix := tc.wantDetailPrefix[lang]
						if !strings.Contains(bar, prefix) {
							t.Fatalf("status bar at 120 cols should include detail prefix %q:\n%q", prefix, bar)
						}
					}

					// In list body, both error message and retry prompt appear without truncation
					if !strings.Contains(plain, cat.T("error.action_retry")) &&
						!strings.Contains(plain, "Pressione r para tentar novamente") &&
						!strings.Contains(plain, "Press r to retry") {
						t.Fatalf("list body missing actionable retry prompt:\n%s", plain)
					}
				})
			}
		}
	}
}

func TestChatHeaderStateIn80Columns(t *testing.T) {
	langs := []string{i18n.LangPTBR, i18n.LangEN}
	states := []struct {
		state     ChatState
		wantState map[string]string
	}{
		{
			state: ChatStateConnecting,
			wantState: map[string]string{
				i18n.LangPTBR: "Conectando...",
				i18n.LangEN:   "Connecting...",
			},
		},
		{
			state: ChatStateConnected,
			wantState: map[string]string{
				i18n.LangPTBR: "Conectado",
				i18n.LangEN:   "Connected",
			},
		},
		{
			state: ChatStateReconnecting,
			wantState: map[string]string{
				i18n.LangPTBR: "Reconectando...",
				i18n.LangEN:   "Reconnecting...",
			},
		},
	}

	for _, lang := range langs {
		cat, err := i18n.New(lang)
		if err != nil {
			t.Fatalf("i18n.New(%s): %v", lang, err)
		}
		for _, st := range states {
			for _, focus := range []Focus{FocusList, FocusChat} {
				name := fmt.Sprintf("%s/%v/focus_%s", lang, st.state, focus)
				t.Run(name, func(t *testing.T) {
					m, _, _ := setupTwoPanelModel(t)
					m.catalog = cat
					m.layout.focus = focus
					m.chat.panel.channel = "gaules"
					m.chat.panel.state = st.state
					m = resize(m, 80, 20)

					v := m.View()
					plain := ansi.Strip(v)
					assertAllLinesFit(t, v, 80)

					col := chatColumn(t, m)
					headerText := col[0]
					if len(col) > 1 && strings.HasPrefix(col[1], "  ") && !strings.Contains(col[1], ":") {
						headerText += " " + col[1]
					}

					if !strings.Contains(headerText, "gaules") {
						t.Fatalf("chat header at 80 cols lacks channel name 'gaules':\n%q", headerText)
					}
					wantStateText := st.wantState[lang]
					if !strings.Contains(headerText, wantStateText) {
						t.Fatalf("chat header at 80 cols lacks state text %q:\n%q\nfull chat:\n%s", wantStateText, headerText, strings.Join(col, "\n"))
					}

					// State must not be cut with ellipsis in 80 columns
					cutState := strings.TrimSuffix(wantStateText, "...") + "…"
					if strings.Contains(headerText, cutState) {
						t.Fatalf("chat header state truncated with ellipsis: %q", headerText)
					}

					_ = plain
				})
			}
		}
	}
}

func TestSendNoticeIn80Columns(t *testing.T) {
	langs := []string{i18n.LangPTBR, i18n.LangEN}

	for _, lang := range langs {
		cat, err := i18n.New(lang)
		if err != nil {
			t.Fatalf("i18n.New(%s): %v", lang, err)
		}

		t.Run(lang+"/sending_message", func(t *testing.T) {
			m, _, _ := setupTwoPanelModel(t)
			m.catalog = cat
			m = resize(m, 80, 20)
			m = withChatMessages(t, m, ChatMessage{
				Kind:        chat.MessageChat,
				ID:          "send-1",
				User:        "pedrosatin",
				DisplayName: "pedrosatin",
				Body:        "teste de envio",
				Delivery:    chat.DeliverySending,
			})

			v := m.View()
			assertAllLinesFit(t, v, 80)

			wantBadge := cat.T("chat.send.sending")
			if !strings.Contains(v, wantBadge) {
				t.Fatalf("view at 80 cols missing sending badge %q:\n%s", wantBadge, v)
			}
		})

		t.Run(lang+"/rejected_message_with_reason", func(t *testing.T) {
			reasons := []struct {
				reason chat.RejectReason
				name   string
			}{
				{chat.RejectSlowMode, "slow_mode"},
				{chat.RejectFollowersOnly, "followers_only"},
				{chat.RejectSubsOnly, "subs_only"},
				{chat.RejectRateLimited, "rate_limit"},
			}
			for _, r := range reasons {
				m, _, _ := setupTwoPanelModel(t)
				m.catalog = cat
				m = resize(m, 80, 20)
				m = withChatMessages(t, m, ChatMessage{
					Kind:         chat.MessageChat,
					ID:           "rej-1",
					User:         "pedrosatin",
					DisplayName:  "pedrosatin",
					Body:         "mensagem recusada",
					Delivery:     chat.DeliveryRejected,
					RejectReason: r.reason,
				})

				v := m.View()
				assertAllLinesFit(t, v, 80)

				reasonText := m.formatRejectReason(r.reason, "")
				wantBadge := fmt.Sprintf(cat.T("chat.send.rejected"), reasonText)
				col := chatColumn(t, m)
				fullCol := strings.Join(col, "\n")
				// The whole badge appears without ellipsis truncation
				if !strings.Contains(squash(fullCol), squash(wantBadge)) {
					t.Fatalf("reason %s: view at 80 cols missing rejected badge %q:\n%s", r.name, wantBadge, fullCol)
				}
			}
		})

		t.Run(lang+"/footer_send_warning_wrapped", func(t *testing.T) {
			warnings := []string{
				cat.T("chat.warn.rate_limit"),
				cat.T("chat.warn.channel_switched"),
				cat.T("chat.warn.no_token"),
			}
			for _, w := range warnings {
				m, _, _ := setupTwoPanelModel(t)
				m.catalog = cat
				m = resize(m, 80, 20)
				m.chat.panel.sendWarning = w

				v := m.View()
				assertAllLinesFit(t, v, 80)

				col := chatColumn(t, m)
				fullCol := strings.Join(col, "\n")

				// The entire warning must be visible without being truncated by ellipsis
				if !strings.Contains(squash(fullCol), squash(w)) {
					t.Fatalf("send warning %q was cut in 80 columns:\n%s", w, fullCol)
				}

				// The input line must still be the last row of the panel (pinned footer contract)
				lastRow := col[len(col)-1]
				if !strings.HasPrefix(lastRow, "  > ") {
					t.Fatalf("last row of chat panel is not input line: %q", lastRow)
				}
			}
		})
	}
}

func ruler(w int) string {
	var b strings.Builder
	for i := 1; i <= w; i++ {
		if i%10 == 0 {
			fmt.Fprintf(&b, "%d", (i/10)%10)
		} else {
			b.WriteString("-")
		}
	}
	return b.String()
}

func printScreen(t *testing.T, title, view string, width int) {
	t.Logf("\n================================================================================\nCAPTURA PTY 80 COLUNAS: %s\nRégua (80 colunas):\n%s\n--------------------------------------------------------------------------------", title, ruler(width))
	lines := strings.Split(ansi.Strip(view), "\n")
	for idx, line := range lines {
		w := ansi.StringWidth(line)
		t.Logf("%2d | %s | (%d cols)", idx+1, line, w)
	}
	t.Logf("================================================================================\n")
}

func TestCapturePTY80Columns(t *testing.T) {
	catPT, err := i18n.New(i18n.LangPTBR)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}

	// CASO 1: Erro de rede da lista em 80 colunas
	t.Run("Caso_1_Erro_Rede_80_Colunas", func(t *testing.T) {
		m, _, _ := setupTwoPanelModel(t)
		m.catalog = catPT
		m = resize(m, 80, 10)
		m.list.err = &twitch.Error{
			Kind: twitch.Rede,
			Op:   "categories",
			Err:  errors.New("dial tcp: lookup gql.twitch.tv: no such host"),
		}
		printScreen(t, "Caso 1: Erro da lista (Rede) em 80 colunas", m.View(), 80)
	})

	// CASO 1b: Erro de contrato da lista em 80 colunas
	t.Run("Caso_1b_Erro_Contrato_80_Colunas", func(t *testing.T) {
		m, _, _ := setupTwoPanelModel(t)
		m.catalog = catPT
		m = resize(m, 80, 10)
		m.list.err = &twitch.Error{
			Kind: twitch.Contrato,
			Op:   "categories",
			Err:  errors.New("unexpected EOF in GraphQL JSON response body"),
		}
		printScreen(t, "Caso 1b: Erro da lista (Contrato) em 80 colunas", m.View(), 80)
	})

	// CASO 2: Cabeçalho do chat em 80 colunas (conectando, conectado, reconectando)
	for _, st := range []struct {
		name  string
		state ChatState
	}{
		{"Conectando", ChatStateConnecting},
		{"Conectado", ChatStateConnected},
		{"Reconectando", ChatStateReconnecting},
	} {
		t.Run("Caso_2_Cabecalho_"+st.name+"_80_Colunas", func(t *testing.T) {
			m, _, _ := setupTwoPanelModel(t)
			m.catalog = catPT
			m.chat.panel.channel = "gaules"
			m.chat.panel.state = st.state
			m = resize(m, 80, 10)
			printScreen(t, "Caso 2: Cabeçalho do chat ("+st.name+") em 80 colunas", m.View(), 80)
		})
	}

	// CASO 3a: Aviso de envio (enviando e recusado com razão) em 80 colunas
	t.Run("Caso_3a_Aviso_Envio_Mensagem_80_Colunas", func(t *testing.T) {
		m, _, _ := setupTwoPanelModel(t)
		m.catalog = catPT
		m = resize(m, 80, 12)
		m = withChatMessages(t, m,
			ChatMessage{
				Generation:  1,
				Kind:        chat.MessageChat,
				ID:          "m1",
				User:        "pedrosatin",
				DisplayName: "pedrosatin",
				Body:        "mensagem para a tribo",
				Delivery:    chat.DeliverySending,
			},
			ChatMessage{
				Generation:   1,
				Kind:         chat.MessageChat,
				ID:           "m2",
				User:         "pedrosatin",
				DisplayName:  "pedrosatin",
				Body:         "segunda mensagem rápida",
				Delivery:     chat.DeliveryRejected,
				RejectReason: chat.RejectSlowMode,
			},
		)
		printScreen(t, "Caso 3a: Aviso de envio no chat (enviando e recusado com razão) em 80 colunas", m.View(), 80)
	})

	// CASO 3b: Aviso de envio no rodapé (limite de envio) em 80 colunas
	t.Run("Caso_3b_Aviso_Envio_Rodape_80_Colunas", func(t *testing.T) {
		m, _, _ := setupTwoPanelModel(t)
		m.catalog = catPT
		m = resize(m, 80, 12)
		m.chat.panel.sendWarning = catPT.T("chat.warn.rate_limit")
		printScreen(t, "Caso 3b: Aviso de envio no rodapé (limite de envio atingido) em 80 colunas", m.View(), 80)
	})
}
