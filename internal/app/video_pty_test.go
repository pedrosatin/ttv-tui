//go:build unix

package app

import (
	"context"
	"io"
	"testing"
	"time"
)

func TestQueryDA1(t *testing.T) {
	for _, reply := range []string{"\x1b[?62;4;22c", "\x1b[?62;22c", ""} {
		t.Run(reply, func(t *testing.T) {
			master, slave := openPTY(t)
			defer func() { _ = master.Close(); _ = slave.Close() }()
			done := make(chan error, 1)
			go func() {
				query := make([]byte, 3)
				_, err := io.ReadFull(master, query)
				if err == nil && string(query) != "\x1b[c" {
					err = io.ErrUnexpectedEOF
				}
				if err == nil && reply != "" {
					// Fragment the DA1 response, as a real terminal may do.
					for _, b := range []byte(reply) {
						if _, err = master.Write([]byte{b}); err != nil {
							break
						}
					}
				}
				done <- err
			}()
			start := time.Now()
			got, err := queryDA1(context.Background(), slave, slave)
			if reply == "" {
				if err == nil || time.Since(start) > 2*time.Second {
					t.Fatalf("timeout: %v", err)
				}
			} else if err != nil || string(got) != reply {
				t.Fatalf("got %q err=%v", got, err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			// The cancelled reader is gone and the stream is still usable.
			if _, err := slave.Write([]byte("ok")); err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, 2)
			if _, err := io.ReadFull(master, buf); err != nil || string(buf) != "ok" {
				t.Fatalf("terminal not restored: %q %v", buf, err)
			}
		})
	}
}
