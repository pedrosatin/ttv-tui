package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// resolveTUI procura a TUI em caminhos absolutos; nunca consulta o PATH.
func resolveTUI(getenv func(string) string, executable, home func() (string, error), stat func(string) (os.FileInfo, error)) (string, error) {
	pt := false
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if locale := strings.TrimSpace(getenv(key)); locale != "" {
			pt = strings.HasPrefix(strings.ToLower(locale), "pt")
			break
		}
	}
	message := func(en, br string) string {
		if pt {
			return br
		}
		return en
	}
	validate := func(path string) error {
		if !filepath.IsAbs(path) {
			return fmt.Errorf("%s", message("path must be absolute", "o caminho deve ser absoluto"))
		}
		if exeSuffix != "" && !strings.HasSuffix(strings.ToLower(path), exeSuffix) {
			return fmt.Errorf("%s", message("path must end in .exe", "o caminho deve terminar em .exe"))
		}
		info, err := stat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || !isExecutable(info) {
			return fmt.Errorf("%s", message("path must be a regular executable file", "o caminho deve ser um arquivo regular executável"))
		}
		return nil
	}
	if path := getenv("TTV_WINDOW_TUI"); path != "" {
		if err := validate(path); err != nil {
			return "", fmt.Errorf("TTV_WINDOW_TUI=%q: %w", path, err)
		}
		return path, nil
	}
	var candidates []string
	if exe, err := executable(); err == nil && filepath.IsAbs(exe) {
		dirs := []string{filepath.Dir(exe)}
		if resolved, err := filepath.EvalSymlinks(exe); err == nil && filepath.Dir(resolved) != dirs[0] {
			dirs = append(dirs, filepath.Dir(resolved))
		}
		// O irmão do link tem prioridade sobre o irmão do destino.
		for _, dir := range dirs {
			candidates = append(candidates, filepath.Join(dir, "ttv-tui"+exeSuffix))
		}
		for _, dir := range dirs {
			if filepath.Base(dir) == "ttv-tui-window" {
				candidates = append(candidates, filepath.Join(filepath.Dir(dir), "ttv-tui"+exeSuffix))
			}
		}
	}
	if dir, err := home(); err == nil && filepath.IsAbs(dir) {
		candidates = append(candidates, filepath.Join(dir, ".local", "bin", "ttv-tui"+exeSuffix))
	}
	for _, path := range candidates {
		if validate(path) == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf(message("ttv-tui not found in %s; put ttv-tui next to ttv-tui-window or set TTV_WINDOW_TUI to its absolute path", "ttv-tui não encontrado em %s. Instale o ttv-tui ao lado do ttv-tui-window ou defina TTV_WINDOW_TUI com o caminho absoluto do executável."), strings.Join(candidates, ", "))
}
