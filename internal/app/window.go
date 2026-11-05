package app

import (
	"github.com/pedrosatin/ttv-tui/internal/ui"
	"github.com/pedrosatin/ttv-tui/internal/wm"
)

// hyprWindowBridge adapts wm.Hyprland to ui.WindowController so the UI package
// never imports the compositor bridge directly.
type hyprWindowBridge struct {
	ctrl *wm.Hyprland
}

func (b hyprWindowBridge) Apply(layout ui.LayoutMode) error {
	if b.ctrl == nil {
		return nil
	}
	return b.ctrl.Apply(string(layout))
}
