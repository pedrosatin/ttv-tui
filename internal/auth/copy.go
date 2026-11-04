package auth

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// runSQLite is replaceable in tests.
var runSQLite = runSQLiteCLI

func runSQLiteCLI(dbPath, sql string) (string, error) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		return "", &Error{
			Op:      "sqlite",
			Err:     ErrSQLiteUnavailable,
			Message: "sqlite3 não está no PATH; instale o pacote sqlite para ler o banco de cookies",
		}
	}
	cmd := exec.Command("sqlite3", "-separator", "|", dbPath, sql)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", wrap("sqlite", err, fmt.Sprintf("falha ao consultar %s: %s", dbPath, strings.TrimSpace(string(out))))
	}
	return string(out), nil
}

// copyCookieDB copies a cookie database (and WAL/SHM sidecars when present) into a
// temporary directory so the browser's lock on the original file is not contested.
// Never writes to the original. cleanup removes the temp directory.
func copyCookieDB(src string) (tmpPath string, cleanup func(), err error) {
	noop := func() {}
	if src == "" {
		return "", noop, &Error{Op: "copy", Err: fmt.Errorf("caminho de cookies vazio"), Message: "caminho de cookies vazio"}
	}
	if _, err := os.Stat(src); err != nil {
		return "", noop, wrap("copy", err, fmt.Sprintf("banco de cookies ausente: %s", src))
	}

	dir, err := os.MkdirTemp("", "ttv-tui-cookies-*")
	if err != nil {
		return "", noop, wrap("copy", err, "falha ao criar diretório temporário para cookies")
	}
	cleanup = func() { _ = os.RemoveAll(dir) }

	base := filepath.Base(src)
	dst := filepath.Join(dir, base)

	// Prefer the SQLite online backup API when sqlite3 is available: it produces a
	// consistent snapshot even if the browser has the DB open in WAL mode.
	if _, lookErr := exec.LookPath("sqlite3"); lookErr == nil {
		backupSQL := fmt.Sprintf(".backup '%s'", strings.ReplaceAll(dst, "'", "''"))
		cmd := exec.Command("sqlite3", src, backupSQL)
		if out, backupErr := cmd.CombinedOutput(); backupErr == nil {
			return dst, cleanup, nil
		} else {
			// Fall through to file copy; keep the error only if file copy also fails.
			_ = out
		}
	}

	if err := copyFile(src, dst); err != nil {
		cleanup()
		return "", noop, wrap("copy", err, fmt.Sprintf("falha ao copiar %s", src))
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		sideSrc := src + suffix
		if st, statErr := os.Stat(sideSrc); statErr == nil && !st.IsDir() {
			if cpErr := copyFile(sideSrc, dst+suffix); cpErr != nil {
				cleanup()
				return "", noop, wrap("copy", cpErr, fmt.Sprintf("falha ao copiar sidecar %s", sideSrc))
			}
		}
	}
	return dst, cleanup, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
