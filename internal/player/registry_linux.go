package player

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// osGroupHasEndpoint reports whether a live member of pgid is the player of
// endpoint, as argvCarriesEndpoint judges its argv.
func osGroupHasEndpoint(pgid int, endpoint string) (bool, error) {
	for _, pid := range groupMembers(pgid) {
		argv, err := procArgv(pid)
		if err != nil {
			continue
		}
		if argvCarriesEndpoint(argv, endpoint) {
			return true, nil
		}
	}
	return false, nil
}

// groupMembers lists the live (non-zombie) processes whose group is pgid. A
// /proc that cannot be read yields none.
func groupMembers(pgid int) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var pids []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if procGroup(pid) == pgid {
			pids = append(pids, pid)
		}
	}
	return pids
}

// procGroup é o grupo de pid em /proc/<pid>/stat, ou 0 quando o processo saiu ou
// é zumbi.
func procGroup(pid int) int {
	fields := procStatFields(pid)
	if len(fields) < 3 || fields[0] == "Z" {
		return 0
	}
	pgrp, err := strconv.Atoi(fields[2])
	if err != nil {
		return 0
	}
	return pgrp
}

// procArgv é o argv de pid, lido de /proc/<pid>/cmdline.
func procArgv(pid int) ([]string, error) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return nil, err
	}
	return strings.Split(string(bytes.TrimSuffix(data, []byte{0})), "\x00"), nil
}

// pidfdOpen é o unix.PidfdOpen; um teste o troca para simular um kernel sem
// pidfd (anterior ao 5.3, ou um seccomp que bloqueia a chamada).
var pidfdOpen = unix.PidfdOpen

// osSignalPlayerGroup confere a identidade do grupo e sinaliza seus membros por
// pidfd. Primeiro passa por groupIsPlayer, que decide "não sei" do mesmo jeito em
// todo SO. Depois abre cada membro antes de lê-lo: o pidfd fixa o processo, e um
// pidfd_send_signal com sinal 0 depois da leitura prova que /proc descrevia esse
// mesmo processo e não um que herdou o PID. O sinal vai pelos mesmos pidfds, então
// nenhum processo que apareceu depois da conferência o recebe. Sem pidfd no
// kernel, cai na conferência pelo PGID (signalCheckedGroup).
func osSignalPlayerGroup(pgid int, socket string, sig groupSignal) (bool, error) {
	if isPlayer, err := groupIsPlayer(pgid, socket); err != nil || !isPlayer {
		return false, err
	}
	var fds []int
	defer func() {
		for _, fd := range fds {
			_ = unix.Close(fd)
		}
	}()
	isPlayer := false
	for _, pid := range groupMembers(pgid) {
		fd, err := pidfdOpen(pid, 0)
		if err != nil {
			if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EPERM) {
				return signalCheckedGroup(pgid, socket, sig)
			}
			continue
		}
		if procGroup(pid) != pgid {
			_ = unix.Close(fd)
			continue
		}
		argv, argvErr := procArgv(pid)
		if unix.PidfdSendSignal(fd, 0, nil, 0) != nil {
			// Saiu durante a leitura: o que /proc mostrou pode ser de outro processo.
			_ = unix.Close(fd)
			continue
		}
		fds = append(fds, fd)
		if argvErr == nil && argvCarriesEndpoint(argv, socket) {
			isPlayer = true
		}
	}
	if !isPlayer {
		return false, nil
	}
	signal := unix.SIGTERM
	if sig == signalKill {
		signal = unix.SIGKILL
	}
	for _, fd := range fds {
		_ = unix.PidfdSendSignal(fd, signal, nil, 0)
	}
	return true, nil
}

// procStatFields returns /proc/<pid>/stat fields starting at field 3 (state),
// skipping the parenthesised comm which may contain spaces.
func procStatFields(pid int) []string {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return nil
	}
	i := bytes.LastIndexByte(data, ')')
	if i < 0 || i+2 > len(data) {
		return nil
	}
	return strings.Fields(string(data[i+2:]))
}

// osProcStartTime returns field 22 (starttime, clock ticks since boot), or 0 when
// the process is gone.
func osProcStartTime(pid int) (uint64, error) {
	fields := procStatFields(pid)
	if len(fields) < 20 {
		return 0, nil
	}
	v, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return 0, nil
	}
	return v, nil
}
