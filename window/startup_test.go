package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

type metricFace struct {
	font.Face
	width, height int
	missing       bool
}

func (f metricFace) Metrics() font.Metrics                     { return font.Metrics{Ascent: fixed.I(f.height)} }
func (f metricFace) GlyphAdvance(r rune) (fixed.Int26_6, bool) { return fixed.I(f.width), !f.missing }

func TestCellMetricsRejectInvalid(t *testing.T) {
	for _, tc := range []struct {
		name string
		face metricFace
	}{
		{"width", metricFace{width: 0, height: 16}},
		{"height", metricFace{width: 8, height: 0}},
		{"missing M", metricFace{width: 8, height: 16, missing: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, _, err := cellMetrics(tc.face); err == nil {
				t.Fatal("invalid cell accepted without opening error")
			}
		})
	}
	if w, h, _, err := cellMetrics(metricFace{width: 8, height: 16}); err != nil || w != 8 || h != 16 {
		t.Fatalf("valid metrics: %d %d %v", w, h, err)
	}
}

func TestStartupProbe(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	bin := filepath.Join(dir, "ttv-tui-window")
	if out, err := exec.Command("go", "build", "-buildvcs=false", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	t.Setenv("HOME", dir)
	src := filepath.Join(dir, "sdl.c")
	// SDL doubles keep these subprocesses independent of the display and GPU.
	code := `#include <SDL3/SDL.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
bool SDL_Init(SDL_InitFlags flags) { fputs("SDL init\n",stderr); return true; }
SDL_Window *SDL_CreateWindow(const char *title,int w,int h,SDL_WindowFlags flags) {
 return strcmp(getenv("PROBE_FAIL"),"window") ? (SDL_Window *)1 : NULL;
}
SDL_GLContext SDL_GL_CreateContext(SDL_Window *win) { return strcmp(getenv("PROBE_FAIL"),"ok") ? NULL : (SDL_GLContext)1; }
bool SDL_GL_DestroyContext(SDL_GLContext ctx) { return true; }
float SDL_GetWindowDisplayScale(SDL_Window *win) { return 1; }
bool SDL_GL_SetSwapInterval(int interval) { return true; }
bool SDL_StartTextInput(SDL_Window *win) { return true; }
const char *SDL_GetError(void) { return "injected SDL failure"; }
void SDL_Quit(void) {}
void SDL_DestroyWindow(SDL_Window *win) {}
`
	if err := os.WriteFile(src, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	flags, err := exec.Command("pkg-config", "--cflags", "sdl3").Output()
	if err != nil {
		t.Fatal(err)
	}
	lib := filepath.Join(dir, "sdl.so")
	args := append(strings.Fields(string(flags)), "-shared", "-fPIC", src, "-o", lib)
	if out, err := exec.Command("cc", args...).CombinedOutput(); err != nil {
		t.Fatalf("cc: %v %s", err, out)
	}
	shortTmp, err := os.MkdirTemp("/tmp", "ttv-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(shortTmp) })
	for _, stage := range []string{"window", "context", "socket", "ok"} {
		t.Run(stage, func(t *testing.T) {
			tmp := shortTmp
			if stage == "socket" {
				tmp = filepath.Join(dir, strings.Repeat("x", 100))
				if err := os.Mkdir(tmp, 0700); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command(bin, "--probe")
			cmd.Env = append(os.Environ(), "LD_PRELOAD="+lib, "PROBE_FAIL="+stage, "TMPDIR="+tmp)
			out, err := cmd.CombinedOutput()
			if stage == "ok" {
				if err != nil {
					t.Fatalf("valid probe: %v %s", err, out)
				}
				return
			}
			if err == nil {
				t.Fatalf("probe approved without usable %s; output: %s", stage, out)
			}
			if stage == "socket" {
				if !strings.Contains(string(out), "socket path") || strings.Contains(string(out), "SDL init") {
					t.Fatalf("socket rejected too late: %s", out)
				}
			} else if !strings.Contains(string(out), "injected SDL failure") {
				t.Fatalf("missing SDL error: %s", out)
			}
		})
	}
}
