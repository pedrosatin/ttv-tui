package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// PermissionNotice registra ajustes e falhas de chmod sem interromper o acesso ao config.
// Path, quando preenchido, é o alvo do symlink do config e substitui o caminho no aviso.
type PermissionNotice struct {
	Fixed  bool
	Failed bool
	Path   string
}

// chmodFunc aplica o modo pelo descritor já aberto e conferido.
type chmodFunc func(*os.File, os.FileMode) error

func fchmod(f *os.File, mode os.FileMode) error { return f.Chmod(mode) }

var errFileChanged = errors.New("config path changed while securing it")

// Secure restringe o arquivo do config e o diretório padrão. Se o config é um
// symlink, só restringe o alvo quando ele é arquivo regular do usuário atual.
// Caminhos ausentes são ignorados; falhas de chmod ficam no aviso.
func Secure(path string) (PermissionNotice, error) {
	return tightenPermissions(path, fchmod)
}

// tightenPermissions restringe o arquivo e somente o diretório padrão.
// Falhas de stat do caminho são fatais; falhas de chmod ficam no aviso retornado.
func tightenPermissions(path string, chmod chmodFunc) (PermissionNotice, error) {
	var notice PermissionNotice
	if runtime.GOOS == "windows" {
		return notice, nil
	}
	if err := tightenFile(path, chmod, &notice); err != nil {
		return notice, err
	}
	if err := tightenConfigDir(filepath.Dir(path), chmod, &notice); err != nil {
		return notice, err
	}
	return notice, nil
}

// tightenFile restringe o config para 0600. Um symlink é resolvido e o alvo só
// recebe chmod se for arquivo regular do usuário atual; caso contrário o aviso
// registra a falha com o caminho do alvo.
func tightenFile(path string, chmod chmodFunc, notice *PermissionNotice) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat config permissions: %w", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		if info.Mode().Perm()&0o077 != 0 {
			record(notice, restrict(path, info, 0o600, chmod))
		}
		return nil
	}
	target, err := filepath.EvalSymlinks(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		notice.Failed = true
		notice.Path = path
		return nil
	}
	if abs, err := filepath.Abs(target); err == nil {
		target = abs
	}
	info, err = os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() || !ownedByCurrentUser(info) {
		notice.Failed = true
		notice.Path = target
		return nil
	}
	if info.Mode().Perm()&0o077 == 0 {
		return nil
	}
	notice.Path = target
	record(notice, restrict(target, info, 0o600, chmod))
	return nil
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
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat config permissions: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 == 0 {
		return nil
	}
	record(notice, restrict(dir, info, 0o700, chmod))
	return nil
}

func record(notice *PermissionNotice, err error) {
	if err != nil {
		notice.Failed = true
	} else {
		notice.Fixed = true
	}
}

// restrict abre name sem seguir symlink, confere que é o mesmo objeto visto no
// Lstat e aplica o modo pelo descritor, fechando a janela entre stat e chmod.
func restrict(name string, want os.FileInfo, mode os.FileMode, chmod chmodFunc) error {
	f, err := os.OpenFile(name, os.O_RDONLY|openNoFollow, 0)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	got, err := f.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(want, got) {
		return errFileChanged
	}
	return chmod(f, mode)
}
