// ttv-tui-window hosts the TUI (PTY + terminal emulator) and the video (libmpv) in
// one window. A shim named "mpv" on the child's PATH hands streamlink's stdin and
// the IPC path to the embedded core, so the TUI drives the player as it always did.
package main

/*
#cgo pkg-config: sdl3 mpv gl
#include "bridge.h"
*/
import "C"

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

func init() { runtime.LockOSThread() }

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func main() {
	if filepath.Base(os.Args[0]) == "mpv" {
		shimMain()
		return
	}
	tui := os.Getenv("TTV_WINDOW_TUI")
	if tui == "" {
		tui = filepath.Join(os.Getenv("HOME"), ".local/bin/ttv-tui")
		if exe, err := os.Executable(); err == nil {
			if sib := filepath.Join(filepath.Dir(exe), "ttv-tui"); fileExists(sib) {
				tui = sib
			}
		}
	}
	loadTheme()
	app_id := os.Getenv("TTV_WINDOW_APPID")
	if app_id == "" {
		app_id = "ttv-tui"
	}

	dir, err := os.MkdirTemp("", "ttv-shell-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	binDir := filepath.Join(dir, "bin")
	_ = os.Mkdir(binDir, 0o755)
	exe, _ := os.Executable()
	_ = os.Symlink(exe, filepath.Join(binDir, "mpv"))
	ctlPath := filepath.Join(dir, "ctl.sock")
	testPath := os.Getenv("TTV_WINDOW_TEST") // só os testes de fluxo abrem esta porta

	cApp := C.CString(app_id)
	cHint := C.CString("SDL_APP_ID")
	C.SDL_SetHint(cHint, cApp)
	drv, wl := C.CString("SDL_VIDEO_DRIVER"), C.CString("wayland")
	C.SDL_SetHint(drv, wl)
	fatal := func(args ...any) {
		fmt.Fprintln(os.Stderr, args...)
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}
	if !C.SDL_Init(C.SDL_INIT_VIDEO | C.SDL_INIT_EVENTS) {
		fatal(C.GoString(C.SDL_GetError()))
	}
	// --probe answers whether this machine can open the window: the libraries
	// loaded (or this process would not exist), a Wayland display and a usable font.
	if len(os.Args) == 2 && os.Args[1] == "--probe" {
		if _, err := newFontSet(14); err != nil {
			fatal(err)
		}
		C.SDL_Quit()
		return
	}
	C.reg_events()
	title := C.CString("ttv-tui")
	win := C.make_window(title)
	C.SDL_GL_CreateContext(win)
	C.SDL_GL_SetSwapInterval(1)
	C.SDL_StartTextInput(win)

	scale := float64(C.SDL_GetWindowDisplayScale(win))
	if scale <= 0 {
		scale = 1
	}
	pt := 9.0
	if v, err := strconv.ParseFloat(os.Getenv("TTV_WINDOW_PT"), 64); err == nil {
		pt = v
	}
	fs, err := newFontSet(pt * 96.0 / 72.0 * scale)
	if err != nil {
		fatal(err)
	}

	a := &app{win: win, fs: fs, ctlq: make(chan ctlMsg, 16), paintReq: make(chan struct{}, 1), dirtyY1: -1}
	go a.painter()
	a.tex = C.make_tex()
	a.term = vt.NewSafeEmulator(80, 24)
	a.relayout()
	go a.serveCtl(ctlPath)
	if testPath != "" {
		_ = os.Remove(testPath)
		go a.serveTest(testPath)
	}
	layoutPath := filepath.Join(dir, "layout.sock")
	layoutLn, err := net.Listen("unix", layoutPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "layout:", err)
		os.Exit(1)
	}
	go a.serveLayout(layoutLn)

	a.cmd = exec.Command(tui, os.Args[1:]...)
	a.cmd.Env = append(os.Environ(),
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"TTV_WINDOW_SOCK="+ctlPath,
		"TTV_TUI_VIDEO_HOST="+layoutPath,
		"TERM=xterm-256color", "COLORTERM=truecolor")
	ptmx, err := pty.StartWithSize(a.cmd, &pty.Winsize{Cols: uint16(a.cols), Rows: uint16(a.rows), X: uint16(a.pw), Y: uint16(a.ph)})
	if err != nil {
		fmt.Fprintln(os.Stderr, "pty:", err)
		os.Exit(1)
	}
	a.ptmx = ptmx
	a.inputq = make(chan []byte, 64)
	go a.pumpInput()
	defer ptmx.Close()
	go a.pumpPTY()
	go a.pumpReplies()
	tuiDone := make(chan struct{})
	go func() {
		_ = a.cmd.Wait()
		close(tuiDone)
		C.push_quit()
	}()

	var ev C.SDL_Event
	for !a.quit {
		wait := C.Sint32(-1)
		if a.panelChanged && a.sess != nil {
			wait = 8 // sem quadro de vídeo a caminho (pausa), ainda precisamos apresentar o painel
		}
		if C.SDL_WaitEventTimeout(&ev, wait) {
			a.handle(&ev)
			for C.SDL_PollEvent(&ev) {
				a.handle(&ev)
			}
		}
		a.frame()
	}

	_ = a.cmd.Process.Signal(syscall.SIGHUP)
	waitTUI(tuiDone, tuiStopWait, func() { _ = a.cmd.Process.Kill() })
	a.stopSession()
	C.SDL_Quit()
}

// internal/player: defaultStopWait = 3 s e groupKillWait = 2 s.
// internal/app: orphanStopWait = 3 s. Mais 1 s para a limpeza da TUI.
const tuiStopWait = 3*time.Second + 2*time.Second + time.Second

func waitTUI(done <-chan struct{}, wait time.Duration, kill func()) {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		kill()
		<-done
	}
}
