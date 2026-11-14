package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

// Controles bidi, o CGJ (U+034F) e as tags U+E0000 a U+E007F saem da tela e da cópia.
func TestChatRemoveInvisiveisDeTerceiros(t *testing.T) {
	controles := []rune{
		'\u2066', '\u2067', '\u2068', '\u2069',
		'\u200e', '\u200f', '\u061c',
		'\u202a', '\u202b', '\u202c', '\u202d', '\u202e',
		'\u034f',
	}
	for r := rune(0xe0000); r <= 0xe007f; r++ {
		controles = append(controles, r)
	}
	for _, r := range controles {
		t.Run(fmt.Sprintf("U+%04X", r), func(t *testing.T) {
			m := scrollFixture(t, i18n.LangPTBR, 0)
			texto := "an" + string(r) + "tes"
			msg := ChatMessage{Kind: chat.MessageChat, DisplayName: texto, Body: texto}
			for _, fallback := range []bool{false, true} {
				copia := msg
				if fallback {
					copia.DisplayName, copia.User = "", texto
				}
				lead, content := m.formatChatMessageParts(copia, false)
				if got := ansi.Strip(lead + content); strings.ContainsRune(got, r) || !strings.Contains(got, "antes: antes") {
					t.Errorf("tela (login=%v) = %q; esperado nome e mensagem sem controle", fallback, got)
				}
				payload, err := decodeOSC52Payload(osc52Sequence(formatChatCopyText(copia)))
				if err != nil {
					t.Fatal(err)
				}
				if payload != "antes: antes" {
					t.Errorf("cópia (login=%v) = %q", fallback, payload)
				}
			}
			if got := sanitizeScreenText(texto); got != "antes" {
				t.Errorf("texto de tela = %q", got)
			}
		})
	}
}

func TestChatPreservaArabeEHebraico(t *testing.T) {
	for _, texto := range []string{"مرحبا بالعالم", "שלום עולם"} {
		t.Run(texto, func(t *testing.T) {
			m := scrollFixture(t, i18n.LangPTBR, 0)
			msg := ChatMessage{Kind: chat.MessageChat, DisplayName: texto, Body: texto}
			lead, content := m.formatChatMessageParts(msg, false)
			if got := ansi.Strip(lead + content); !strings.Contains(got, texto+": "+texto) {
				t.Errorf("tela = %q", got)
			}
			if got := formatChatCopyText(msg); got != texto+": "+texto {
				t.Errorf("cópia = %q", got)
			}
			if got := sanitizeScreenText(texto); got != texto {
				t.Errorf("texto de tela = %q", got)
			}
		})
	}
}
