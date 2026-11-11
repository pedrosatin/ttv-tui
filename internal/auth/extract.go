package auth

import "fmt"

// ExtractToken copies the profile cookie DB, extracts the twitch.tv auth-token,
// and returns the plaintext token. For a Chromium-family v11 cookie it reads the
// Safe Storage secret from the keyring; a v10 cookie uses Chromium's fixed Linux
// password and never touches the keyring. The caller must not log or print the token.
func ExtractToken(p Profile) (string, error) {
	switch p.Family {
	case "firefox":
		return extractFirefoxAuthToken(p.CookiesPath)
	case "chromium":
		return extractChromiumAuthToken(p.CookiesPath, func() (string, error) {
			return LookupKeyringSecret(p.Browser)
		})
	default:
		return "", &Error{
			Op:      "extract",
			Err:     ErrBrowserNotFound,
			Message: fmt.Sprintf("família de navegador %q não suportada", p.Family),
		}
	}
}

// ExtractChromiumWithPassword decrypts auth-token from cookiesPath using an
// explicit Safe Storage password for a v11 cookie. Used by tests and by callers
// that already resolved the keyring secret.
func ExtractChromiumWithPassword(cookiesPath, password string) (string, error) {
	return extractChromiumAuthToken(cookiesPath, func() (string, error) { return password, nil })
}

// ExtractFirefox reads auth-token from a Firefox cookies.sqlite path.
func ExtractFirefox(cookiesPath string) (string, error) {
	return extractFirefoxAuthToken(cookiesPath)
}
