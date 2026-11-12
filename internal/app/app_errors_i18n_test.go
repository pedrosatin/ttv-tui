package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Errors raised before the config is read take their language from the
// environment and still come from the catalog.
func TestRunUnsupportedLangErrorIsLocalized(t *testing.T) {
	cases := []struct {
		locale string
		want   string
	}{
		{"pt_BR.UTF-8", "não suportada"},
		{"en_US.UTF-8", "unsupported --lang"},
	}
	for _, tc := range cases {
		t.Run(tc.locale, func(t *testing.T) {
			t.Setenv("LC_ALL", tc.locale)
			err := Run(Options{Lang: "fr"})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Run error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestRunInvalidConfigExplainsRecoveryAndHidesToken(t *testing.T) {
	const secret = "segredo-de-teste"
	invalidContent := "access_token = \"" + secret + "\"\n[broken section\n"

	cases := []struct {
		locale       string
		wantPrefix   string
		wantRecovery string
		wantLoginCmd string
	}{
		{
			locale:       "pt_BR.UTF-8",
			wantPrefix:   "carregar configuração:",
			wantRecovery: "mova o arquivo para o lado com 'mv ",
			wantLoginCmd: "rode 'ttv-tui login' de novo",
		},
		{
			locale:       "en_US.UTF-8",
			wantPrefix:   "load config:",
			wantRecovery: "move the file aside with 'mv ",
			wantLoginCmd: "run 'ttv-tui login' again",
		},
	}

	for _, tc := range cases {
		t.Run(tc.locale, func(t *testing.T) {
			t.Setenv("LC_ALL", tc.locale)
			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "config.toml")
			if err := os.WriteFile(cfgPath, []byte(invalidContent), 0o600); err != nil {
				t.Fatalf("write invalid config: %v", err)
			}

			err := Run(Options{ConfigPath: cfgPath})
			if err == nil {
				t.Fatal("Run with invalid config must return an error")
			}
			msg := err.Error()

			if strings.Contains(msg, secret) {
				t.Fatalf("SECURITY VIOLATION: secret token %q leaked in error message: %q", secret, msg)
			}
			if !strings.Contains(msg, tc.wantPrefix) {
				t.Fatalf("error %q does not contain prefix %q", msg, tc.wantPrefix)
			}
			if !strings.Contains(msg, tc.wantRecovery) {
				t.Fatalf("error %q does not explain recovery %q", msg, tc.wantRecovery)
			}
			wantBakCmd := fmt.Sprintf("'mv %q %q'", cfgPath, cfgPath+".bak")
			if !strings.Contains(msg, wantBakCmd) {
				t.Fatalf("error %q does not contain backup command %q", msg, wantBakCmd)
			}
			if !strings.Contains(msg, tc.wantLoginCmd) {
				t.Fatalf("error %q does not instruct to rerun login: %q", msg, tc.wantLoginCmd)
			}
		})
	}

	// Also verify default config path via XDG_CONFIG_HOME.
	t.Run("default path from environment", func(t *testing.T) {
		t.Setenv("LC_ALL", "pt_BR.UTF-8")
		xdg := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", xdg)
		cfgDir := filepath.Join(xdg, "ttv-tui")
		if err := os.MkdirAll(cfgDir, 0o700); err != nil {
			t.Fatal(err)
		}
		cfgPath := filepath.Join(cfgDir, "config.toml")
		if err := os.WriteFile(cfgPath, []byte(invalidContent), 0o600); err != nil {
			t.Fatal(err)
		}

		err := Run(Options{})
		if err == nil {
			t.Fatal("Run with invalid default config must return an error")
		}
		msg := err.Error()
		if strings.Contains(msg, secret) {
			t.Fatalf("SECURITY VIOLATION: secret token %q leaked in error message: %q", secret, msg)
		}
		wantBakCmd := fmt.Sprintf("'mv %q %q'", cfgPath, cfgPath+".bak")
		if !strings.Contains(msg, wantBakCmd) {
			t.Fatalf("error %q does not contain backup command with default path %q", msg, wantBakCmd)
		}
	})
}

// The token line itself is malformed: the error names the key, not the value.
func TestRunUnterminatedTokenDoesNotLeak(t *testing.T) {
	const secret = "segredo-de-teste"
	t.Setenv("LC_ALL", "en_US.UTF-8")
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, []byte("access_token = \""+secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := Run(Options{ConfigPath: cfgPath})
	if err == nil || strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "'mv ") {
		t.Fatalf("Run error = %v, want the recovery hint without the token", err)
	}
}

// Moving the file aside does not fix a read error or a missing home directory,
// so those errors carry no recovery hint.
func TestRunConfigErrorsWithoutRecoveryHint(t *testing.T) {
	t.Setenv("LC_ALL", "en_US.UTF-8")
	t.Run("path is a directory", func(t *testing.T) {
		err := Run(Options{ConfigPath: t.TempDir()})
		if err == nil || strings.Contains(err.Error(), "'mv ") {
			t.Fatalf("Run error = %v, want a read error without the hint", err)
		}
	})
	t.Run("no home directory", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("HOME", "")
		err := Run(Options{})
		if err == nil || strings.Contains(err.Error(), "'mv ") {
			t.Fatalf("Run error = %v, want a path error without the hint", err)
		}
	})
}
