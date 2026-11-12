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

func createUnicodeEnvBlock(env []string) *uint16 {
	if len(env) == 0 {
		return nil
	}
	m := make(map[string]string)
	for _, kv := range env {
		idx := strings.IndexByte(kv, '=')
		if idx > 0 {
			k := strings.ToUpper(kv[:idx])
			m[k] = kv[idx+1:]
		}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var block []uint16
	for _, k := range keys {
		line := k + "=" + m[k]
		for _, r := range line {
			block = utf16.AppendRune(block, r)
		}
		block = append(block, 0)
	}
	block = append(block, 0)
	return &block[0]
}

type windowsPTY struct {
	hPC       windows.Handle
	inPipe    *os.File
	outPipe   *os.File
	proc      windows.Handle
	thread    windows.Handle
	closeOnce sync.Once
}

func (p *windowsPTY) Read(b []byte) (int, error) {
	return p.outPipe.Read(b)
}

func (p *windowsPTY) Write(b []byte) (int, error) {
	return p.inPipe.Write(b)
}

func (p *windowsPTY) Close() error {
	p.closeOnce.Do(func() {
		_ = p.inPipe.Close()
		_ = p.outPipe.Close()
		windows.ClosePseudoConsole(p.hPC)
		if p.thread != 0 {
			_ = windows.CloseHandle(p.thread)
		}
		if p.proc != 0 {
			_ = windows.CloseHandle(p.proc)
		}
	})
	return nil
}

func (p *windowsPTY) Resize(cols, rows, pw, ph int) error {
	return windows.ResizePseudoConsole(p.hPC, windows.Coord{X: int16(cols), Y: int16(rows)})
}

func startPTY(cmd *exec.Cmd, cols, rows, pw, ph int) (ptyDevice, error) {
	var inRead, inWrite, outRead, outWrite windows.Handle
	if err := windows.CreatePipe(&inRead, &inWrite, nil, 0); err != nil {
		return nil, err
	}
	if err := windows.CreatePipe(&outRead, &outWrite, nil, 0); err != nil {
		windows.CloseHandle(inRead)
		windows.CloseHandle(inWrite)
		return nil, err
	}

	var hPC windows.Handle
	size := windows.Coord{X: int16(cols), Y: int16(rows)}
	if err := windows.CreatePseudoConsole(size, inRead, outWrite, 0, &hPC); err != nil {
		windows.CloseHandle(inRead)
		windows.CloseHandle(inWrite)
		windows.CloseHandle(outRead)
		windows.CloseHandle(outWrite)
		return nil, err
	}

	al, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		windows.ClosePseudoConsole(hPC)
		return nil, err
	}
	defer al.Delete()

	if err := al.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, unsafe.Pointer(hPC), unsafe.Sizeof(hPC)); err != nil {
		windows.ClosePseudoConsole(hPC)
		return nil, err
	}

	var si windows.StartupInfoEx
	si.Cb = uint32(unsafe.Sizeof(si))
	si.ProcThreadAttributeList = al.List()

	var pi windows.ProcessInformation
	argv := append([]string{cmd.Path}, cmd.Args[1:]...)
	cmdLine := windows.StringToUTF16Ptr(windows.ComposeCommandLine(argv))
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT)
	var envBlock *uint16
	if len(cmd.Env) > 0 {
		envBlock = createUnicodeEnvBlock(cmd.Env)
		flags |= windows.CREATE_UNICODE_ENVIRONMENT
	}

	err = windows.CreateProcess(nil, cmdLine, nil, nil, false, flags, envBlock, nil, &si.StartupInfo, &pi)
	windows.CloseHandle(inRead)
	windows.CloseHandle(outWrite)
	if err != nil {
		windows.CloseHandle(inWrite)
		windows.CloseHandle(outRead)
		windows.ClosePseudoConsole(hPC)
		return nil, err
	}

	p, findErr := os.FindProcess(int(pi.ProcessId))
	if findErr != nil {
		cmd.Process = &os.Process{Pid: int(pi.ProcessId)}
	} else {
		cmd.Process = p
	}

	return &windowsPTY{
		hPC:     hPC,
		inPipe:  os.NewFile(uintptr(inWrite), "conpty-in"),
		outPipe: os.NewFile(uintptr(outRead), "conpty-out"),
		proc:    pi.Process,
		thread:  pi.Thread,
	}, nil
}
