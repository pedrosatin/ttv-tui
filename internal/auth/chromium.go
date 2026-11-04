package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"
)

const (
	chromiumSalt       = "saltysalt"
	chromiumIV         = "                " // 16 spaces
	chromiumKeyLen     = 16                 // AES-128-CBC (Chromium Linux); ticket said 256 but DKLEN is 16
	chromiumIterations = 1
	cookiePrefixV10    = "v10"
	cookiePrefixV11    = "v11"
	hostHashLen        = 32
)

// keyringLookup is replaceable in tests.
var keyringLookup = lookupKeyringSecret

// LookupKeyringSecret reads the Chromium OSCrypt password from the desktop
// keyring via secret-tool (schema chrome_libsecret_os_crypt_password_v2).
func LookupKeyringSecret(browser Browser) (string, error) {
	return keyringLookup(browser)
}

func lookupKeyringSecret(browser Browser) (string, error) {
	spec, ok := specFor(browser)
	if !ok {
		return "", &Error{
			Op:      "keyring",
			Err:     ErrBrowserNotFound,
			Message: fmt.Sprintf("navegador %q não é da família Chromium", browser),
		}
	}
	if _, err := exec.LookPath("secret-tool"); err != nil {
		return "", &Error{
			Op:      "keyring",
			Err:     ErrKeyringUnavailable,
			Message: "secret-tool não está no PATH; instale libsecret (secret-tool) e desbloqueie o keyring da sessão",
		}
	}

	// Prefer the modern application attribute used by chrome_libsecret_os_crypt_password_v2.
	if secret, err := secretToolLookup("application", spec.KeyringApp); err == nil && secret != "" {
		return secret, nil
	}
	// Legacy service/account labels ("Brave Safe Storage" / "Brave").
	if spec.KeyringService != "" && spec.KeyringAccount != "" {
		if secret, err := secretToolLookup("service", spec.KeyringService, "account", spec.KeyringAccount); err == nil && secret != "" {
			return secret, nil
		}
	}
	return "", &Error{
		Op:  "keyring",
		Err: ErrKeyringUnavailable,
		Message: fmt.Sprintf(
			"não foi possível ler o segredo Safe Storage do %s (application=%s); desbloqueie o keyring da sessão ou faça login no navegador uma vez",
			browser, spec.KeyringApp,
		),
	}
}

func secretToolLookup(attrs ...string) (string, error) {
	args := append([]string{"lookup"}, attrs...)
	cmd := exec.Command("secret-tool", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\n\r"), nil
}

// DeriveChromiumKey derives the AES key used by Chromium Linux OSCrypt
// (PBKDF2-HMAC-SHA1, salt "saltysalt", 1 iteration, 16 bytes).
func DeriveChromiumKey(password string) ([]byte, error) {
	key, err := pbkdf2.Key(sha1.New, password, []byte(chromiumSalt), chromiumIterations, chromiumKeyLen)
	if err != nil {
		return nil, fmt.Errorf("pbkdf2: %w", err)
	}
	return key, nil
}

// DecryptChromiumValue decrypts a Chromium encrypted_value blob (v10/v11).
// On Linux both versions use AES-128-CBC with IV of 16 spaces.
// If plaintext starts with sha256(hostKey) (Chromium cookie DB >= 24 / Chrome >= 130),
// those 32 bytes are discarded.
func DecryptChromiumValue(encrypted []byte, password, hostKey string) (string, error) {
	if len(encrypted) < 3 {
		return "", &Error{
			Op:      "decrypt",
			Err:     ErrUnknownCookieFormat,
			Message: "cookie encrypted_value curto demais; o formato não é v10/v11 do Chromium",
		}
	}
	prefix := string(encrypted[:3])
	ciphertext := encrypted[3:]
	switch prefix {
	case cookiePrefixV10, cookiePrefixV11:
		// ok
	default:
		return "", &Error{
			Op:      "decrypt",
			Err:     ErrUnknownCookieFormat,
			Message: fmt.Sprintf("prefixo de cookie %q não suportado (esperado v10 ou v11)", prefix),
		}
	}
	if len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return "", &Error{
			Op:      "decrypt",
			Err:     ErrUnknownCookieFormat,
			Message: "ciphertext com tamanho inválido para AES-CBC",
		}
	}

	key, err := DeriveChromiumKey(password)
	if err != nil {
		return "", wrap("decrypt", err, "falha ao derivar chave do keyring")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", wrap("decrypt", err, "falha ao inicializar AES")
	}
	plain := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, []byte(chromiumIV)).CryptBlocks(plain, ciphertext)
	plain, err = pkcs7Unpad(plain, aes.BlockSize)
	if err != nil {
		return "", &Error{
			Op:      "decrypt",
			Err:     ErrUnknownCookieFormat,
			Message: "padding AES inválido (chave do keyring errada ou cookie corrompido?)",
		}
	}

	if hostKey != "" && len(plain) >= hostHashLen {
		sum := sha256.Sum256([]byte(hostKey))
		if string(plain[:hostHashLen]) == string(sum[:]) {
			plain = plain[hostHashLen:]
		}
	}
	return string(plain), nil
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, fmt.Errorf("invalid padded length")
	}
	pad := int(data[len(data)-1])
	if pad == 0 || pad > blockSize || pad > len(data) {
		return nil, fmt.Errorf("invalid padding size")
	}
	for i := 0; i < pad; i++ {
		if data[len(data)-1-i] != byte(pad) {
			return nil, fmt.Errorf("invalid padding bytes")
		}
	}
	return data[:len(data)-pad], nil
}

// EncryptChromiumValue encrypts plaintext the way Chromium Linux does (v11 + optional host hash).
// Intended for test fixtures.
func EncryptChromiumValue(plaintext, password, hostKey string, withHostHash bool) ([]byte, error) {
	payload := []byte(plaintext)
	if withHostHash && hostKey != "" {
		sum := sha256.Sum256([]byte(hostKey))
		payload = append(sum[:], payload...)
	}
	key, err := DeriveChromiumKey(password)
	if err != nil {
		return nil, err
	}
	padded, err := pkcs7Pad(payload, aes.BlockSize)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	ciphertext := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, []byte(chromiumIV)).CryptBlocks(ciphertext, padded)
	out := make([]byte, 0, 3+len(ciphertext))
	out = append(out, []byte(cookiePrefixV11)...)
	out = append(out, ciphertext...)
	return out, nil
}

func pkcs7Pad(data []byte, blockSize int) ([]byte, error) {
	if blockSize <= 0 || blockSize > 255 {
		return nil, fmt.Errorf("invalid block size")
	}
	pad := blockSize - (len(data) % blockSize)
	out := make([]byte, len(data)+pad)
	copy(out, data)
	for i := len(data); i < len(out); i++ {
		out[i] = byte(pad)
	}
	return out, nil
}

type chromiumCookieRow struct {
	HostKey      string
	EncryptedHex string
	Value        string
	MetaVersion  int
}

// extractChromiumAuthToken reads auth-token from a (possibly live) Cookies DB path,
// copying it to a temp dir first. password is the Safe Storage secret.
func extractChromiumAuthToken(cookiesPath, password string) (string, error) {
	tmpDB, cleanup, err := copyCookieDB(cookiesPath)
	if err != nil {
		return "", err
	}
	defer cleanup()

	row, err := queryChromiumAuthToken(tmpDB)
	if err != nil {
		return "", err
	}
	if row.Value != "" && row.EncryptedHex == "" {
		return row.Value, nil
	}
	if row.EncryptedHex == "" {
		return "", &Error{
			Op:      "extract",
			Err:     ErrCookieNotFound,
			Message: "perfil sem cookie auth-token do twitch.tv; abra o twitch.tv neste perfil e faça login",
		}
	}
	enc, err := hex.DecodeString(row.EncryptedHex)
	if err != nil {
		return "", &Error{
			Op:      "extract",
			Err:     ErrUnknownCookieFormat,
			Message: "encrypted_value não é hex válido",
		}
	}
	token, err := DecryptChromiumValue(enc, password, row.HostKey)
	if err != nil {
		return "", err
	}
	if token == "" {
		return "", &Error{
			Op:      "extract",
			Err:     ErrCookieNotFound,
			Message: "cookie auth-token decriptado veio vazio",
		}
	}
	return token, nil
}

func queryChromiumAuthToken(dbPath string) (chromiumCookieRow, error) {
	// host_key, hex(encrypted_value), plaintext value, meta version
	sql := `SELECT c.host_key, hex(c.encrypted_value), c.value, ` +
		`IFNULL((SELECT value FROM meta WHERE key='version'), '0') ` +
		`FROM cookies c ` +
		`WHERE c.name='auth-token' AND (c.host_key='.twitch.tv' OR c.host_key='twitch.tv') ` +
		`LIMIT 1;`
	out, err := runSQLite(dbPath, sql)
	if err != nil {
		return chromiumCookieRow{}, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return chromiumCookieRow{}, &Error{
			Op:      "extract",
			Err:     ErrCookieNotFound,
			Message: "perfil sem cookie auth-token do twitch.tv; abra o twitch.tv neste perfil e faça login",
		}
	}
	parts := strings.Split(out, "|")
	if len(parts) < 4 {
		return chromiumCookieRow{}, &Error{
			Op:      "extract",
			Err:     ErrUnknownCookieFormat,
			Message: "resposta inesperada do sqlite ao ler cookies",
		}
	}
	ver := 0
	_, _ = fmt.Sscanf(parts[3], "%d", &ver)
	return chromiumCookieRow{
		HostKey:      parts[0],
		EncryptedHex: parts[1],
		Value:        parts[2],
		MetaVersion:  ver,
	}, nil
}
