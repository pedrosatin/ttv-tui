package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	defaultValidateURL     = "https://id.twitch.tv/oauth2/validate"
	defaultValidateTimeout = 10 * time.Second
)

// Validation is the non-secret subset of a successful OAuth validate response.
type Validation struct {
	ClientID  string   `json:"client_id"`
	Login     string   `json:"login"`
	Scopes    []string `json:"scopes"`
	UserID    string   `json:"user_id"`
	ExpiresIn int      `json:"expires_in"`
}

// ValidateClient performs token validation. HTTPClient and URL are overridable for tests.
type ValidateClient struct {
	HTTPClient *http.Client
	URL        string
}

// Validate checks token against id.twitch.tv/oauth2/validate.
// The token itself is never included in returned errors.
func Validate(ctx context.Context, token string) (Validation, error) {
	return (ValidateClient{}).Validate(ctx, token)
}

// Validate implements token validation with optional test overrides.
func (c ValidateClient) Validate(ctx context.Context, token string) (Validation, error) {
	token = strings.TrimSpace(token)
	token = strings.TrimPrefix(token, "oauth:")
	if token == "" {
		return Validation{}, &Error{
			Op:      "validate",
			Err:     ErrTokenInvalid,
			Message: "token vazio; colha de novo com login --from-browser",
		}
	}

	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultValidateTimeout}
	}
	url := c.URL
	if url == "" {
		url = defaultValidateURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Validation{}, wrap("validate", err, "falha ao montar requisição de validação")
	}
	req.Header.Set("Authorization", "OAuth "+token)

	resp, err := client.Do(req)
	if err != nil {
		if isTimeoutErr(err) || errors.Is(err, context.DeadlineExceeded) {
			return Validation{}, &Error{
				Op:      "validate",
				Err:     ErrValidateTimeout,
				Message: "timeout ao falar com id.twitch.tv; tente de novo quando a rede responder",
			}
		}
		return Validation{}, wrap("validate", err, "falha de rede ao validar token")
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Validation{}, wrap("validate", err, "falha ao ler resposta de validação")
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var v Validation
		if err := json.Unmarshal(body, &v); err != nil {
			return Validation{}, wrap("validate", err, "resposta de validação inválida")
		}
		if v.Login == "" {
			return Validation{}, &Error{
				Op:      "validate",
				Err:     errors.New("resposta de validação sem login"),
				Message: "resposta de validação sem login",
			}
		}
		return v, nil
	case http.StatusUnauthorized:
		return Validation{}, &Error{
			Op:      "validate",
			Err:     ErrTokenInvalid,
			Message: "token rejeitado pelo Twitch (401); faça login no navegador e colha de novo",
		}
	default:
		return Validation{}, &Error{
			Op:      "validate",
			Err:     fmt.Errorf("HTTP %d", resp.StatusCode),
			Message: fmt.Sprintf("validação retornou HTTP %d", resp.StatusCode),
		}
	}
}

func isTimeoutErr(err error) bool {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded")
}
