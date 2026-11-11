package auth

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestMain points HOME and the XDG dirs at an empty temp dir, so no test in this
// package reads the operator's ~/.sqliterc, browser profiles or ttv-tui config.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ttv-tui-auth-test-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, k := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME"} {
		if err := os.Setenv(k, dir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

const firefoxSchema = `CREATE TABLE moz_cookies(id INTEGER PRIMARY KEY, ` +
	`originAttributes TEXT NOT NULL DEFAULT '', name TEXT, value TEXT, host TEXT, creationTime INTEGER);`

func firefoxRow(originAttributes, value string, created int) string {
	return fmt.Sprintf(`INSERT INTO moz_cookies(originAttributes, name, value, host, creationTime) `+
		`VALUES('%s', 'auth-token', '%s', '.twitch.tv', %d);`, originAttributes, value, created)
}

func sqlite3Exec(t *testing.T, path, sql string) {
	t.Helper()
	requireSQLite3(t)
	if out, err := exec.Command("sqlite3", "-init", os.DevNull, path, sql).CombinedOutput(); err != nil {
		t.Fatalf("sqlite3: %v: %s", err, out)
	}
}

func chromiumV10(t *testing.T, value, hostKey string) []byte {
	t.Helper()
	enc, err := EncryptChromiumValue(value, chromiumV10Pass, hostKey, true)
	if err != nil {
		t.Fatal(err)
	}
	enc[2] = '0' // v11 -> v10
	return enc
}

func TestExtract_IgnoresSqliterc(t *testing.T) {
	ff := filepath.Join(t.TempDir(), "cookies.sqlite")
	sqlite3Exec(t, ff, firefoxSchema+firefoxRow("", "token-firefox", 1))
	cr := writeChromiumCookieDB(t, chromiumV10(t, "token-chromium", ".twitch.tv"))
	fakeKeyring(t, "", errors.New("cookie v10 não consulta o chaveiro"))

	home, xdg := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	rc := []byte(".headers on\n.mode box\n")
	if err := os.WriteFile(filepath.Join(home, ".sqliterc"), rc, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(xdg, "sqlite3"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(xdg, "sqlite3", "sqliterc"), rc, 0o600); err != nil {
		t.Fatal(err)
	}

	if got, err := ExtractFirefox(ff); err != nil || got != "token-firefox" {
		t.Errorf("Firefox com .sqliterc: got %q, err %v", got, err)
	}
	if got, err := ExtractToken(chromiumProfile(cr)); err != nil || got != "token-chromium" {
		t.Errorf("Chromium com .sqliterc: got %q, err %v", got, err)
	}
}

// The newest cookie wins in Chromium, whatever the row order on disk.
func TestExtractChromium_PicksNewestRow(t *testing.T) {
	db := writeChromiumCookieDB(t, chromiumV10(t, "token-velho", ".twitch.tv"))
	sqlite3Exec(t, db, `INSERT INTO cookies VALUES(2, 'twitch.tv', 'auth-token', '', X'`+
		hex.EncodeToString(chromiumV10(t, "token-novo", "twitch.tv"))+`');`)
	fakeKeyring(t, "", errors.New("cookie v10 não consulta o chaveiro"))

	got, err := ExtractToken(chromiumProfile(db))
	if err != nil {
		t.Fatalf("ExtractToken: %v", err)
	}
	if got != "token-novo" {
		t.Fatalf("got %q, want token-novo", got)
	}
}

// Firefox prefers the default container, then the newest cookie; a container
// cookie is used only when no default one exists.
func TestExtractFirefox_PicksDefaultContainerThenNewest(t *testing.T) {
	cases := []struct {
		name string
		rows string
		want string
	}{
		{
			name: "container mais novo perde para o padrão",
			rows: firefoxRow("^userContextId=1", "token-container", 3) +
				firefoxRow("", "token-velho", 1) +
				firefoxRow("", "token-novo", 2),
			want: "token-novo",
		},
		{
			name: "só o container tem cookie",
			rows: firefoxRow("^userContextId=1", "token-container", 1),
			want: "token-container",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := filepath.Join(t.TempDir(), "cookies.sqlite")
			sqlite3Exec(t, db, firefoxSchema+tc.rows)
			got, err := ExtractFirefox(db)
			if err != nil {
				t.Fatalf("ExtractFirefox: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// interruptWrite runs sql in a sqlite3 process and kills it after the last
// statement, so the database keeps the WAL or hot journal a crashed browser
// would leave behind.
func interruptWrite(t *testing.T, path, sql string) {
	t.Helper()
	requireSQLite3(t)
	cmd := exec.Command("sqlite3", "-init", os.DevNull, path)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(stdin, sql+"\nSELECT 'pronto';\n"); err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		if sc.Text() == "pronto" {
			break
		}
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
}

func readOrMissing(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "<ausente>"
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestExtractFirefox_LeavesPendingWALAndJournalOnOriginal(t *testing.T) {
	cases := []struct {
		name    string
		sidecar string
		sql     string
		want    string
	}{
		{
			name:    "WAL",
			sidecar: "-wal",
			sql:     `PRAGMA journal_mode=WAL;` + firefoxSchema + firefoxRow("", "token-wal", 1),
			want:    "token-wal",
		},
		{
			name:    "journal",
			sidecar: "-journal",
			sql: firefoxSchema + firefoxRow("", "token-confirmado", 1) +
				`CREATE TABLE pad(b BLOB); PRAGMA cache_size=2; BEGIN;` +
				`UPDATE moz_cookies SET value='token-pendente';` +
				`INSERT INTO pad VALUES(zeroblob(400000));`,
			want: "token-confirmado",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := filepath.Join(t.TempDir(), "cookies.sqlite")
			interruptWrite(t, db, tc.sql)
			side := db + tc.sidecar
			if st, err := os.Stat(side); err != nil || st.Size() == 0 {
				t.Fatalf("fixture sem %s pendente: %v", tc.sidecar, err)
			}
			mainBefore, sideBefore := readOrMissing(t, db), readOrMissing(t, side)

			got, err := ExtractFirefox(db)
			if err != nil {
				t.Fatalf("ExtractFirefox: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
			if readOrMissing(t, db) != mainBefore {
				t.Errorf("banco original mudou: o %s pendente foi aplicado a ele", tc.sidecar)
			}
			if readOrMissing(t, side) != sideBefore {
				t.Errorf("%s do original mudou ou sumiu", tc.sidecar)
			}
		})
	}
}
