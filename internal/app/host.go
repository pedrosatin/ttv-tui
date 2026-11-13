package app

import (
	"encoding/json"
	"net"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/ui"
)

// videoHostEnv names the socket of the window that hosts this TUI and draws the
// video itself. Unset means an ordinary terminal.
const videoHostEnv = "TTV_TUI_VIDEO_HOST"

// videoHost tells the hosting window which cells belong to the video. SetPanel runs
// inside Update, so it only hands the newest panel to a writer goroutine: a host
// that stops reading costs stale geometry, never a frozen UI.
type videoHost struct {
	conn    net.Conn
	updates chan ui.HostPanel
	// states leva o resumo do estado (SetState), que só os cenários da janela pedem.
	states chan ui.StateReport
	done   chan struct{}
}

// hostStateMsg é como o resumo do estado vai pelo socket: um objeto à parte,
// que a janela distingue de um painel pela chave testState.
type hostStateMsg struct {
	TestState ui.StateReport `json:"testState"`
}

func dialVideoHost(path string) (*videoHost, error) {
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return nil, err
	}
	h := &videoHost{conn: conn, updates: make(chan ui.HostPanel, 1), states: make(chan ui.StateReport, 1), done: make(chan struct{})}
	go h.write()
	return h, nil
}

func (h *videoHost) write() {
	enc := json.NewEncoder(h.conn)
	for {
		select {
		case <-h.done:
			return
		case panel := <-h.updates:
			if err := enc.Encode(panel); err != nil {
				return
			}
		case st := <-h.states:
			if err := enc.Encode(hostStateMsg{TestState: st}); err != nil {
				return
			}
		}
	}
}

// SetPanel replaces any panel the writer has not taken yet.
func (h *videoHost) SetPanel(panel ui.HostPanel) {
	for {
		select {
		case h.updates <- panel:
			return
		default:
		}
		select {
		case <-h.updates:
		default:
		}
	}
}

// SetState é SetPanel para o resumo do estado: só o mais novo vai à janela.
func (h *videoHost) SetState(st ui.StateReport) {
	for {
		select {
		case h.states <- st:
			return
		default:
		}
		select {
		case <-h.states:
		default:
		}
	}
}

// Close ends the writer. The host treats the closed socket as the TUI being gone.
func (h *videoHost) Close() {
	close(h.done)
	_ = h.conn.Close()
}
