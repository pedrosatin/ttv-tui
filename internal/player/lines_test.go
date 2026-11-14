package player

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestLongLineThenOffline(t *testing.T) {
	mgr := newTestManager(t, "long-line-offline", Options{SocketWait: 30 * time.Second})
	gen, err := mgr.Start(testTarget("xqc"))
	if err != nil {
		t.Fatal(err)
	}
	mgr.mu.Lock()
	pid := mgr.session.Pid()
	mgr.mu.Unlock()
	ev := waitError(t, mgr, gen, 3*time.Second)
	if ev.Failure != FailureOffline {
		t.Fatalf("falha = %q, esperado offline", ev.Failure)
	}
	deadline := time.Now().Add(3 * time.Second)
	for groupExists(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("processo %d continua vivo após offline", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestConsumeLinesLimitsLastLine(t *testing.T) {
	for _, ending := range []string{"", "\n", "\r\n"} {
		for _, size := range []int{0, 65536, streamlinkLineLimit - 1, streamlinkLineLimit, streamlinkLineLimit + 1, 2 * 1024 * 1024} {
			t.Run(fmt.Sprintf("%d/%q", size, ending), func(t *testing.T) {
				s := &Session{}
				s.readers.Add(1)
				s.consumeLines(context.Background(), strings.NewReader(strings.Repeat("x", size)+ending))
				want := strings.Repeat("x", size)
				if size > streamlinkLineLimit {
					want = want[:streamlinkLineLimit-len(truncatedLineSuffix)] + truncatedLineSuffix
				}
				if s.lastLine != want {
					t.Fatalf("última linha: %d bytes; esperado %d bytes, truncagem = %v", len(s.lastLine), len(want), strings.HasSuffix(s.lastLine, truncatedLineSuffix))
				}
			})
		}
	}
}
