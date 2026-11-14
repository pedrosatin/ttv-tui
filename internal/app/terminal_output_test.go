package app

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

// assertNoESCBELCSI fails when out carries ESC, BEL or the C1 CSI, as a byte or
// as the UTF-8 form of U+009B.
func assertNoESCBELCSI(t *testing.T, what, out string) {
	t.Helper()
	if strings.ContainsAny(out, "\x1b\x07\u009b") || strings.IndexByte(out, 0x9b) >= 0 {
		t.Fatalf("%s carries ESC, BEL or CSI: %q", what, out)
	}
}

func TestLogPlayerNotesEscapesControls(t *testing.T) {
	cat, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatal(err)
	}
	note := "player: [stream.hls][error] 403 \x1b]0;ZZ\x07\x1b[2J stream output\u009b2J\x9b"
	var log bytes.Buffer
	logPlayerNotes(&log, []string{note}, cat)
	assertNoESCBELCSI(t, "player notes", log.String())
	if want := `403 \x1b]0;ZZ\x07\x1b[2J stream output\u009b2J\x9b` + "\n"; !strings.HasSuffix(log.String(), want) {
		t.Fatalf("log = %q, want suffix %q", log.String(), want)
	}
	if strings.Count(log.String(), "\n") != 1 {
		t.Fatalf("one note became more than one line: %q", log.String())
	}
}

func TestLogStreamlinkRollbacksEscapesControls(t *testing.T) {
	cat, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	logStreamlinkRollbacks(&log, []streamlinkRollback{{
		From: "8.0.0\x1b]0;ZZ\x07\x1b[2J stream output",
		To:   "7.9.9\n\x9b",
	}}, cat)
	assertNoESCBELCSI(t, "rollback message", log.String())
	if !strings.Contains(log.String(), `streamlink 8.0.0\x1b]0;ZZ\x07\x1b[2J stream output failed`) ||
		!strings.HasSuffix(log.String(), `went back to 7.9.9\n\x9b`+"\n") {
		t.Fatalf("log = %q", log.String())
	}
}

func TestLogSaveFlushErrorEscapesPathControls(t *testing.T) {
	cat, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatal(err)
	}
	saveErr := &os.PathError{Op: "rename", Path: "/state/\x1b]0;ZZ\x07\x1b[2J/layout.json", Err: syscall.EACCES}
	var log bytes.Buffer
	logSaveFlushError(&log, fmt.Errorf("layout: %w", saveErr), cat)
	assertNoESCBELCSI(t, "save warning", log.String())
	if want := `rename /state/\x1b]0;ZZ\x07\x1b[2J/layout.json: permission denied` + "\n"; !strings.HasSuffix(log.String(), want) {
		t.Fatalf("log = %q, want suffix %q", log.String(), want)
	}
}

func TestWriteInputConfWarningEscapesPathControls(t *testing.T) {
	cat, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	writeInputConfWarning(&log, &os.PathError{Op: "open", Path: "/run/\x1b]0;ZZ\x07/input.conf", Err: syscall.EACCES}, cat)
	assertNoESCBELCSI(t, "input.conf warning", log.String())
	if !strings.Contains(log.String(), `open /run/\x1b]0;ZZ\x07/input.conf: permission denied`) {
		t.Fatalf("log = %q", log.String())
	}
}
