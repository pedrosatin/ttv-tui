package config

import (
	"sync"

	"github.com/pedrosatin/ttv-tui/internal/filelock"
)

// Serializa transações locais no config para goroutines do mesmo processo não
// disputarem entre si as tentativas limitadas do lock em disco.
var configFileMu sync.Mutex

func lockConfig(path string) (func() error, error) {
	configFileMu.Lock()
	release, err := filelock.Lock(path)
	if err != nil {
		configFileMu.Unlock()
		return nil, err
	}
	return func() error {
		defer configFileMu.Unlock()
		return release()
	}, nil
}
