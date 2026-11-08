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
	updates chan ui.VideoPanel
	done    chan struct{}
}

func dialVideoHost(path string) (*videoHost, error) {
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return nil, err
	}
	h := &videoHost{conn: conn, updates: make(chan ui.VideoPanel, 1), done: make(chan struct{})}
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
		}
	}
}

// SetPanel replaces any panel the writer has not taken yet.
func (h *videoHost) SetPanel(panel ui.VideoPanel) {
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

// Close ends the writer. The host treats the closed socket as the TUI being gone.
func (h *videoHost) Close() {
	close(h.done)
	_ = h.conn.Close()
}
