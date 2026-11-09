package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func withExactCellWidths(t *testing.T) {
	t.Helper()
	exactCellWidths = true
	t.Cleanup(func() { exactCellWidths = false })
}

// Um emoji em qualquer coluna não pode custar o último caractere da linha inteira.
func TestExactWidthsKeepTheRowWhole(t *testing.T) {
	withExactCellWidths(t)
	row := "🙂 lista" + strings.Repeat("x", 14)
	if got := ansi.StringWidth(row); got != 22 {
		t.Fatalf("largura de teste %d", got)
	}
	if got := fitScreenLines(row, 22); got != row {
		t.Fatalf("cortou a linha: %q", got)
	}
}

func TestExactWidthsUseOneEllipsis(t *testing.T) {
	withExactCellWidths(t)
	got := fitListRow("🙂 um titulo bem comprido para caber aqui", 16)
	if strings.Contains(got, "...") || !strings.HasSuffix(got, "…") {
		t.Fatalf("corte: %q", got)
	}
	if again := padOrTruncateVisual(got, 16); strings.Contains(again, ".…") {
		t.Fatalf("corte duplo: %q", again)
	}
}

func TestExactWidthsWrapToTheWholeColumn(t *testing.T) {
	withExactCellWidths(t)
	rows := wrapChatRow("a: ", "🙂 "+strings.Repeat("palavra ", 12), 24)
	for _, row := range rows {
		if strings.Contains(row, "…") {
			t.Fatalf("quebra não pode cortar com reticências: %q", row)
		}
	}
	if len(rows) < 2 {
		t.Fatalf("esperava quebra, veio %q", rows)
	}
}

func TestChatNeverOpensOnTheTailOfAMessage(t *testing.T) {
	rows := []string{"a: 1", "   2", "   3", "b: 1", "   2", "c: 1"}
	starts := []bool{true, false, false, true, false, true}
	got := trimToMessageStart(rows, starts, 4)
	want := []string{"", "b: 1", "   2", "c: 1"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("veio %q, quero %q", got, want)
	}
}

func TestChatShowsTheEndOfAMessageTallerThanThePanel(t *testing.T) {
	rows := []string{"a: 1", "   2", "   3", "   4"}
	starts := []bool{true, false, false, false}
	got := trimToMessageStart(rows, starts, 2)
	if strings.Join(got, "|") != "   3|   4" {
		t.Fatalf("veio %q", got)
	}
}

func TestHostedNarrowWindowWarnsOnOneLine(t *testing.T) {
	m := hostedModel(t, 70, 49, true)
	if got := m.focusStatusText(); got != m.t("status.chat_narrow") {
		t.Fatalf("janela estreita: %q", got)
	}
	if strings.Contains(m.frame(), m.t("chat.hidden_narrow")) {
		t.Fatal("o aviso longo voltou para a coluna da lista")
	}
}

func TestHostedVideoOnlySaysHowToComeBack(t *testing.T) {
	m := hostedModel(t, 212, 49, true)
	m.layout.showList, m.layout.showChat = false, false
	if got := m.focusStatusText(); got != m.t("status.video_only") {
		t.Fatalf("só o vídeo: %q", got)
	}
}

func TestHostedNarrowHintSurvivesATightBar(t *testing.T) {
	m := hostedModel(t, 70, 49, true)
	m.player.state = PlayerStatePlaying
	m.player.login = "um_canal_com_nome_bem_comprido"
	if bar := ansi.Strip(m.StatusBar()); !strings.Contains(bar, m.t("status.chat_narrow")) {
		t.Fatalf("a dica sumiu da barra: %q", bar)
	}
}
