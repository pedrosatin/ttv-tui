package twitch

import (
	"errors"
	"fmt"
	"time"
)

// ErrorKind classifies errors produced by a Source implementation.
type ErrorKind string

const (
	Rede      ErrorKind = "Rede"
	RateLimit ErrorKind = "RateLimit"
	Servidor  ErrorKind = "Servidor"
	Contrato  ErrorKind = "Contrato"
)

// Error is the typed error returned by Source operations, carrying an ErrorKind
// classification and optional HTTP / rate-limit metadata.
type Error struct {
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
	prefix := fmt.Sprintf("twitch %s", e.Kind)
	if e.Op != "" {
		prefix = fmt.Sprintf("twitch %s [%s]", e.Op, e.Kind)
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

// KindOf extracts the ErrorKind from err if it wraps a *twitch.Error.
// If err does not wrap a *twitch.Error, it returns an empty ErrorKind.
func KindOf(err error) ErrorKind {
	var te *Error
	if errors.As(err, &te) && te != nil {
		return te.Kind
	}
	return ""
}
