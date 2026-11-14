package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

type tuiFile struct{ mode os.FileMode }

func (f tuiFile) Name() string       { return "ttv-tui" }
func (f tuiFile) Size() int64        { return 0 }
func (f tuiFile) Mode() os.FileMode  { return f.mode }
func (f tuiFile) ModTime() time.Time { return time.Time{} }
func (f tuiFile) IsDir() bool        { return f.mode.IsDir() }
func (f tuiFile) Sys() any           { return nil }

func TestResolveTUI(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "ttv-tui-window", "ttv-tui-window"+exeSuffix)
	sibling := filepath.Join(filepath.Dir(exe), "ttv-tui"+exeSuffix)
	parent := filepath.Join(root, "ttv-tui"+exeSuffix)
	homePath := filepath.Join(root, "home", ".local", "bin", "ttv-tui"+exeSuffix)
	for _, tc := range []struct {
		name, override, home, want, locale, reason string
		files                                      map[string]os.FileMode
		exeError                                   bool
	}{
		{name: "home vazio", reason: "not found"},
		{name: "override relativo", override: "./ttv-tui" + exeSuffix, files: map[string]os.FileMode{sibling: 0755}, reason: "absolute"},
		{name: "override relativo PT", override: "ttv-tui", locale: "pt_BR.UTF-8", reason: "absoluto"},
		{name: "layout install.ps1", files: map[string]os.FileMode{parent: 0755}, want: parent},
		{name: "irmão antes do pai", files: map[string]os.FileMode{sibling: 0755, parent: 0755}, want: sibling},
		{name: "override antes do irmão", override: parent, files: map[string]os.FileMode{sibling: 0755, parent: 0755}, want: parent},
		{name: "home", home: filepath.Join(root, "home"), files: map[string]os.FileMode{homePath: 0755}, want: homePath},
		{name: "home relativo", home: "relative", reason: "not found"},
		{name: "executável indisponível", exeError: true, reason: "not found"},
		{name: "diretório recusado", files: map[string]os.FileMode{sibling: os.ModeDir | 0755}, reason: "not found"},
		{name: "pipe recusado", files: map[string]os.FileMode{sibling: os.ModeNamedPipe | 0755}, reason: "not found"},
		{name: "override ausente", override: parent, reason: "TTV_WINDOW_TUI"},
		{name: "não encontrado PT", locale: "pt-BR", reason: "não encontrado"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			getenv := func(key string) string {
				if key == "TTV_WINDOW_TUI" {
					return tc.override
				}
				if key == "LANG" {
					return tc.locale
				}
				return ""
			}
			stat := func(path string) (os.FileInfo, error) {
				if !filepath.IsAbs(path) {
					t.Fatalf("stat relativo: %s", path)
				}
				if mode, ok := tc.files[path]; ok {
					return tuiFile{mode}, nil
				}
				return nil, os.ErrNotExist
			}
			got, err := resolveTUI(getenv, func() (string, error) {
				if tc.exeError {
					return "", errors.New("indisponível")
				}
				return exe, nil
			}, func() (string, error) { return tc.home, nil }, stat)
			if tc.reason != "" {
				if err == nil || !strings.Contains(err.Error(), tc.reason) || got != "" {
					t.Fatalf("got %q, %v; motivo %q", got, err, tc.reason)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestResolveTUIRealPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("O teste precisa criar links simbólicos e conferir permissões Unix")
	}
	// No macOS o TempDir fica sob /tmp, que é link para /private/tmp; o
	// resolvedor devolve o destino do link, então a raiz já vem resolvida.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	raw := filepath.Join(root, "links", "ttv-tui-window")
	target := filepath.Join(root, "destino", "ttv-tui-window")
	for _, dir := range []string{filepath.Dir(raw), filepath.Dir(target)} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path string, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(target, 0755)
	if err := os.Symlink(target, raw); err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(filepath.Dir(raw), "ttv-tui")
	resolvedSibling := filepath.Join(filepath.Dir(target), "ttv-tui")
	write(resolvedSibling, 0755)
	run := func(want string) {
		t.Helper()
		got, err := resolveTUI(func(string) string { return "" }, func() (string, error) { return raw, nil }, func() (string, error) { return "", os.ErrNotExist }, os.Stat)
		if err != nil || got != want {
			t.Fatalf("got %q, %v; want %q", got, err, want)
		}
	}
	t.Run("irmão do destino", func(t *testing.T) { run(resolvedSibling) })
	write(sibling, 0755)
	t.Run("irmão do link primeiro", func(t *testing.T) { run(sibling) })
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	t.Run("link quebrado usa caminho cru", func(t *testing.T) { run(sibling) })
	if err := os.Remove(sibling); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(resolvedSibling, sibling); err != nil {
		t.Fatal(err)
	}
	t.Run("TUI que é link", func(t *testing.T) { run(sibling) })
}

func TestResolveTUIRejectsCheckoutParent(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "window", "ttv-tui-window"+exeSuffix)
	parent := filepath.Join(root, "ttv-tui"+exeSuffix)
	var tried []string
	_, err := resolveTUI(func(string) string { return "" }, func() (string, error) { return exe, nil }, func() (string, error) { return "", os.ErrNotExist }, func(path string) (os.FileInfo, error) {
		tried = append(tried, path)
		if path == parent {
			return tuiFile{0755}, nil
		}
		return nil, os.ErrNotExist
	})
	if err == nil || strings.Contains(strings.Join(tried, ","), parent) {
		t.Fatalf("pai aceito: %v, %v", tried, err)
	}
	want := "ttv-tui not found in " + filepath.Join(filepath.Dir(exe), "ttv-tui"+exeSuffix) + "; put ttv-tui next to ttv-tui-window or set TTV_WINDOW_TUI to its absolute path"
	if err.Error() != want {
		t.Fatalf("erro: %v; want %s", err, want)
	}
}

func TestResolveTUIRejectsNonExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("No Windows, a validação confere só a extensão .exe")
	}
	path := filepath.Join(t.TempDir(), "ttv-tui")
	if err := os.WriteFile(path, []byte("teste"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := resolveTUI(func(key string) string {
		if key == "TTV_WINDOW_TUI" {
			return path
		}
		return ""
	}, func() (string, error) { t.Fatal("consultou executável com override"); return "", nil }, func() (string, error) { t.Fatal("consultou home com override"); return "", nil }, os.Stat)
	if err == nil || !strings.Contains(err.Error(), "regular executable") {
		t.Fatalf("erro: %v", err)
	}
}

func TestResolveTUILocalePrecedence(t *testing.T) {
	_, err := resolveTUI(func(key string) string {
		switch key {
		case "LC_ALL":
			return "en_US.UTF-8"
		case "LC_MESSAGES", "LANG":
			return "pt_BR.UTF-8"
		case "TTV_WINDOW_TUI":
			return "relative"
		}
		return ""
	}, func() (string, error) { return "", os.ErrNotExist }, func() (string, error) { return "", os.ErrNotExist }, func(string) (os.FileInfo, error) { t.Fatal("consultou caminho relativo"); return nil, nil })
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("erro: %v", err)
	}
}

func TestResolveTUIMessages(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "window", "ttv-tui-window"+exeSuffix)
	candidate := filepath.Join(filepath.Dir(exe), "ttv-tui"+exeSuffix)
	home := filepath.Join(root, "home")
	list := candidate + ", " + filepath.Join(home, ".local", "bin", "ttv-tui"+exeSuffix)
	for _, tc := range []struct{ locale, override, want string }{
		{"en", "", "ttv-tui not found in " + list + "; put ttv-tui next to ttv-tui-window or set TTV_WINDOW_TUI to its absolute path"},
		{"pt_BR", "", "ttv-tui não encontrado em " + list + ". Instale o ttv-tui ao lado do ttv-tui-window ou defina TTV_WINDOW_TUI com o caminho absoluto do executável."},
		{"en", candidate, "TTV_WINDOW_TUI=" + strconv.Quote(candidate) + ": " + (&os.PathError{Op: "stat", Path: candidate, Err: os.ErrNotExist}).Error()},
		{"en", "relative", "TTV_WINDOW_TUI=\"relative\": path must be absolute"},
	} {
		t.Run(tc.locale+tc.override, func(t *testing.T) {
			_, err := resolveTUI(func(key string) string {
				switch key {
				case "LANG":
					return tc.locale
				case "TTV_WINDOW_TUI":
					return tc.override
				}
				return ""
			}, func() (string, error) { return exe, nil }, func() (string, error) { return home, nil }, func(path string) (os.FileInfo, error) {
				return nil, &os.PathError{Op: "stat", Path: path, Err: os.ErrNotExist}
			})
			if err == nil || err.Error() != tc.want {
				t.Fatalf("got %v; want %s", err, tc.want)
			}
		})
	}
}
