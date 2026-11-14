package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// PermissionNotice registra ajustes e falhas de chmod sem interromper o acesso ao config.
// Cada componente que recebe chmod é verificado com Lstat: symlinks são ignorados,
// mas um arquivo regular dentro de um diretório-symlink pode ser restringido.
type PermissionNotice struct {
	Fixed  bool
	Failed bool
}

type chmodFunc func(string, os.FileMode) error

// Secure restringe o arquivo regular e o diretório padrão, sem alterar symlinks.
// Caminhos ausentes são ignorados; falhas de chmod ficam no aviso.
func Secure(path string) (PermissionNotice, error) {
	return tightenPermissions(path, os.Chmod)
}

// tightenPermissions restringe o arquivo regular e somente o diretório padrão.
// Falhas de stat são fatais; falhas de chmod ficam no aviso retornado.
func tightenPermissions(path string, chmod chmodFunc) (PermissionNotice, error) {
	var notice PermissionNotice
	if runtime.GOOS == "windows" {
		return notice, nil
	}
	dir := filepath.Dir(path)
	if err := tightenMode(path, 0o600, chmod, &notice); err != nil {
		return notice, err
	}
	if err := tightenConfigDir(dir, chmod, &notice); err != nil {
		return notice, err
	}
	return notice, nil
}

// tightenConfigDir só restringe o diretório de DefaultPath, sem seguir symlinks.
func tightenConfigDir(dir string, chmod chmodFunc, notice *PermissionNotice) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	path, err := DefaultPath()
	if err != nil {
		return nil
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve config directory: %w", err)
	}
	standard, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("resolve default config directory: %w", err)
	}
	if absolute != standard {
		return nil
	}
	return tightenMode(dir, 0o700, chmod, notice)
}

func tightenMode(path string, mode os.FileMode, chmod chmodFunc, notice *PermissionNotice) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat config permissions: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 == 0 {
		return nil
	}
	if err := chmod(path, mode); err != nil {
		notice.Failed = true
	} else {
		notice.Fixed = true
	}
	return nil
}
