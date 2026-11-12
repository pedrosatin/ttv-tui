package twitch

import (
	"github.com/pedrosatin/ttv-tui/internal/catalog"
)

// ErrorKind is an alias for catalog.ErrorKind.
type ErrorKind = catalog.ErrorKind

const (
	Rede      ErrorKind = catalog.Rede
	RateLimit ErrorKind = catalog.RateLimit
	Servidor  ErrorKind = catalog.Servidor
	Contrato  ErrorKind = catalog.Contrato
)

// Error is an alias for catalog.Error.
type Error = catalog.Error

// KindOf extracts the ErrorKind from err if it wraps a *catalog.Error.
func KindOf(err error) ErrorKind {
	return catalog.KindOf(err)
}
