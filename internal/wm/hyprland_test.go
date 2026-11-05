package wm

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewHyprlandMissingBinary(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir) // empty of hyprctl
	if _, err := NewHyprland(); err == nil {
		t.Fatal("expected error when hyprctl missing from PATH")
	}
}

func TestNewHyprlandFindsBinary(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "hyprctl")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	t.Setenv("PATH", dir)
	h, err := NewHyprland()
	if err != nil {
		t.Fatalf("NewHyprland: %v", err)
	}
	if h.bin != bin {
		t.Fatalf("bin=%q, want %q", h.bin, bin)
	}
}

func TestPlacementForLayout(t *testing.T) {
	cases := map[string]Placement{
		"split":   PlaceRight,
		"list":    PlaceRight,
		"chat":    PlaceLeft,
		"focus":   PlaceCenter,
		"CHAT":    PlaceLeft,
		"unknown": PlaceRight,
	}
	for in, want := range cases {
		if got := PlacementForLayout(in); got != want {
			t.Fatalf("PlacementForLayout(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestHyprlandPlaceDispatchesMoveResizeFloat(t *testing.T) {
	var calls [][]string
	h := &Hyprland{
		bin: "hyprctl",
		lookPath: func(string) (string, error) {
			return "hyprctl", nil
		},
		runCmd: func(name string, args ...string) ([]byte, error) {
			calls = append(calls, append([]string{name}, args...))
			joined := strings.Join(args, " ")
			switch {
			case strings.HasPrefix(joined, "clients"):
				return []byte(`[{"address":"0xabc","class":"mpv","mapped":true,"hidden":false}]`), nil
			case strings.HasPrefix(joined, "monitors"):
				return []byte(`[{"id":0,"name":"eDP-1","width":1920,"height":1080,"x":0,"y":0,"focused":true}]`), nil
			case strings.HasPrefix(joined, "dispatch"):
				return []byte("ok"), nil
			default:
				return nil, errors.New("unexpected " + joined)
			}
		},
	}
	if err := h.Apply("split"); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(calls) < 5 {
		t.Fatalf("expected clients+monitors+3 dispatches, got %d: %v", len(calls), calls)
	}
	var sawFloat, sawResize, sawMove bool
	for _, c := range calls {
		arg := strings.Join(c[1:], " ")
		if strings.Contains(arg, "window.float") && strings.Contains(arg, "address:0xabc") {
			sawFloat = true
		}
		if strings.Contains(arg, "window.resize") && strings.Contains(arg, "x = 960") {
			sawResize = true
		}
		if strings.Contains(arg, "window.move") && strings.Contains(arg, "x = 960") && strings.Contains(arg, "relative = false") {
			sawMove = true
		}
	}
	if !sawFloat || !sawResize || !sawMove {
		t.Fatalf("missing dispatch(es) float=%v resize=%v move=%v calls=%v", sawFloat, sawResize, sawMove, calls)
	}
}

func TestHyprlandPlaceNoMPVIsNoop(t *testing.T) {
	h := &Hyprland{
		bin: "hyprctl",
		runCmd: func(name string, args ...string) ([]byte, error) {
			joined := strings.Join(args, " ")
			if strings.HasPrefix(joined, "clients") {
				return []byte(`[{"address":"0x1","class":"Alacritty","mapped":true,"hidden":false}]`), nil
			}
			if strings.HasPrefix(joined, "monitors") {
				return []byte(`[{"id":0,"width":100,"height":100,"x":0,"y":0,"focused":true}]`), nil
			}
			t.Fatalf("unexpected call %v", args)
			return nil, nil
		},
	}
	if err := h.Place(PlaceRight); err != nil {
		t.Fatalf("Place with no mpv: %v", err)
	}
}

func TestMonitorRect(t *testing.T) {
	m := hyprMonitor{Width: 2000, Height: 1000, X: 100, Y: 50}
	x, y, w, h := m.rect(PlaceLeft)
	if x != 100 || y != 50 || w != 1000 || h != 1000 {
		t.Fatalf("left=%d,%d %dx%d", x, y, w, h)
	}
	x, y, w, h = m.rect(PlaceRight)
	if x != 1100 || w != 1000 {
		t.Fatalf("right=%d,%d %dx%d", x, y, w, h)
	}
	x, y, w, h = m.rect(PlaceCenter)
	if x != 600 || w != 1000 {
		t.Fatalf("center=%d,%d %dx%d", x, y, w, h)
	}
}
