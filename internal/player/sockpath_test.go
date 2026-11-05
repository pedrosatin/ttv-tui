package player

import (
	"runtime"
	"testing"
)

func TestMaxSocketPathLenFor(t *testing.T) {
	tests := []struct {
		goos string
		want int // 0 = no limit
	}{
		{"linux", 107},   // sun_path is 108 bytes, one for the NUL
		{"darwin", 103},  // sun_path is 104 bytes, one for the NUL
		{"freebsd", 103}, // the BSDs share Darwin's 104
		{"windows", 0},   // named pipe, not a path
	}
	for _, tc := range tests {
		if got := maxSocketPathLenFor(tc.goos); got != tc.want {
			t.Errorf("maxSocketPathLenFor(%q) = %d, want %d", tc.goos, got, tc.want)
		}
	}
	if sunPathSizeLinux != 108 || sunPathSizeDarwin != 104 {
		t.Errorf("sun_path sizes = %d/%d, want 108/104", sunPathSizeLinux, sunPathSizeDarwin)
	}
}

func TestMaxSocketPathLenMatchesRunningOS(t *testing.T) {
	if got, want := maxSocketPathLen, maxSocketPathLenFor(runtime.GOOS); got != want {
		t.Fatalf("maxSocketPathLen = %d, want %d for %s", got, want, runtime.GOOS)
	}
}
