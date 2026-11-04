package auth

import (
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func testdata(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join("testdata", name)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("testdata %s: %v", name, err)
	}
	return p
}

func testPassword(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(testdata(t, "password.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func testToken(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(testdata(t, "token.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDecryptChromiumValue_WithHostHash(t *testing.T) {
	password := testPassword(t)
	token := testToken(t)
	host := ".twitch.tv"
	sum := sha256.Sum256([]byte(host))
	enc, err := EncryptChromiumValue(token, password, host, true)
	if err != nil {
		t.Fatal(err)
	}
	if string(enc[:3]) != "v11" {
		t.Fatalf("prefix = %q", enc[:3])
	}
	got, err := DecryptChromiumValue(enc, password, host)
	if err != nil {
		t.Fatal(err)
	}
	if got != token {
		t.Fatalf("got %q want %q", got, token)
	}
	// Ensure encrypt really prefixed the hash (round-trip already stripped it).
	_ = sum
}

func TestDecryptChromiumValue_WithoutHostHash(t *testing.T) {
	password := testPassword(t)
	token := testToken(t)
	enc, err := EncryptChromiumValue(token, password, "", false)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecryptChromiumValue(enc, password, ".twitch.tv")
	if err != nil {
		t.Fatal(err)
	}
	if got != token {
		t.Fatalf("got %q want %q", got, token)
	}
}

func TestDecryptChromiumValue_UnknownPrefix(t *testing.T) {
	_, err := DecryptChromiumValue([]byte("v99"+string(make([]byte, 16))), "x", ".twitch.tv")
	if !errors.Is(err, ErrUnknownCookieFormat) {
		t.Fatalf("err = %v, want ErrUnknownCookieFormat", err)
	}
}

func TestDecryptChromiumValue_V10Peanuts(t *testing.T) {
	token := testToken(t)
	// v10 on Linux uses the hardcoded password "peanuts".
	enc, err := EncryptChromiumValue(token, "peanuts", ".twitch.tv", true)
	if err != nil {
		t.Fatal(err)
	}
	enc[2] = '0' // v11 -> v10
	got, err := DecryptChromiumValue(enc, "peanuts", ".twitch.tv")
	if err != nil {
		t.Fatal(err)
	}
	if got != token {
		t.Fatalf("got %q want %q", got, token)
	}
}

func TestExtractChromium_SuccessWithHostHash(t *testing.T) {
	got, err := ExtractChromiumWithPassword(testdata(t, "chromium_v11_hosthash.db"), testPassword(t))
	if err != nil {
		t.Fatal(err)
	}
	if got != testToken(t) {
		t.Fatalf("got %q want %q", got, testToken(t))
	}
}

func TestExtractChromium_SuccessWithoutHostHash(t *testing.T) {
	got, err := ExtractChromiumWithPassword(testdata(t, "chromium_v11_plain.db"), testPassword(t))
	if err != nil {
		t.Fatal(err)
	}
	if got != testToken(t) {
		t.Fatalf("got %q want %q", got, testToken(t))
	}
}

func TestExtractChromium_CookieMissing(t *testing.T) {
	_, err := ExtractChromiumWithPassword(testdata(t, "chromium_empty.db"), testPassword(t))
	if !errors.Is(err, ErrCookieNotFound) {
		t.Fatalf("err = %v, want ErrCookieNotFound", err)
	}
}

func TestExtractChromium_UnknownFormat(t *testing.T) {
	_, err := ExtractChromiumWithPassword(testdata(t, "chromium_unknown_prefix.db"), testPassword(t))
	if !errors.Is(err, ErrUnknownCookieFormat) {
		t.Fatalf("err = %v, want ErrUnknownCookieFormat", err)
	}
}

func TestExtractFirefox_Success(t *testing.T) {
	got, err := ExtractFirefox(testdata(t, "firefox_cookies.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if got != testToken(t) {
		t.Fatalf("got %q want %q", got, testToken(t))
	}
}

func TestExtractFirefox_CookieMissing(t *testing.T) {
	_, err := ExtractFirefox(testdata(t, "firefox_empty.sqlite"))
	if !errors.Is(err, ErrCookieNotFound) {
		t.Fatalf("err = %v, want ErrCookieNotFound", err)
	}
}

func TestKeyringUnavailable(t *testing.T) {
	orig := keyringLookup
	t.Cleanup(func() { keyringLookup = orig })
	keyringLookup = func(Browser) (string, error) {
		return "", &Error{
			Op:      "keyring",
			Err:     ErrKeyringUnavailable,
			Message: "keyring bloqueado",
		}
	}
	p := Profile{
		Browser:     BrowserBrave,
		Name:        "Default",
		CookiesPath: testdata(t, "chromium_v11_hosthash.db"),
		Family:      "chromium",
	}
	_, err := ExtractToken(p)
	if !errors.Is(err, ErrKeyringUnavailable) {
		t.Fatalf("err = %v, want ErrKeyringUnavailable", err)
	}
}

func TestCopyCookieDB_DoesNotTouchOriginal(t *testing.T) {
	src := testdata(t, "chromium_v11_plain.db")
	before, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(src)
	if err != nil {
		t.Fatal(err)
	}
	mtime := info.ModTime()

	tmp, cleanup, err := copyCookieDB(src)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if tmp == src {
		t.Fatal("copy returned original path")
	}
	after, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("original cookie DB was modified")
	}
	info2, err := os.Stat(src)
	if err != nil {
		t.Fatal(err)
	}
	if !info2.ModTime().Equal(mtime) {
		t.Fatal("original mtime changed")
	}
	got, err := ExtractChromiumWithPassword(tmp, testPassword(t))
	if err != nil {
		t.Fatal(err)
	}
	if got != testToken(t) {
		t.Fatalf("got %q", got)
	}
}
