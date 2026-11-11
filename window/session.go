package main

/*
#include "bridge.h"
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"syscall"
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

func (a *app) startSession(msg ctlMsg) {
	if a.sess != nil {
		a.stopSession()
	}
	m := C.mpv_create()
	opts := [][2]string{
		{"vo", "libmpv"}, {"hwdec", "auto-safe"}, {"input-ipc-server", msg.ipc},
		{"idle", "once"}, {"volume-max", "100"}, {"input-default-bindings", "no"},
		{"input-vo-keyboard", "no"}, {"osd-level", "1"}, {"osd-bar", "no"},
		{"background-color", fmt.Sprintf("#%02x%02x%02x", bgR, bgG, bgB)},
		{"keep-open", "no"}, {"terminal", "no"},
	}
	if os.Getenv("TTV_WINDOW_MUTE") == "1" {
		opts = append(opts, [2]string{"mute", "yes"})
	}
	for _, o := range opts {
		a.setOpt(m, o[0], o[1])
	}
	C.mpv_initialize(m)
	rc := C.make_rc(m, a.win)
	if rc == nil {
		fmt.Fprintln(os.Stderr, "render context falhou")
		C.mpv_terminate_destroy(m)
		_ = syscall.Close(msg.fd) // libmpv never took it: streamlink has to see the pipe close
		msg.conn.Close()
		return
	}
	s := &session{m: m, rc: rc, setProperty: propertySetter(m), conn: msg.conn, ipc: msg.ipc, done: make(chan struct{}), stopping: make(chan struct{}), started: time.Now()}
	a.sess = s
	a.mu.Lock()
	a.sessions++
	a.lastIpc = msg.ipc
	a.mu.Unlock()
	url := C.CString(fmt.Sprintf("fdclose://%d", msg.fd))
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
	a.relayout()
}

func (a *app) stopSession() {
	s := a.sess
	if s == nil {
		return
	}
	a.sess = nil
	s.stop.Store(true)
	close(s.stopping)
	C.mpv_wakeup(s.m)
	<-s.done
	C.rc_free(s.rc)
	C.mpv_terminate_destroy(s.m)
	s.conn.Close()
	a.refreshHole()
	a.relayout()
}

func (a *app) serveCtl(path string) {
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		panic(err)
	}
	for {
		c, err := l.AcceptUnix()
		if err != nil {
			return
		}
		go func(c *net.UnixConn) {
			data, oob := make([]byte, 8192), make([]byte, 128)
			n, oobn, _, _, err := c.ReadMsgUnix(data, oob)
			if err != nil {
				c.Close()
				return
			}
			var req struct {
				IPC string `json:"ipc"`
			}
			_ = json.Unmarshal(data[:n], &req)
			fd := -1
			if ms, err := syscall.ParseSocketControlMessage(oob[:oobn]); err == nil && len(ms) > 0 {
				if fds, err := syscall.ParseUnixRights(&ms[0]); err == nil && len(fds) > 0 {
					fd = fds[0]
				}
			}
			a.ctlq <- ctlMsg{kind: "start", ipc: req.IPC, fd: fd, conn: c}
			C.push_ev(C.ev_ctl())
		}(c)
	}
}

// stopSession pode aguardar done mesmo com ctlq cheia.
func (a *app) notifyEnded(s *session) {
	select {
	case a.ctlq <- ctlMsg{kind: "ended", s: s}:
		C.push_ev(C.ev_ctl())
	case <-s.stopping:
	}
}
