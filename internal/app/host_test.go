package app

import (
	"bufio"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/ui"
)

func TestVideoHostDeliversPanels(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	h, err := dialVideoHost(path)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	conn, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	want := ui.VideoPanel{Row: 1, Col: 44, Rows: 48, Cols: 122, Visible: true}
	h.SetPanel(want)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var got ui.VideoPanel
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(line, &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("recebeu %+v, quero %+v", got, want)
	}
}

// Update chama SetPanel: um host que parou de ler não pode travar a interface.
func TestVideoHostSetPanelNeverBlocks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	h, err := dialVideoHost(path)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200000; i++ {
			h.SetPanel(ui.VideoPanel{Cols: i})
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("SetPanel travou com o host sem ler")
	}
}
