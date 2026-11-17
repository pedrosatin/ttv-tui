//go:build !unix

package config

import "os"

// Fora do Unix, Secure não altera permissões; estes valores só satisfazem a compilação.
const openNoFollow = 0

func ownedByCurrentUser(os.FileInfo) bool { return false }
