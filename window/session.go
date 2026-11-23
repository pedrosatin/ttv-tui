package main

/*
#include "bridge.h"
*/
import "C"

import (
	"fmt"
	"net"
	"os"
	"time"
	"unsafe"
)

func (a *app) setOpt(m *C.mpv_handle, k, v string) {
	ck, cv := C.CString(k), C.CString(v)
	C.mpv_set_option_string(m, ck, cv)
	C.free(unsafe.Pointer(ck))
	C.free(unsafe.Pointer(cv))
}

// propertySetter só enfileira no core; pode ser usado com o render ativo.
func propertySetter(m *C.mpv_handle) func(string, string) {
	return func(k, v string) {
		ck, cv := C.CString(k), C.CString(v)
		C.set_property(m, ck, cv)
		C.free(unsafe.Pointer(ck))
		C.free(unsafe.Pointer(cv))
	}
}

func normalizeRole(role string) string {
	if role == rolePreview {
		return rolePreview
	}
	return roleMain
}

// sessionOptions devolve as opções do libmpv para a sessão. A prévia sobe muda
// mesmo quando TTV_WINDOW_MUTE está desligado.
func sessionOptions(role, ipc, muteEnv string) [][2]string {
	opts := [][2]string{
		{"vo", "libmpv"}, {"hwdec", "auto-safe"}, {"input-ipc-server", ipc},
		{"idle", "once"}, {"volume-max", "100"}, {"input-default-bindings", "no"},
		{"input-vo-keyboard", "no"}, {"osd-level", "1"}, {"osd-bar", "no"},
		{"background-color", fmt.Sprintf("#%02x%02x%02x", bgR, bgG, bgB)},
		{"keep-open", "no"}, {"terminal", "no"},
	}
	if normalizeRole(role) == rolePreview || muteEnv == "1" {
		opts = append(opts, [2]string{"mute", "yes"})
	}
	return opts
}

// activeVideo devolve a sessão que ocupa o painel inteiro de vídeo agora:
// a principal tem prioridade sobre a prévia.
func (a *app) activeVideo() *session {
	if a.sess != nil {
		return a.sess
	}
	return a.preview
}

func (a *app) startSession(msg ctlMsg) {
	role := normalizeRole(msg.role)
	if role == rolePreview {
		a.stopPreviewSession()
	} else {
		a.stopMainSession()
	}
	m := C.mpv_create()
	for _, o := range sessionOptions(role, msg.ipc, os.Getenv("TTV_WINDOW_MUTE")) {
		a.setOpt(m, o[0], o[1])
	}
	C.mpv_initialize(m)
	rc := C.make_rc(m, a.win)
	if rc == nil {
		fmt.Fprintln(os.Stderr, "render context falhou")
		C.mpv_terminate_destroy(m)
		closeFD(msg.fd) // libmpv never took it: streamlink has to see the pipe close
		_ = msg.conn.Close()
		return
	}
	s := &session{
		role:        role,
		m:           m,
		rc:          rc,
		setProperty: propertySetter(m),
		conn:        msg.conn,
		ipc:         msg.ipc,
		done:        make(chan struct{}),
		stopping:    make(chan struct{}),
		started:     time.Now(),
	}
	if role == rolePreview {
		a.preview = s
		a.previewDirty = false
		a.previewAtHostSeq = a.hostSeq
		C.pip_fbo_free()
	} else {
		a.sess = s
		a.mainDirty = false
	}
	a.mu.Lock()
	a.sessions++
	a.lastIpc = msg.ipc
	a.lastRole = role
	a.mu.Unlock()
	url := C.CString(msg.url)
	C.load_fd(m, url)
	C.free(unsafe.Pointer(url))
	go func() {
		defer close(s.done)
		for {
			ev := C.mpv_wait_event(m, -1)
			if ev.event_id == C.MPV_EVENT_SHUTDOWN {
				a.notifyEnded(s)
				return
			}
			if s.stop.Load() {
				return
			}
		}
	}()
	go func() { // shim morreu (TUI matou o grupo): encerra o core
		b := make([]byte, 1)
		_, _ = msg.conn.Read(b)
		a.notifyEnded(s)
	}()
	a.refreshHole()
	if a.win != nil {
		a.relayout()
	} else {
		a.applyMargins()
		a.layoutDirty = true
	}
}

func (a *app) stopMainSession() {
	a.stopSlot(&a.sess)
}

func (a *app) stopPreviewSession() {
	a.stopSlot(&a.preview)
}

func (a *app) stopSession() {
	a.stopMainSession()
	a.stopPreviewSession()
}

func (a *app) endSession(s *session) {
	switch s {
	case nil:
		return
	case a.sess:
		a.stopMainSession()
	case a.preview:
		a.stopPreviewSession()
	}
}

func (a *app) stopSlot(slot **session) {
	s := *slot
	if s == nil {
		return
	}
	isPreview := slot == &a.preview
	*slot = nil
	if isPreview {
		a.previewDirty = false
		C.pip_fbo_free()
	} else {
		a.mainDirty = false
		// Sai do modo PiP: o FBO da prévia sobre a principal não serve mais.
		if a.preview != nil {
			C.pip_fbo_free()
		}
	}
	s.stop.Store(true)
	if s.stopping != nil {
		close(s.stopping)
	}
	if s.m != nil {
		C.mpv_wakeup(s.m)
	}
	if s.done != nil {
		<-s.done
	}
	if s.rc != nil {
		C.rc_free(s.rc)
	}
	if s.m != nil {
		C.mpv_terminate_destroy(s.m)
	}
	if s.conn != nil {
		_ = s.conn.Close()
	}
	a.refreshHole()
	if a.win != nil {
		a.relayout()
	} else {
		a.applyMargins()
		a.layoutDirty = true
	}
}

func (a *app) serveCtl(l *net.UnixListener) {
	for {
		c, err := l.AcceptUnix()
		if err != nil {
			return
		}
		go func(c *net.UnixConn) {
			msg, err := readStart(c)
			if err != nil {
				_ = c.Close()
				return
			}
			a.ctlq <- msg
			C.push_ev(C.ev_ctl())
		}(c)
	}
}

// stopSlot pode aguardar done mesmo com ctlq cheia.
func (a *app) notifyEnded(s *session) {
	select {
	case a.ctlq <- ctlMsg{kind: "ended", s: s}:
		C.push_ev(C.ev_ctl())
	case <-s.stopping:
	}
}
