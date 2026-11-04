package auth

import (
	"strings"
)

// extractFirefoxAuthToken reads the plaintext auth-token cookie from a Firefox cookies.sqlite.
func extractFirefoxAuthToken(cookiesPath string) (string, error) {
	tmpDB, cleanup, err := copyCookieDB(cookiesPath)
	if err != nil {
		return "", err
	}
	defer cleanup()

	sql := `SELECT value FROM moz_cookies ` +
		`WHERE name='auth-token' AND (host='.twitch.tv' OR host='twitch.tv') ` +
		`LIMIT 1;`
	out, err := runSQLite(tmpDB, sql)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(out)
	if token == "" {
		return "", &Error{
			Op:      "extract",
			Err:     ErrCookieNotFound,
			Message: "perfil sem cookie auth-token do twitch.tv; abra o twitch.tv neste perfil e faça login",
		}
	}
	return token, nil
}
