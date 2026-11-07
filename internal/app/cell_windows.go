package app

import "os"

// cellSize has no source on Windows, where the sixel backend does not run.
func cellSize(*os.File) (width, height int) { return 0, 0 }
