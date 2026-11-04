package player

import (
	"errors"
	"io"
	"net"
	"os"
	"syscall"
	"testing"
)

func TestIsIPCPeerGone(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: true},
		{name: "EOF", err: io.EOF, want: true},
		{name: "net.ErrClosed", err: net.ErrClosed, want: true},
		{name: "ECONNRESET", err: syscall.ECONNRESET, want: true},
		{name: "EPIPE", err: syscall.EPIPE, want: true},
		{name: "ECONNABORTED", err: syscall.ECONNABORTED, want: true},
		{
			name: "wrapped OpError SyscallError ECONNRESET",
			err: &net.OpError{
				Op:  "read",
				Net: "unix",
				Err: &os.SyscallError{Syscall: "read", Err: syscall.ECONNRESET},
			},
			want: true,
		},
		{name: "unrelated", err: errors.New("boom"), want: false},
		{name: "EACCES", err: syscall.EACCES, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isIPCPeerGone(tc.err); got != tc.want {
				t.Fatalf("isIPCPeerGone(%v)=%v want %v", tc.err, got, tc.want)
			}
		})
	}
}
