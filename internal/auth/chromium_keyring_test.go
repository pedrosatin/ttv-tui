package auth

import (
	"encoding/hex"
	"errors"
	"os/exec"
	"path/filepath"
	"testing"
)

// writeChromiumCookieDB builds a Chromium Cookies database in a temp dir with
// a single twitch.tv auth-token row holding encrypted.
func writeChromiumCookieDB(t *testing.T, encrypted []byte) string {
	t.Helper()
	requireSQLite3(t)
	path := filepath.Join(t.TempDir(), "Cookies")
	sql := `CREATE TABLE meta(key TEXT NOT NULL UNIQUE PRIMARY KEY, value TEXT NOT NULL);` +
		`INSERT INTO meta VALUES('version','24');` +
		`CREATE TABLE cookies(creation_utc INTEGER NOT NULL, host_key TEXT NOT NULL, name TEXT NOT NULL, value TEXT NOT NULL, encrypted_value BLOB NOT NULL);` +
		`INSERT INTO cookies VALUES(1, '.twitch.tv','auth-token','',X'` + hex.EncodeToString(encrypted) + `');`
	if out, err := exec.Command("sqlite3", path, sql).CombinedOutput(); err != nil {
		t.Fatalf("sqlite3: %v: %s", err, out)
	}
	return path
}

// fakeKeyring replaces the keyring lookup for one test and counts the calls.
func fakeKeyring(t *testing.T, secret string, err error) *int {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	orig := keyringLookup
	t.Cleanup(func() { keyringLookup = orig })
	calls := 0
	keyringLookup = func(Browser) (string, error) {
		calls++
		return secret, err
	}
	return &calls
}

func chromiumProfile(cookiesPath string) Profile {
	return Profile{Browser: BrowserBrave, Name: "Default", CookiesPath: cookiesPath, Family: "chromium"}
}

func TestExtractToken_V10UsesPeanutsWithoutKeyring(t *testing.T) {
	token := testToken(t)
	enc, err := EncryptChromiumValue(token, "peanuts", ".twitch.tv", true)
	if err != nil {
		t.Fatal(err)
	}
	enc[2] = '0' // v11 -> v10
	db := writeChromiumCookieDB(t, enc)
	calls := fakeKeyring(t, "", &Error{Op: "keyring", Err: ErrKeyringUnavailable, Message: "sem chaveiro"})

	got, err := ExtractToken(chromiumProfile(db))
	if err != nil {
		t.Fatalf("ExtractToken: %v", err)
	}
	if got != token {
		t.Fatal("token decifrado não bate com a fixture v10")
	}
	if *calls != 0 {
		t.Fatalf("chaveiro consultado %d vez(es) para cookie v10", *calls)
	}
}

func TestExtractToken_V11UsesKeyringSecret(t *testing.T) {
	token := testToken(t)
	password := testPassword(t)
	enc, err := EncryptChromiumValue(token, password, ".twitch.tv", true)
	if err != nil {
		t.Fatal(err)
	}
	db := writeChromiumCookieDB(t, enc)
	calls := fakeKeyring(t, password, nil)

	got, err := ExtractToken(chromiumProfile(db))
	if err != nil {
		t.Fatalf("ExtractToken: %v", err)
	}
	if got != token {
		t.Fatal("token decifrado não bate com a fixture v11")
	}
	if *calls != 1 {
		t.Fatalf("chaveiro consultado %d vez(es), esperado 1", *calls)
	}
}

func TestExtractToken_V11WithoutKeyringReportsKeyring(t *testing.T) {
	enc, err := EncryptChromiumValue(testToken(t), testPassword(t), ".twitch.tv", true)
	if err != nil {
		t.Fatal(err)
	}
	db := writeChromiumCookieDB(t, enc)
	fakeKeyring(t, "", &Error{Op: "keyring", Err: ErrKeyringUnavailable, Message: "sem chaveiro"})

	_, err = ExtractToken(chromiumProfile(db))
	if !errors.Is(err, ErrKeyringUnavailable) {
		t.Fatalf("err = %v, want ErrKeyringUnavailable", err)
	}
}
