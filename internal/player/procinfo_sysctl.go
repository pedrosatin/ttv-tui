package player

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// Reading the Darwin process table. Only the three sysctl calls in
// registry_darwin.go need macOS; everything that interprets their bytes lives
// here, without a build tag, so the same code runs against fixed data on any OS.

// struct kinfo_proc as the kernel hands it to a 64-bit process, little-endian on
// both Darwin targets. registry_darwin.go refuses to compile if these drift from
// the layout in x/sys.
const (
	kinfoProcSize   = 648
	kinfoStartSec   = 0   // kp_proc.p_starttime.tv_sec
	kinfoStartUsec  = 8   // kp_proc.p_starttime.tv_usec
	kinfoStat       = 36  // kp_proc.p_stat
	kinfoPID        = 40  // kp_proc.p_pid
	kinfoPGID       = 564 // kp_eproc.e_pgid
	kinfoStatZombie = 5   // SZOMB
)

// kinfoProc is what the registry needs from one kinfo_proc.
type kinfoProc struct {
	pid    int
	pgid   int
	start  uint64 // p_starttime in microseconds since the epoch
	zombie bool
}

// parseKinfoProcs decodes a sysctl answer, which holds one kinfo_proc per match.
// No bytes means no match: the kernel reports a missing PID that way, not with an
// error.
func parseKinfoProcs(raw []byte) ([]kinfoProc, error) {
	if len(raw)%kinfoProcSize != 0 {
		return nil, fmt.Errorf("player: kinfo_proc answer of %d bytes", len(raw))
	}
	procs := make([]kinfoProc, 0, len(raw)/kinfoProcSize)
	for ; len(raw) > 0; raw = raw[kinfoProcSize:] {
		sec := int64(binary.LittleEndian.Uint64(raw[kinfoStartSec:]))
		usec := int32(binary.LittleEndian.Uint32(raw[kinfoStartUsec:]))
		if sec < 0 || usec < 0 || usec >= 1_000_000 {
			return nil, fmt.Errorf("player: kinfo_proc start time %d.%d out of range", sec, usec)
		}
		procs = append(procs, kinfoProc{
			pid:    int(int32(binary.LittleEndian.Uint32(raw[kinfoPID:]))),
			pgid:   int(int32(binary.LittleEndian.Uint32(raw[kinfoPGID:]))),
			start:  uint64(sec)*1_000_000 + uint64(usec),
			zombie: raw[kinfoStat] == kinfoStatZombie,
		})
	}
	return procs, nil
}

// parseProcArgs2 returns the argv in a KERN_PROCARGS2 answer, laid out as
//
//	argc (int32) | exec path NUL | NUL padding | argv... | environment...
//
// The environment is left out: it is inherited, so a process that merely descends
// from the player can carry the socket path there. An answer that does not hold
// argc complete strings is an error, not a shorter argv.
func parseProcArgs2(raw []byte) ([]string, error) {
	if len(raw) < 4 {
		return nil, fmt.Errorf("player: procargs answer of %d bytes", len(raw))
	}
	argc := int(int32(binary.LittleEndian.Uint32(raw)))
	if argc < 0 {
		return nil, fmt.Errorf("player: procargs argc %d", argc)
	}
	rest := raw[4:]
	end := bytes.IndexByte(rest, 0)
	if end < 0 {
		return nil, errors.New("player: procargs exec path not terminated")
	}
	rest = bytes.TrimLeft(rest[end:], "\x00")
	argv := make([]string, 0, min(argc, 64))
	for range argc {
		end := bytes.IndexByte(rest, 0)
		if end < 0 {
			return nil, fmt.Errorf("player: procargs holds %d of %d arguments", len(argv), argc)
		}
		argv = append(argv, string(rest[:end]))
		rest = rest[end+1:]
	}
	return argv, nil
}

// sysctlProcs is the raw answers of the kernel: kern.proc.pid, kern.proc.pgrp and
// kern.procargs2. A field is a function so a test can answer with fixed bytes.
type sysctlProcs struct {
	proc  func(pid int) ([]byte, error)
	group func(pgid int) ([]byte, error)
	args  func(pid int) ([]byte, error)
}

// startTime returns p_starttime of pid, or 0 when pid is gone. A zombie is gone:
// it exited and only waits to be reaped. An answer that does not describe pid is
// an error, so a layout this code does not know ends in "cannot tell".
func (s sysctlProcs) startTime(pid int) (uint64, error) {
	raw, err := s.proc(pid)
	if err != nil {
		return 0, fmt.Errorf("player: read process %d: %w", pid, err)
	}
	procs, err := parseKinfoProcs(raw)
	if err != nil {
		return 0, err
	}
	switch {
	case len(procs) == 0:
		return 0, nil
	case len(procs) > 1 || procs[0].pid != pid:
		return 0, fmt.Errorf("player: unexpected kinfo_proc answer for process %d", pid)
	case procs[0].zombie:
		return 0, nil
	case procs[0].start == 0:
		return 0, fmt.Errorf("player: process %d has no start time", pid)
	}
	return procs[0].start, nil
}

// groupHasEndpoint reports whether a live member of pgid is the player of
// endpoint, as argvCarriesEndpoint judges its argv. "No"
// needs every member read: the kernel refuses another user's arguments with the
// same EINVAL it gives for a process that is gone, so a member it refuses counts
// as unknown unless its start time proves that member left. A match in any member
// is proof on its own.
func (s sysctlProcs) groupHasEndpoint(pgid int, endpoint string) (bool, error) {
	raw, err := s.group(pgid)
	if err != nil {
		return false, fmt.Errorf("player: list process group %d: %w", pgid, err)
	}
	members, err := parseKinfoProcs(raw)
	if err != nil {
		return false, err
	}
	var unknown error
	for _, m := range members {
		if m.zombie {
			continue
		}
		if m.pgid != pgid {
			unknown = fmt.Errorf("player: process %d listed in group %d reports group %d", m.pid, pgid, m.pgid)
			continue
		}
		argv, err := s.argv(m.pid)
		if err != nil {
			if start, startErr := s.startTime(m.pid); startErr != nil || start == m.start {
				unknown = err
			}
			continue
		}
		if argvCarriesEndpoint(argv, endpoint) {
			return true, nil
		}
	}
	return false, unknown
}

func (s sysctlProcs) argv(pid int) ([]string, error) {
	raw, err := s.args(pid)
	if err != nil {
		return nil, fmt.Errorf("player: read arguments of process %d: %w", pid, err)
	}
	return parseProcArgs2(raw)
}
