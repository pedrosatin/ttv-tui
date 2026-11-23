package ui

import (
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

// Cada nome sai de volta igual em tea.KeyMsg.String(): o nome que se escreve é
// o que o bubbletea mostra.
func TestParseKeyNameVoltaAoNomeDoBubbletea(t *testing.T) {
	for _, name := range []string{
		"enter", "esc", "tab", "shift+tab", "backspace", "ctrl+f", "ctrl+q", "ctrl+@",
		"up", "down", "left", "right", "alt+up", "alt+down", "ctrl+left",
		"pgup", "pgdown", "home", "end", "f1", "delete",
		"g", "/", "?", "+", "alt+j", "ç",
	} {
		k, ok := parseKeyName(name)
		if !ok {
			t.Fatalf("parseKeyName(%q) recusou o nome", name)
		}
		if got := k.String(); got != name {
			t.Errorf("parseKeyName(%q).String() = %q", name, got)
		}
	}
}

// O espaço tem nome próprio, porque " " não cabe na notação, e chega com a runa,
// como o terminal entrega.
func TestParseKeyNameEspaco(t *testing.T) {
	for _, tc := range []struct {
		name string
		alt  bool
	}{{"space", false}, {"alt+space", true}} {
		k, ok := parseKeyName(tc.name)
		if !ok {
			t.Fatalf("parseKeyName(%q) recusou o nome", tc.name)
		}
		if k.Type != tea.KeySpace || string(k.Runes) != " " || k.Alt != tc.alt {
			t.Errorf("parseKeyName(%q) = %+v, quer espaço com alt=%v", tc.name, k, tc.alt)
		}
	}
}

func TestParseKeyNameRecusaNomeDesconhecido(t *testing.T) {
	for _, name := range []string{
		"", "ctrl+ç", "alt+", "pgbaixo", "enterr",
		// Uma sequência precisa de uma tecla de verdade depois do prefixo.
		"ctrl+w ", "ctrl+w invalida", "ctrl+w h l", "g ", "g invalida", "g ctrl+ç",
	} {
		if k, ok := parseKeyName(name); ok {
			t.Errorf("parseKeyName(%q) = %q, quer erro", name, k.String())
		}
	}
}

func TestParseKeysMisturaTeclasETextoSolto(t *testing.T) {
	keys, err := parseKeys(`ctrl+f g aules enter "enter" space ?`)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, k := range keys {
		got = append(got, k.String())
	}
	want := []string{"ctrl+f", "g", "a", "u", "l", "e", "s", "enter", "e", "n", "t", "e", "r", " ", "?"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseKeys = %q, quer %q", got, want)
	}
}

func TestParseKeysRecusaTeclaComModificadorErrado(t *testing.T) {
	if _, err := parseKeys("ctrl+ç"); err == nil || !strings.Contains(err.Error(), "ctrl+ç") {
		t.Fatalf("parseKeys(ctrl+ç) = %v, quer erro com o nome", err)
	}
}

// Um temporizador de uma hora não segura o cenário, e os lotes rodam em ordem.
func TestSettleNaoEsperaTemporizadorERodaLotesEmOrdem(t *testing.T) {
	m := newClassicModel(mustCatalog(t, i18n.LangPTBR))
	m.timer = scenarioTimer
	fired := false
	hour := m.after(time.Hour, func(time.Time) tea.Msg { fired = true; return nil })
	size := func(w int) tea.Cmd {
		return func() tea.Msg { return tea.WindowSizeMsg{Width: w, Height: 30} }
	}

	// Se settle esperasse o relógio, runCmd reprovaria o teste em cmdDeadline.
	m = settle(t, m, tea.Batch(hour, tea.Sequence(size(90), size(100))))
	if fired {
		t.Fatal("o temporizador disparou; num cenário ele fica sem disparar")
	}
	if m.width != 100 {
		t.Fatalf("largura = %d, quer 100: a sequência rodou fora de ordem", m.width)
	}
}

func TestScreenDiffMostraSoAsLinhasQueMudaram(t *testing.T) {
	diff := screenDiff("a\nb\nc\n", "a\nB\nc\nd\n")
	for _, want := range []string{"linha  2", "golden: |b|", "tela:   |B|", "linha  4", "golden: (sem a linha)", "tela:   |d|"} {
		if !strings.Contains(diff, want) {
			t.Errorf("diff sem %q:\n%s", want, diff)
		}
	}
	if strings.Contains(diff, "linha  1") || strings.Contains(diff, "linha  3") {
		t.Errorf("diff mostra linha igual:\n%s", diff)
	}
}

// Uma sequência do mapa padrão é o prefixo e uma tecla, com o nome que
// handlePrefixKey monta ao receber as duas. O espaço fica como "space".
func TestParseKeyNameSequencia(t *testing.T) {
	for name, want := range map[string]string{
		"ctrl+w h": "ctrl+w h", "ctrl+w +": "ctrl+w +", "ctrl+w <": "ctrl+w <",
		"g c": "g c", "ctrl+w space": "ctrl+w space", "ctrl+w esc": "ctrl+w esc",
	} {
		k, ok := parseKeyName(name)
		if !ok {
			t.Fatalf("parseKeyName(%q) recusou a sequência", name)
		}
		if got := keyName(k); got != want {
			t.Errorf("parseKeyName(%q) = %q, quer %q", name, got, want)
		}
	}
}
