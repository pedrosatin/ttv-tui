//go:build windows

package main

import (
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// createUnicodeEnvBlock builds the block CREATE_UNICODE_ENVIRONMENT expects:
// one entry per name, case-insensitive, sorted, ending in a double NUL. The
// last value of a repeated name wins, so the shim's PATH beats the inherited one.
func createUnicodeEnvBlock(env []string) *uint16 {
	if len(env) == 0 {
		return nil
	}
	m := make(map[string]string)
	for _, kv := range env {
		if idx := strings.IndexByte(kv, '='); idx > 0 {
			m[strings.ToUpper(kv[:idx])] = kv[idx+1:]
		}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var block []uint16
	for _, k := range keys {
		block = append(block, utf16.Encode([]rune(k+"="+m[k]))...)
		block = append(block, 0)
	}
	block = append(block, 0)
	return &block[0]
}

// windowsPTY is a ConPTY session: the TUI reads and writes a console that
// conhost renders as VT sequences on our pipes.
type windowsPTY struct {
	hPC       windows.Handle
	inPipe    *os.File
	outPipe   *os.File
	proc      windows.Handle
	closeOnce sync.Once
}

func (p *windowsPTY) Read(b []byte) (int, error)  { return p.outPipe.Read(b) }
func (p *windowsPTY) Write(b []byte) (int, error) { return p.inPipe.Write(b) }

// Close closes the pseudoconsole before the pipes: older Windows builds block
// in ClosePseudoConsole until its last output is read, which pumpPTY does.
func (p *windowsPTY) Close() error {
	p.closeOnce.Do(func() {
		windows.ClosePseudoConsole(p.hPC)
		_ = p.inPipe.Close()
		_ = p.outPipe.Close()
		_ = windows.CloseHandle(p.proc)
	})
	return nil
}

func (p *windowsPTY) Resize(cols, rows, _, _ int) error {
	return windows.ResizePseudoConsole(p.hPC, windows.Coord{X: int16(cols), Y: int16(rows)})
}

func startPTY(cmd *exec.Cmd, cols, rows, _, _ int) (ptyDevice, error) {
	var inRead, inWrite, outRead, outWrite windows.Handle
	if err := windows.CreatePipe(&inRead, &inWrite, nil, 0); err != nil {
		return nil, err
	}
	if err := windows.CreatePipe(&outRead, &outWrite, nil, 0); err != nil {
		closeHandles(inRead, inWrite)
		return nil, err
	}
	// conhost keeps its own copies of the far ends; ours close once the child runs.
	defer closeHandles(inRead, outWrite)

	var hPC windows.Handle
	if err := windows.CreatePseudoConsole(windows.Coord{X: int16(cols), Y: int16(rows)}, inRead, outWrite, 0, &hPC); err != nil {
		closeHandles(inWrite, outRead)
		return nil, err
	}
	fail := func(err error) (ptyDevice, error) {
		windows.ClosePseudoConsole(hPC)
		closeHandles(inWrite, outRead)
		return nil, err
	}

	al, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return fail(err)
	}
	defer al.Delete()
	// The attribute takes the HPCON value itself, not a pointer to it.
	if err := al.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, *(*unsafe.Pointer)(unsafe.Pointer(&hPC)), unsafe.Sizeof(hPC)); err != nil {
		return fail(err)
	}

	var si windows.StartupInfoEx
	si.Cb = uint32(unsafe.Sizeof(si))
	si.ProcThreadAttributeList = al.List()

	cmdLine, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{cmd.Path}, cmd.Args[1:]...)))
	if err != nil {
		return fail(err)
	}
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT)
	var envBlock *uint16
	if len(cmd.Env) > 0 {
		envBlock = createUnicodeEnvBlock(cmd.Env)
		flags |= windows.CREATE_UNICODE_ENVIRONMENT
	}
	var pi windows.ProcessInformation
	if err := windows.CreateProcess(nil, cmdLine, nil, nil, false, flags, envBlock, nil, &si.StartupInfo, &pi); err != nil {
		return fail(err)
	}
	_ = windows.CloseHandle(pi.Thread)

	// exec.Cmd.Wait only needs Process; FindProcess opens a handle that can wait.
	proc, err := os.FindProcess(int(pi.ProcessId))
	if err != nil {
		_ = windows.TerminateProcess(pi.Process, 1)
		_ = windows.CloseHandle(pi.Process)
		return fail(err)
	}
	cmd.Process = proc

	return &windowsPTY{
		hPC:     hPC,
		inPipe:  os.NewFile(uintptr(inWrite), "conpty-in"),
		outPipe: os.NewFile(uintptr(outRead), "conpty-out"),
		proc:    pi.Process,
	}, nil
}

func closeHandles(hs ...windows.Handle) {
	for _, h := range hs {
		_ = windows.CloseHandle(h)
	}
}

// askTUIToQuit closes the pseudoconsole, which sends the TUI CTRL_CLOSE_EVENT
// (SIGTERM to Go). Windows has no SIGHUP, and os.Interrupt cannot be sent to
// another process. Close runs apart because it may wait for the output drain.
func askTUIToQuit(_ *exec.Cmd, p ptyDevice) {
	go func() { _ = p.Close() }()
}
