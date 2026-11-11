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
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)
	binDir := filepath.Join(dir, "bin")
	_ = os.Mkdir(binDir, 0o755)
	exe, _ := os.Executable()
	_ = os.Symlink(exe, filepath.Join(binDir, "mpv"))
	ctlPath := filepath.Join(dir, "ctl.sock")
	testPath := os.Getenv("TTV_WINDOW_TEST") // só os testes de fluxo abrem esta porta

	fatal := func(args ...any) {
		fmt.Fprintln(os.Stderr, args...)
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}
	ctlLn, err := net.ListenUnix("unix", &net.UnixAddr{Name: ctlPath, Net: "unix"})
	if err != nil {
		fatal("control socket path:", err)
	}
	defer ctlLn.Close()
	layoutPath := filepath.Join(dir, "layout.sock")
	layoutLn, err := net.Listen("unix", layoutPath)
	if err != nil {
		fatal("layout socket path:", err)
	}
	defer layoutLn.Close()
	cApp := C.CString(app_id)
	cHint := C.CString("SDL_APP_ID")
	C.SDL_SetHint(cHint, cApp)
	drv, wl := C.CString("SDL_VIDEO_DRIVER"), C.CString("wayland")
	C.SDL_SetHint(drv, wl)
	if !C.SDL_Init(C.SDL_INIT_VIDEO | C.SDL_INIT_EVENTS) {
		fatal(C.GoString(C.SDL_GetError()))
	}
	defer C.SDL_Quit()
	title := C.CString("ttv-tui")
	win := C.make_window(title)
	if win == nil {
		fatal("window:", C.GoString(C.SDL_GetError()))
	}
	defer C.SDL_DestroyWindow(win)
	gl := C.SDL_GL_CreateContext(win)
	if gl == nil {
		fatal("GL context:", C.GoString(C.SDL_GetError()))
	}
	defer C.SDL_GL_DestroyContext(gl)

	scale := float64(C.SDL_GetWindowDisplayScale(win))
	pt := 9.0
	if v, err := strconv.ParseFloat(os.Getenv("TTV_WINDOW_PT"), 64); err == nil {
		pt = v
	}
	fs, err := scaledFontSet(pt, scale)
	if err != nil {
		fatal(err)
	}

	// Probe uses the same window, GL context and font metrics as normal startup.
	if len(os.Args) == 2 && os.Args[1] == "--probe" {
		return
	}
	C.SDL_GL_SetSwapInterval(1)
	C.SDL_StartTextInput(win)
	C.reg_events()

	a := &app{win: win, fs: fs, pt: pt, ctlq: make(chan ctlMsg, 16), paintReq: make(chan struct{}, 1), dirtyY1: -1}
	go a.painter()
	a.tex = C.make_tex()
	a.term = vt.NewSafeEmulator(80, 24)
	a.relayout()
	go a.serveCtl(ctlLn)
	if testPath != "" {
		_ = os.Remove(testPath)
		go a.serveTest(testPath)
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
