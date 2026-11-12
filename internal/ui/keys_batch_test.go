package ui

import (
	"fmt"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

func batchCategoriesModel(t *testing.T, n int) Model {
	t.Helper()
	catEN, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New(en): %v", err)
	}
	items := make([]catalog.Category, n)
	for i := 0; i < n; i++ {
		items[i] = catalog.Category{
			ID:      fmt.Sprintf("cat-%d", i+1),
			Name:    fmt.Sprintf("Category %d", i+1),
			Viewers: catalog.IntPtr((n - i) * 1000),
		}
	}
	m := New(catEN)
	updated, _ := m.Update(CategoriesLoadedMsg{
		Generation: m.Generation(),
		Page:       catalog.Page[catalog.Category]{Items: items},
	})
	m = updated.(Model)
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	return updated.(Model)
}

func TestExpandBatchedKeyMsg(t *testing.T) {
	t.Parallel()

	multi := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("jjj")}
	got := expandBatchedKeyMsg(multi)
	if len(got) != 3 {
		t.Fatalf("len=%d, want 3", len(got))
	}
	for i, k := range got {
		if k.Type != tea.KeyRunes || len(k.Runes) != 1 || k.Runes[0] != 'j' {
			t.Fatalf("key[%d]=%#v, want single 'j'", i, k)
		}
	}

	paste := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("jjj"), Paste: true}
	got = expandBatchedKeyMsg(paste)
	if len(got) != 1 || !got[0].Paste || string(got[0].Runes) != "jjj" {
		t.Fatalf("paste must stay intact, got %#v", got)
	}

	single := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	got = expandBatchedKeyMsg(single)
	if len(got) != 1 || got[0].String() != "j" {
		t.Fatalf("single rune must stay intact, got %#v", got)
	}

	enter := tea.KeyMsg{Type: tea.KeyEnter}
	got = expandBatchedKeyMsg(enter)
	if len(got) != 1 || got[0].Type != tea.KeyEnter {
		t.Fatalf("control key must stay intact, got %#v", got)
	}
}

func TestBatchedRunesMoveHighlight(t *testing.T) {
	m := batchCategoriesModel(t, 6)
	if m.HighlightedID() != "cat-1" {
		t.Fatalf("start HighlightedID=%q, want cat-1", m.HighlightedID())
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("jjj")})
	m = updated.(Model)
	if m.HighlightedID() != "cat-4" {
		t.Fatalf("after batched jjj, HighlightedID=%q, want cat-4", m.HighlightedID())
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("kkk")})
	m = updated.(Model)
	if m.HighlightedID() != "cat-1" {
		t.Fatalf("after batched kkk, HighlightedID=%q, want cat-1", m.HighlightedID())
	}
}

func TestBatchedMixedRunesAndShortcuts(t *testing.T) {
	// Mixed navigation runes with volume shortcuts in one KeyMsg: j moves,
	// + adjusts volume once, j moves again. Control-typed keys (Enter/Esc/Tab)
	// arrive as their own KeyMsg types; the batch path leaves them untouched.
	catEN, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New(en): %v", err)
	}
	pb := &fakePlayback{}
	m := New(catEN, WithPlayback(pb))
	updated, _ := m.Update(CategoriesLoadedMsg{
		Generation: m.Generation(),
		Page: catalog.Page[catalog.Category]{
			Items: []catalog.Category{
				{ID: "cat-1", Name: "One", Viewers: catalog.IntPtr(3)},
				{ID: "cat-2", Name: "Two", Viewers: catalog.IntPtr(2)},
				{ID: "cat-3", Name: "Three", Viewers: catalog.IntPtr(1)},
			},
		},
	})
	m = updated.(Model)
	updated, _ = m.Update(PlayerStatusMsg{
		Generation: m.Generation(),
		State:      PlayerStatePlaying,
		Login:      "gaules",
		Volume:     50,
	})
	m = updated.(Model)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j', '+', 'j'}})
	m = updated.(Model)
	if m.HighlightedID() != "cat-3" {
		t.Fatalf("after j+j batch, HighlightedID=%q, want cat-3", m.HighlightedID())
	}
	if cmd == nil {
		t.Fatal("expected volume AdjustVolume cmd from '+' in the batch")
	}
	_ = cmd()
	if len(pb.volDeltas) != 1 || pb.volDeltas[0] != 5 {
		t.Fatalf("volDeltas=%v, want [5] (single + in the batch)", pb.volDeltas)
	}

	// Control key in its own message still works after a batched burst.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(Model)
	if m.HighlightedID() != "cat-2" {
		t.Fatalf("after KeyUp, HighlightedID=%q, want cat-2", m.HighlightedID())
	}
}

func TestBatchedRunesComposeAsText(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	m = resize(m, 120, 14)
	m = sendKeys(m, keyTab, keyI)
	if !m.Composing() {
		t.Fatal("expected composing after Tab+i")
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("abc")})
	m = updated.(Model)
	if m.ComposeDraft() != "abc" {
		t.Fatalf("ComposeDraft=%q, want abc", m.ComposeDraft())
	}
	if !m.Composing() {
		t.Fatal("composing left unexpectedly")
	}
}

func TestBatchedHelpToggleIsNotDuplicatedPerMatch(t *testing.T) {
	// A single '?' toggles once. A batched '??' is exactly two keypresses
	// (open then close), not a match on the string "??" and not a third
	// phantom toggle from also handling the original multi-rune message.
	m := batchCategoriesModel(t, 3)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = updated.(Model)
	if !m.HelpVisible() {
		t.Fatal("single '?' should open help once")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = updated.(Model)
	if m.HelpVisible() {
		t.Fatal("second single '?' should close help")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("??")})
	m = updated.(Model)
	if m.HelpVisible() {
		t.Fatal("batched '??' should open then close, leaving help closed")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("??"), Paste: true})
	m = updated.(Model)
	if m.HelpVisible() {
		t.Fatal("paste must not toggle help")
	}
}

func TestBatchedNonShortcutRunesStaySilentOutsideCompose(t *testing.T) {
	m := batchCategoriesModel(t, 3)
	start := m.HighlightedID()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("abc")})
	m = updated.(Model)
	if m.HighlightedID() != start {
		t.Fatalf("highlight moved on non-shortcut batch: %q → %q", start, m.HighlightedID())
	}
	if m.Composing() || m.ComposeDraft() != "" {
		t.Fatalf("non-shortcut batch must not enter compose or write draft: composing=%v draft=%q",
			m.Composing(), m.ComposeDraft())
	}
}

// TestBatchedJJJPTYEvidence prints the ▸ marker before and after a single
// KeyMsg carrying "jjj", so a -v run shows the cursor three rows lower.
func TestBatchedJJJPTYEvidence(t *testing.T) {
	m := batchCategoriesModel(t, 6)

	beforeView := ansi.Strip(m.View())
	beforeMark := markerLine(beforeView)
	if m.HighlightedID() != "cat-1" {
		t.Fatalf("before: HighlightedID=%q marker=%q", m.HighlightedID(), beforeMark)
	}
	t.Logf("BEFORE jjj: %s", beforeMark)
	fmt.Printf("\n--- PTY CAPTURE BEFORE jjj ---\n%s\n--- END BEFORE ---\n", beforeView)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("jjj")})
	m = updated.(Model)

	afterView := ansi.Strip(m.View())
	afterMark := markerLine(afterView)
	if m.HighlightedID() != "cat-4" {
		t.Fatalf("after jjj: HighlightedID=%q marker=%q, want cat-4", m.HighlightedID(), afterMark)
	}
	if !strings.Contains(afterMark, "Category 4") {
		t.Fatalf("after marker should land on Category 4, got %q", afterMark)
	}
	t.Logf("AFTER  jjj: %s", afterMark)
	fmt.Printf("\n--- PTY CAPTURE AFTER jjj ---\n%s\n--- END AFTER ---\n", afterView)
}
