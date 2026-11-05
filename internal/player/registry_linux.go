package player

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// osGroupHasArg reports whether a live member of pgid carries arg in its command
// line.
func osGroupHasArg(pgid int, arg string) (bool, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false, nil
	}
	needle := []byte(arg)
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		fields := procStatFields(pid)
		if len(fields) < 3 || fields[0] == "Z" {
			continue
		}
		if pgrp, err := strconv.Atoi(fields[2]); err != nil || pgrp != pgid {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err != nil {
			continue
		}
		if bytes.Contains(cmdline, needle) {
			return true, nil
		}
	}
	return false, nil
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
