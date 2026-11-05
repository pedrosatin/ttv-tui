// Package wm provides optional compositor bridges that place the external
// player window beside the TUI. Missing tools disable the feature without
// breaking the app.
package wm

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Placement is where the player window should sit on the active monitor.
type Placement string

const (
	PlaceRight  Placement = "right"
	PlaceLeft   Placement = "left"
	PlaceCenter Placement = "center"
)

// PlacementForLayout maps TUI layout tokens (split|chat|list|focus) to a
// monitor half / center slot for the mpv window.
func PlacementForLayout(layout string) Placement {
	switch strings.ToLower(strings.TrimSpace(layout)) {
	case "chat":
		return PlaceLeft
	case "focus":
		return PlaceCenter
	default:
		// split and list keep the player on the right so the TUI can sit left.
		return PlaceRight
	}
}

// Hyprland moves/resizes an mpv window through hyprctl using the Lua dispatch
// shape already used by operator scripts (hl.dsp.window.* with address:).
type Hyprland struct {
	bin string
	// lookPath and runCmd are overridable for tests.
	lookPath func(file string) (string, error)
	runCmd   func(name string, args ...string) ([]byte, error)
}

// NewHyprland returns a controller when hyprctl is on PATH. Callers that set
// window_control=hyprland should treat a non-nil error as "feature off".
func NewHyprland() (*Hyprland, error) {
	h := &Hyprland{
		lookPath: exec.LookPath,
		runCmd: func(name string, args ...string) ([]byte, error) {
			return exec.Command(name, args...).CombinedOutput()
		},
	}
	path, err := h.lookPath("hyprctl")
	if err != nil {
		return nil, fmt.Errorf("hyprctl not found in PATH: %w", err)
	}
	h.bin = path
	return h, nil
}

// Apply places the mpv window for the given layout token. No mpv window is a
// silent success so layout changes never fail the TUI.
func (h *Hyprland) Apply(layout string) error {
	if h == nil {
		return nil
	}
	return h.Place(PlacementForLayout(layout))
}

// Place floats, resizes and moves the first mapped mpv window.
func (h *Hyprland) Place(place Placement) error {
	if h == nil {
		return nil
	}
	if h.bin == "" {
		path, err := h.lookPath("hyprctl")
		if err != nil {
			return fmt.Errorf("hyprctl not found in PATH: %w", err)
		}
		h.bin = path
	}

	addr, err := h.findMPVAddress()
	if err != nil {
		return err
	}
	if addr == "" {
		return nil
	}
	mon, err := h.activeMonitor()
	if err != nil {
		return err
	}
	x, y, w, ht := mon.rect(place)
	if err := h.dispatch(fmt.Sprintf(`hl.dsp.window.float({ action = "on", window = "address:%s" })`, addr)); err != nil {
		return err
	}
	if err := h.dispatch(fmt.Sprintf(`hl.dsp.window.resize({ x = %d, y = %d, exact = true, window = "address:%s" })`, w, ht, addr)); err != nil {
		return err
	}
	if err := h.dispatch(fmt.Sprintf(`hl.dsp.window.move({ x = %d, y = %d, relative = false, window = "address:%s" })`, x, y, addr)); err != nil {
		return err
	}
	return nil
}

type hyprClient struct {
	Address      string `json:"address"`
	Class        string `json:"class"`
	InitialClass string `json:"initialClass"`
	Mapped       bool   `json:"mapped"`
	Hidden       bool   `json:"hidden"`
}

type hyprMonitor struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Focused bool   `json:"focused"`
}

func (m hyprMonitor) rect(place Placement) (x, y, w, h int) {
	w = m.Width / 2
	h = m.Height
	y = m.Y
	switch place {
	case PlaceLeft:
		x = m.X
	case PlaceCenter:
		x = m.X + (m.Width-w)/2
	default:
		x = m.X + m.Width - w
	}
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return x, y, w, h
}

func (h *Hyprland) findMPVAddress() (string, error) {
	out, err := h.run("clients", "-j")
	if err != nil {
		return "", err
	}
	var clients []hyprClient
	if err := json.Unmarshal(out, &clients); err != nil {
		return "", fmt.Errorf("parse hyprctl clients: %w", err)
	}
	for _, c := range clients {
		class := strings.ToLower(c.Class)
		init := strings.ToLower(c.InitialClass)
		if class != "mpv" && init != "mpv" {
			continue
		}
		if !c.Mapped || c.Hidden {
			continue
		}
		addr := strings.TrimSpace(c.Address)
		if addr == "" {
			continue
		}
		return addr, nil
	}
	return "", nil
}

func (h *Hyprland) activeMonitor() (hyprMonitor, error) {
	out, err := h.run("monitors", "-j")
	if err != nil {
		return hyprMonitor{}, err
	}
	var monitors []hyprMonitor
	if err := json.Unmarshal(out, &monitors); err != nil {
		return hyprMonitor{}, fmt.Errorf("parse hyprctl monitors: %w", err)
	}
	if len(monitors) == 0 {
		return hyprMonitor{}, fmt.Errorf("hyprctl monitors: empty")
	}
	for _, m := range monitors {
		if m.Focused {
			return m, nil
		}
	}
	return monitors[0], nil
}

func (h *Hyprland) dispatch(expr string) error {
	out, err := h.run("dispatch", expr)
	if err != nil {
		return err
	}
	reply := strings.TrimSpace(string(out))
	if reply != "" && !strings.EqualFold(reply, "ok") {
		return fmt.Errorf("hyprctl dispatch: %s", reply)
	}
	return nil
}

func (h *Hyprland) run(args ...string) ([]byte, error) {
	run := h.runCmd
	if run == nil {
		run = func(name string, a ...string) ([]byte, error) {
			return exec.Command(name, a...).CombinedOutput()
		}
	}
	out, err := run(h.bin, args...)
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("hyprctl %s: %s", strings.Join(args, " "), msg)
	}
	return out, nil
}
