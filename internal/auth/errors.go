package auth

import (
	"errors"
	"fmt"
)

// Sentinel errors for browser-token extraction and validation.
var (
	ErrBrowserNotFound     = errors.New("navegador não encontrado")
	ErrProfileNotFound     = errors.New("perfil não encontrado")
	ErrCookieNotFound      = errors.New("cookie auth-token do twitch.tv ausente neste perfil")
	ErrUnknownCookieFormat = errors.New("formato de cookie desconhecido")
	ErrKeyringUnavailable  = errors.New("keyring indisponível ou bloqueado")
	ErrTokenInvalid        = errors.New("token inválido ou expirado")
	ErrValidateTimeout     = errors.New("timeout ao validar token")
	ErrSQLiteUnavailable   = errors.New("sqlite3 não encontrado no PATH")
)

// Error is a typed auth error with a stable sentinel and human-readable next step.
type Error struct {
	Op      string
	Err     error
	Message string // user-facing guidance; never contains the token
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	msg := e.Message
	if msg == "" && e.Err != nil {
		msg = e.Err.Error()
	}
	if msg == "" {
		msg = "erro de autenticação"
	}
	if e.Op == "" {
		return msg
	}
	return fmt.Sprintf("auth %s: %s", e.Op, msg)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func wrap(op string, err error, message string) error {
	if err == nil {
		return nil
	}
	var ae *Error
	if errors.As(err, &ae) {
		if ae.Op == "" {
			ae.Op = op
		}
		if message != "" && ae.Message == "" {
			ae.Message = message
		}
		return ae
	}
	return &Error{Op: op, Err: err, Message: message}
}
