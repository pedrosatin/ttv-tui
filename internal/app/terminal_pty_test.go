//go:build unix

package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRejectsRedirectedTerminalStreams(t *testing.T) {
	for _, redirect := range []string{"stdin", "stdout", "null stdin"} {
		t.Run(redirect, func(t *testing.T) {
			master, slave := openPTY(t)
			defer func() { _ = master.Close(); _ = slave.Close() }()
			dir := t.TempDir()
			file, err := os.Create(filepath.Join(dir, "redirected"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = file.Close() }()
			input, output := slave, slave
			switch redirect {
			case "null stdin":
				null, err := os.Open(os.DevNull)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = null.Close() }()
				input = null
			case "stdin":
				input = file
			case "stdout":
				output = file
			}
			var log bytes.Buffer
			err = Run(Options{Lang: "en", ConfigPath: filepath.Join(dir, "config.toml"), Input: input, Output: output, Log: &log})
			if err == nil || !strings.Contains(err.Error(), "ssh -t") {
				t.Fatalf("Run error = %v", err)
			}
			info, err := file.Stat()
			if err != nil {
				t.Fatal(err)
			}
			if info.Size() != 0 || log.Len() != 0 {
				t.Fatal("redirected streams touched")
			}
			// A sentinel lets us read exactly what Run left on the PTY without a timeout.
			if _, err := slave.Write([]byte("sentinel")); err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, 4096)
			n, err := master.Read(buf)
			if err != nil {
				t.Fatal(err)
			}
			if string(buf[:n]) != "sentinel" {
				t.Fatalf("terminal touched: %q", buf[:n])
			}
		})
	}
}
