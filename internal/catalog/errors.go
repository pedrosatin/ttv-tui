package catalog

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

// ErrorKind classifies errors produced by a Source or platform adapter.
type ErrorKind string

const (
	Rede      ErrorKind = "Rede"
	RateLimit ErrorKind = "RateLimit"
	Servidor  ErrorKind = "Servidor"
	Contrato  ErrorKind = "Contrato"
	// Bloqueio is a request the platform refused to a client that is not a
	// browser (HTTP 403 from an anti-bot wall). Trying again right away gets the
	// same answer, and nothing in the response is the platform's own data.
	Bloqueio ErrorKind = "Bloqueio"
)

// Error is the typed error returned by catalog or source operations,
// carrying an ErrorKind classification, an optional platform, and HTTP / rate-limit metadata.
type Error struct {
	Platform   string
	Kind       ErrorKind
	Op         string
	StatusCode int
	ResetAfter time.Duration
	ResetAt    time.Time
	Err        error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	plat := e.Platform
	if plat == "" {
		plat = "twitch"
	}
	prefix := fmt.Sprintf("%s %s", plat, e.Kind)
	if e.Op != "" {
		prefix = fmt.Sprintf("%s %s [%s]", plat, e.Op, e.Kind)
	}
	if e.StatusCode > 0 {
		prefix = fmt.Sprintf("%s (HTTP %d)", prefix, e.StatusCode)
	}
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", prefix, e.Err)
	}
	return prefix
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// KindOf extracts the ErrorKind from err if it wraps a *Error.
// If err does not wrap a *Error, it returns an empty ErrorKind.
func KindOf(err error) ErrorKind {
	var ce *Error
	if errors.As(err, &ce) && ce != nil {
		return ce.Kind
	}
	return ""
}

// HTTPStatusKind classifies the HTTP status of a response the caller already
// treats as a failure: 429 is RateLimit, 5xx is Servidor, and any other status
// is Contrato (the platform answered something the adapter did not expect).
// Network failures with no response are Rede and never reach this function.
func HTTPStatusKind(status int) ErrorKind {
	switch {
	case status == http.StatusTooManyRequests:
		return RateLimit
	case status >= http.StatusInternalServerError:
		return Servidor
	default:
		return Contrato
	}
}
