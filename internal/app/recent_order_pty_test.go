//go:build unix

package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/store"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

// recentOrderSource backs the ticket 08 PTY: a category plus one live channel
// so refresh has something to reload while recents dominate the list order.
type recentOrderSource struct{}

func (recentOrderSource) Categories(context.Context, twitch.PageRequest) (twitch.Page[twitch.Category], error) {
	return twitch.Page[twitch.Category]{Items: []twitch.Category{{ID: "cat-order", Name: "OrderCat"}}}, nil
}

func (recentOrderSource) Channels(context.Context, string, twitch.PageRequest) (twitch.Page[twitch.Channel], error) {
	return twitch.Page[twitch.Channel]{Items: []twitch.Channel{
		{ID: "ch-live", Login: "livechan", DisplayName: "LiveChan", Viewers: twitch.IntPtr(42)},
	}}, nil
}

func (recentOrderSource) Search(context.Context, string) (twitch.Page[twitch.Channel], error) {
	return twitch.Page[twitch.Channel]{Items: []twitch.Channel{
		{ID: "ch-live", Login: "livechan", DisplayName: "LiveChan"},
	}}, nil
}

func (recentOrderSource) SearchCategories(context.Context, string) ([]twitch.Category, error) {
	return nil, nil
}

func (recentOrderSource) ChannelsByLogins(context.Context, []string) ([]twitch.Channel, error) {
	return nil, nil
}

func seedRecentOrderState(statePath string) error {
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		return err
	}
	st := store.State{
		Favorites: []store.Channel{{ID: "fav-1", Login: "favorite_one", DisplayName: "Favorite One"}},
		Recents: []store.Channel{
			{ID: "rec-a", Login: "alice", DisplayName: "Alice"},
			{ID: "rec-b", Login: "bob", DisplayName: "Bob"},
			{ID: "rec-c", Login: "carol", DisplayName: "Carol"},
		},
	}
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return os.WriteFile(statePath, data, 0o600)
}

func recentNamesOrder(screen string) []string {
	names := []string{"Alice", "Bob", "Carol"}
	type hit struct {
		name string
		idx  int
	}
	var hits []hit
	for _, n := range names {
		i := strings.Index(screen, n)
		if i >= 0 {
			hits = append(hits, hit{n, i})
		}
	}
	for i := 0; i < len(hits); i++ {
		for j := i + 1; j < len(hits); j++ {
			if hits[j].idx < hits[i].idx {
				hits[i], hits[j] = hits[j], hits[i]
			}
		}
	}
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.name
	}
	return out
}

// Bubble Tea starts each alternate-screen flush at home, skips unchanged rows
// with newlines, and ends with an absolute move to the last row. Apply complete
// flushes only: screenSince is a write history, not the current terminal screen.
var recentOrderFlush = regexp.MustCompile(`(?s)\x1b\[H(.*?)\x1b\[([0-9]+);H`)

func recentOrderScreen(output string) string {
	var rows []string
	for _, frame := range recentOrderFlush.FindAllStringSubmatch(output, -1) {
		height, _ := strconv.Atoi(frame[2])
		for len(rows) < height {
			rows = append(rows, "")
		}
		rows = rows[:height]
		for i, line := range strings.Split(frame[1], "\n") {
			if i >= height {
				break
			}
			if line != "" {
				rows[i] = strings.Trim(ansiSeq.ReplaceAllString(line, ""), "\r")
			}
		}
	}
	return strings.Join(rows, "\n")
}

func waitRecentOrderScreen(p *ptyTUI, ready func(string) bool) string {
	p.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		screen := recentOrderScreen(p.output())
		if ready(screen) {
			return screen
		}
		if time.Now().After(deadline) {
			p.t.Fatalf("recent list did not converge:\n%s", screen)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRecentOrderScreenAppliesCompleteRedraws(t *testing.T) {
	// The last navigation redraw may straddle mark(): the output after that
	// offset starts with Carol even though Carol remains on the third row.
	initial := "\x1b[H\rAlice\r\nBob\r\n▸ Carol\x1b[3;H"
	partialNavigation := "\x1b[H\n\n▸ "
	openFrame := "\x1b[H\rAlice | Chat — #carol\r\nBob\r\n▸ Carol\x1b[3;H"
	historySinceMark := "Carol\x1b[3;H" + openFrame
	if got := recentNamesOrder(ansiSeq.ReplaceAllString(historySinceMark, "")); !stringSlicesEqual(got, []string{"Carol", "Alice", "Bob"}) {
		t.Fatalf("fixture must reproduce the old write-history misreading, got %v", got)
	}
	output := initial + partialNavigation
	if got := recentNamesOrder(recentOrderScreen(output)); !stringSlicesEqual(got, []string{"Alice", "Bob", "Carol"}) {
		t.Fatalf("incomplete redraw changed screen: %v", got)
	}
	output += historySinceMark
	if got := recentNamesOrder(recentOrderScreen(output)); !stringSlicesEqual(got, []string{"Alice", "Bob", "Carol"}) {
		t.Fatalf("navigation and open changed screen order: %v", got)
	}
	// A sparse flush changes Carol's row only; unchanged rows must survive.
	output += "\x1b[H\n\n▸ Carol (live)\x1b[3;H"
	if screen := recentOrderScreen(output); !strings.Contains(screen, "Carol (live)") || !stringSlicesEqual(recentNamesOrder(screen), []string{"Alice", "Bob", "Carol"}) {
		t.Fatalf("sparse redraw lost rows:\n%s", screen)
	}
	// An actual reorder must still be visible to the assertion.
	output += "\x1b[H\r▸ Carol\r\nAlice\r\nBob\x1b[3;H"
	if got := recentNamesOrder(recentOrderScreen(output)); !stringSlicesEqual(got, []string{"Carol", "Alice", "Bob"}) {
		t.Fatalf("refresh reorder was hidden: %v", got)
	}
}

// TestOpenChannelDoesNotReorderRecents_PTY opens Carol (third recent) and
// checks the on-screen recent order stays Alice → Bob → Carol; after refresh
// it becomes Carol → Alice → Bob. Opening records the newest recent on disk
// immediately; only a list reload applies that order to the visible rows.
func TestOpenChannelDoesNotReorderRecents_PTY(t *testing.T) {
	p := startTUIOnPTY(t, "TTV_APP_RECENT_ORDER=1", "LANG=en_US.UTF-8", "LC_ALL=", "LC_MESSAGES=")

	initialScreen := waitRecentOrderScreen(p, func(screen string) bool {
		return strings.Contains(screen, "Alice") && strings.Contains(screen, "Carol") && strings.Contains(screen, "OrderCat")
	})
	initial := recentNamesOrder(initialScreen)
	if want := []string{"Alice", "Bob", "Carol"}; !stringSlicesEqual(initial, want) {
		t.Fatalf("initial recent order = %v, want %v\n%s", initial, want, initialScreen)
	}

	// A generic highlight can belong to an earlier redraw. Wait for each target
	// row before sending the next key, including the final Carol selection.
	for _, name := range []string{"Alice", "Bob", "Carol"} {
		p.send([]byte("j"))
		waitRecentOrderScreen(p, func(screen string) bool {
			for _, line := range strings.Split(screen, "\n") {
				if strings.Contains(line, "▸") && strings.Contains(line, name) {
					return true
				}
			}
			return false
		})
	}
	oldPid := p.playerPid
	p.send([]byte("\r"))
	p.followPlayer(oldPid)

	// Save runs asynchronously, independently of chat/player startup. Observe
	// its actual effect before asserting that persistence left the UI frozen.
	statePath := filepath.Join(p.dir, "state", "state.json")
	deadline := time.Now().Add(10 * time.Second)
	for {
		st, err := store.LoadFrom(statePath)
		if err != nil {
			t.Fatal(err)
		}
		if len(st.Recents) > 0 && st.Recents[0].Login == "carol" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Carol was not saved as newest recent: %+v", st.Recents)
		}
		time.Sleep(20 * time.Millisecond)
	}
	afterOpen := waitRecentOrderScreen(p, func(screen string) bool {
		return strings.Contains(screen, "Chat — #carol")
	})
	openedOrder := recentNamesOrder(afterOpen)
	if want := []string{"Alice", "Bob", "Carol"}; !stringSlicesEqual(openedOrder, want) {
		t.Fatalf("after opening Carol, recent order = %v, want frozen %v\n%s", openedOrder, want, afterOpen)
	}
	t.Logf("PTY after open (no reorder):\n%s", afterOpen)

	p.send([]byte("r"))
	afterRefresh := waitRecentOrderScreen(p, func(screen string) bool {
		return stringSlicesEqual(recentNamesOrder(screen), []string{"Carol", "Alice", "Bob"})
	})
	t.Logf("PTY after refresh (new order):\n%s", afterRefresh)
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
