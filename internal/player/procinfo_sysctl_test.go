package player

import (
	"encoding/binary"
	"errors"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"
)

// kinfoBytes builds one kinfo_proc at the offsets of the C struct, written here as
// literals so a wrong constant in the parser cannot hide behind this fixture.
func kinfoBytes(pid, pgid int, stat byte, sec int64, usec int32) []byte {
	raw := make([]byte, 648)
	binary.LittleEndian.PutUint64(raw[0:], uint64(sec))
	binary.LittleEndian.PutUint32(raw[8:], uint32(usec))
	raw[36] = stat
	binary.LittleEndian.PutUint32(raw[40:], uint32(pid))
	binary.LittleEndian.PutUint32(raw[564:], uint32(pgid))
	return raw
}

// procArgs2Bytes builds a KERN_PROCARGS2 answer: argc, exec path, padding, argv
// and environment.
func procArgs2Bytes(execPath string, argv, env []string) []byte {
	raw := binary.LittleEndian.AppendUint32(nil, uint32(len(argv)))
	raw = append(raw, execPath...)
	raw = append(raw, 0, 0, 0, 0)
	for _, s := range slices.Concat(argv, env) {
		raw = append(append(raw, s...), 0)
	}
	return raw
}

// fakeProc is one row of the process table. start is in microseconds.
type fakeProc struct {
	pgid    int
	start   uint64
	stat    byte
	argv    []string
	env     []string
	argsErr error
}

func (p fakeProc) kinfo(pid int) []byte {
	return kinfoBytes(pid, p.pgid, p.stat, int64(p.start/1_000_000), int32(p.start%1_000_000))
}

// fakeKernel answers sysctl with bytes, as macOS would, including EINVAL for the
// arguments of a process that is missing or a zombie.
type fakeKernel struct {
	table    map[int]fakeProc
	procErr  error
	groupErr error
}

func (k fakeKernel) procs() sysctlProcs {
	return sysctlProcs{
		proc: func(pid int) ([]byte, error) {
			if k.procErr != nil {
				return nil, k.procErr
			}
			if p, ok := k.table[pid]; ok {
				return p.kinfo(pid), nil
			}
			return nil, nil
		},
		group: func(pgid int) ([]byte, error) {
			if k.groupErr != nil {
				return nil, k.groupErr
			}
			var raw []byte
			for pid, p := range k.table {
				if p.pgid == pgid {
					raw = append(raw, p.kinfo(pid)...)
				}
			}
			return raw, nil
		},
		args: func(pid int) ([]byte, error) {
			p, ok := k.table[pid]
			if !ok || p.stat == kinfoStatZombie {
				return nil, syscall.EINVAL
			}
			if p.argsErr != nil {
				return nil, p.argsErr
			}
			return procArgs2Bytes("/opt/homebrew/bin/x", p.argv, p.env), nil
		},
	}
}

func TestParseKinfoProcs(t *testing.T) {
	raw := append(kinfoBytes(501, 500, 2, 1_700_000_000, 123_456), kinfoBytes(502, 500, 5, 1_700_000_001, 0)...)
	got, err := parseKinfoProcs(raw)
	if err != nil {
		t.Fatalf("parseKinfoProcs: %v", err)
	}
	want := []kinfoProc{
		{pid: 501, pgid: 500, start: 1_700_000_000_123_456},
		{pid: 502, pgid: 500, start: 1_700_000_001_000_000, zombie: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseKinfoProcs = %+v, want %+v", got, want)
	}
}

func TestParseKinfoProcsEmptyIsNoMatch(t *testing.T) {
	got, err := parseKinfoProcs(nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("parseKinfoProcs(nil) = %+v, %v", got, err)
	}
}

func TestParseKinfoProcsRejectsWhatIsNotKinfoProc(t *testing.T) {
	cases := map[string][]byte{
		"truncated":     make([]byte, 647),
		"trailing":      make([]byte, 649),
		"32-bit layout": make([]byte, 492),
		"negative sec":  kinfoBytes(1, 1, 2, -1, 0),
		"usec overflow": kinfoBytes(1, 1, 2, 1, 1_000_000),
		"negative usec": kinfoBytes(1, 1, 2, 1, -1),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if got, err := parseKinfoProcs(raw); err == nil {
				t.Fatalf("parseKinfoProcs = %+v, want an error", got)
			}
		})
	}
}

func TestSysctlStartTime(t *testing.T) {
	const pid = 501
	cases := []struct {
		name    string
		raw     []byte
		rawErr  error
		want    uint64
		wantErr bool
	}{
		{name: "alive", raw: kinfoBytes(pid, pid, 2, 10, 5), want: 10_000_005},
		{name: "gone", raw: nil},
		{name: "zombie", raw: kinfoBytes(pid, pid, 5, 10, 5)},
		{name: "refused", rawErr: syscall.EPERM, wantErr: true},
		{name: "another pid", raw: kinfoBytes(pid+1, pid, 2, 10, 5), wantErr: true},
		{name: "two answers", raw: slices.Concat(kinfoBytes(pid, pid, 2, 10, 5), kinfoBytes(pid, pid, 2, 10, 5)), wantErr: true},
		{name: "no start time", raw: kinfoBytes(pid, pid, 2, 0, 0), wantErr: true},
		{name: "unknown layout", raw: make([]byte, 100), wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := sysctlProcs{proc: func(int) ([]byte, error) { return c.raw, c.rawErr }}
			got, err := s.startTime(pid)
			if (err != nil) != c.wantErr || got != c.want {
				t.Fatalf("startTime = %d, %v, want %d (error: %v)", got, err, c.want, c.wantErr)
			}
		})
	}
}

func TestParseProcArgs2(t *testing.T) {
	raw := []byte("\x03\x00\x00\x00" +
		"/opt/homebrew/bin/mpv\x00\x00\x00\x00" +
		"mpv\x00" +
		"--input-ipc-server=/tmp/1-a/i\x00" +
		"\x00" +
		"TERM=xterm\x00HOME=/Users/x\x00\x00\x00")
	got, err := parseProcArgs2(raw)
	if err != nil {
		t.Fatalf("parseProcArgs2: %v", err)
	}
	want := []string{"mpv", "--input-ipc-server=/tmp/1-a/i", ""}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseProcArgs2 = %q, want %q", got, want)
	}
}

// With a restricted target the kernel cuts the answer right after argv.
func TestParseProcArgs2WithoutEnvironment(t *testing.T) {
	got, err := parseProcArgs2(procArgs2Bytes("/bin/x", []string{"x", "y"}, nil))
	if err != nil || !reflect.DeepEqual(got, []string{"x", "y"}) {
		t.Fatalf("parseProcArgs2 = %q, %v", got, err)
	}
}

func TestParseProcArgs2NoArguments(t *testing.T) {
	got, err := parseProcArgs2(procArgs2Bytes("/bin/x", nil, []string{"A=1"}))
	if err != nil || len(got) != 0 {
		t.Fatalf("parseProcArgs2 = %q, %v, want no argv", got, err)
	}
}

func TestParseProcArgs2RejectsBrokenAnswers(t *testing.T) {
	cases := map[string][]byte{
		"empty":           nil,
		"short":           {1, 0},
		"negative argc":   []byte("\xff\xff\xff\xff/bin/x\x00\x00"),
		"open exec path":  []byte("\x01\x00\x00\x00/bin/x"),
		"missing strings": []byte("\x03\x00\x00\x00/bin/x\x00\x00x\x00y\x00"),
		"open last arg":   []byte("\x02\x00\x00\x00/bin/x\x00\x00x\x00y"),
		"huge argc":       []byte("\xff\xff\xff\x7f/bin/x\x00\x00x\x00"),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if got, err := parseProcArgs2(raw); err == nil {
				t.Fatalf("parseProcArgs2 = %q, want an error", got)
			}
		})
	}
}

func TestSysctlGroupHasArg(t *testing.T) {
	const (
		pgid   = 500
		socket = "/tmp/1-a/i"
	)
	mpv := []string{"mpv", "--input-ipc-server=" + socket}
	cases := []struct {
		name    string
		kernel  fakeKernel
		want    bool
		wantErr error
	}{
		{name: "argv of the leader", kernel: fakeKernel{table: map[int]fakeProc{
			pgid: {pgid: pgid, start: 1, argv: mpv},
		}}, want: true},
		{name: "argv of a descendant", kernel: fakeKernel{table: map[int]fakeProc{
			pgid:     {pgid: pgid, start: 1, argv: []string{"streamlink"}},
			pgid + 1: {pgid: pgid, start: 2, argv: mpv},
		}}, want: true},
		{name: "match beats a refused member", kernel: fakeKernel{table: map[int]fakeProc{
			pgid:     {pgid: pgid, start: 1, argsErr: syscall.EINVAL},
			pgid + 1: {pgid: pgid, start: 2, argv: mpv},
		}}, want: true},
		{name: "no such group", kernel: fakeKernel{}},
		{name: "stranger", kernel: fakeKernel{table: map[int]fakeProc{
			pgid: {pgid: pgid, start: 1, argv: []string{"stranger", "/tmp/1-b/i"}},
		}}},
		{name: "socket only in the environment", kernel: fakeKernel{table: map[int]fakeProc{
			pgid: {pgid: pgid, start: 1, argv: []string{"child"}, env: []string{"SOCK=" + socket}},
		}}},
		{name: "only a zombie has it", kernel: fakeKernel{table: map[int]fakeProc{
			pgid: {pgid: pgid, start: 1, stat: kinfoStatZombie, argv: mpv},
		}}},
		{name: "refused member", wantErr: syscall.EINVAL, kernel: fakeKernel{table: map[int]fakeProc{
			pgid: {pgid: pgid, start: 1, argsErr: syscall.EINVAL},
		}}},
		{name: "refused member among strangers", wantErr: syscall.EPERM, kernel: fakeKernel{table: map[int]fakeProc{
			pgid:     {pgid: pgid, start: 1, argsErr: syscall.EPERM},
			pgid + 1: {pgid: pgid, start: 2, argv: []string{"stranger"}},
		}}},
		{name: "listing refused", wantErr: syscall.EPERM, kernel: fakeKernel{groupErr: syscall.EPERM}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := c.kernel.procs().groupHasArg(pgid, socket)
			if got != c.want || !errors.Is(err, c.wantErr) {
				t.Fatalf("groupHasArg = %v, %v, want %v, %v", got, err, c.want, c.wantErr)
			}
		})
	}
}

// The kernel lists a member and it exits before its arguments are read. EINVAL
// then means "gone", which the start time tells apart from "refused".
func TestSysctlGroupHasArgMemberLeavesMidScan(t *testing.T) {
	const pgid = 500
	listing := kinfoBytes(pgid, pgid, 2, 10, 0)
	cases := []struct {
		name    string
		proc    func(int) ([]byte, error)
		wantErr bool
	}{
		{name: "gone", proc: func(int) ([]byte, error) { return nil, nil }},
		{name: "became a zombie", proc: func(int) ([]byte, error) { return kinfoBytes(pgid, pgid, 5, 10, 0), nil }},
		{name: "pid reused", proc: func(int) ([]byte, error) { return kinfoBytes(pgid, pgid, 2, 11, 0), nil }},
		{name: "same process", proc: func(int) ([]byte, error) { return listing, nil }, wantErr: true},
		{name: "cannot tell", proc: func(int) ([]byte, error) { return nil, syscall.EPERM }, wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := sysctlProcs{
				proc:  c.proc,
				group: func(int) ([]byte, error) { return listing, nil },
				args:  func(int) ([]byte, error) { return nil, syscall.EINVAL },
			}
			got, err := s.groupHasArg(pgid, "/tmp/1-a/i")
			if got || (err != nil) != c.wantErr {
				t.Fatalf("groupHasArg = %v, %v, want false (error: %v)", got, err, c.wantErr)
			}
		})
	}
}

func TestSysctlGroupHasArgDistrustsForeignMember(t *testing.T) {
	const pgid = 500
	s := sysctlProcs{
		group: func(int) ([]byte, error) { return kinfoBytes(pgid+1, pgid+7, 2, 10, 0), nil },
		args:  func(int) ([]byte, error) { return procArgs2Bytes("/bin/x", []string{"x"}, nil), nil },
	}
	got, err := s.groupHasArg(pgid, "/tmp/1-a/i")
	if got || err == nil || !strings.Contains(err.Error(), "reports group") {
		t.Fatalf("groupHasArg = %v, %v, want an error", got, err)
	}
}

// A zombie may have lost its group: the kernel still lists it by p_pgrpid but fills
// e_pgid from the group, which leaves 0. That is an exited process, not a stranger.
func TestSysctlGroupHasArgSkipsZombieWithoutGroup(t *testing.T) {
	const pgid = 500
	s := sysctlProcs{
		group: func(int) ([]byte, error) { return kinfoBytes(pgid, 0, 5, 10, 0), nil },
		args:  func(int) ([]byte, error) { return nil, syscall.EINVAL },
	}
	if got, err := s.groupHasArg(pgid, "/tmp/1-a/i"); got || err != nil {
		t.Fatalf("groupHasArg = %v, %v, want false with no error", got, err)
	}
}

func TestSysctlGroupHasArgMalformedArgumentsAreUnknown(t *testing.T) {
	const pgid = 500
	listing := kinfoBytes(pgid, pgid, 2, 10, 0)
	s := sysctlProcs{
		proc:  func(int) ([]byte, error) { return listing, nil },
		group: func(int) ([]byte, error) { return listing, nil },
		args:  func(int) ([]byte, error) { return []byte("\x05\x00\x00\x00/bin/x\x00\x00x\x00"), nil },
	}
	if got, err := s.groupHasArg(pgid, "/tmp/1-a/i"); got || err == nil {
		t.Fatalf("groupHasArg = %v, %v, want an error", got, err)
	}
}
