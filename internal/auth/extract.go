package auth

import "fmt"

// ExtractToken copies the profile cookie DB, extracts the twitch.tv auth-token,
// and returns the plaintext token. For Chromium-family profiles it reads the
// Safe Storage secret from the keyring. The caller must not log or print the token.
func ExtractToken(p Profile) (string, error) {
	switch p.Family {
	case "firefox":
		return extractFirefoxAuthToken(p.CookiesPath)
	case "chromium":
		secret, err := LookupKeyringSecret(p.Browser)
		if err != nil {
			return "", err
		}
		return extractChromiumAuthToken(p.CookiesPath, secret)
	default:
		return "", &Error{
			Op:      "extract",
			Err:     ErrBrowserNotFound,
			Message: fmt.Sprintf("família de navegador %q não suportada", p.Family),
		}
	}
}

// ExtractTokenFrom resolves browser/profile names and extracts the token.
// profile may be empty to pick the first matching profile for that browser.
func ExtractTokenFrom(browser, profile string) (string, error) {
	p, err := FindProfile(browser, profile)
	if err != nil {
		return "", err
	}
	return ExtractToken(p)
}

// ExtractChromiumWithPassword decrypts auth-token from cookiesPath using an
// explicit Safe Storage password. Used by tests and by callers that already
// resolved the keyring secret.
func ExtractChromiumWithPassword(cookiesPath, password string) (string, error) {
	return extractChromiumAuthToken(cookiesPath, password)
}

// ExtractFirefox reads auth-token from a Firefox cookies.sqlite path.
func ExtractFirefox(cookiesPath string) (string, error) {
	return extractFirefoxAuthToken(cookiesPath)
}
