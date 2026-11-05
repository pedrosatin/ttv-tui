package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/auth"
	"github.com/pedrosatin/ttv-tui/internal/config"
)

func TestRunLogin_FromBrowser_SingleProfileUsedDirectly(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	secret := "single_profile_secret_token_30c"

	var out bytes.Buffer
	err := RunLogin(context.Background(), LoginOptions{
		FromBrowser: true,
		Lang:        "pt-BR",
		ConfigPath:  cfgPath,
		Input:       strings.NewReader(""), // no stdin needed when only 1 profile exists
		Output:      &out,
		ListProfiles: func() ([]auth.Profile, error) {
			return []auth.Profile{
				{Browser: auth.BrowserBrave, Name: "Default", Family: "chromium", CookiesPath: "/fake/Cookies"},
			}, nil
		},
		ExtractToken: func(p auth.Profile) (string, error) {
			if p.Browser != auth.BrowserBrave || p.Name != "Default" {
				t.Fatalf("unexpected profile: %+v", p)
			}
			return secret, nil
		},
		Validate: func(ctx context.Context, token string) (auth.Validation, error) {
			if token != secret {
				t.Fatalf("token = %q, want %q", token, secret)
			}
			return auth.Validation{
				Login:     "pedrosatin",
				UserID:    "123",
				Scopes:    []string{"chat:read", "chat:edit"},
				ExpiresIn: 7200,
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("RunLogin failed: %v", err)
	}

	assertNoTokenLeak(t, out.String(), secret)
	if !strings.Contains(out.String(), "Usando perfil único: brave / Default") {
		t.Fatalf("expected single-profile notice in output, got: %q", out.String())
	}

	saved, err := config.LoadFrom(cfgPath)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if saved.Username != "pedrosatin" || saved.AccessToken != secret {
		t.Fatalf("saved config mismatch: username=%q token_match=%v", saved.Username, saved.AccessToken == secret)
	}
	assertFileMode0600AndNoTmp(t, cfgPath)
}

func TestRunLogin_FromBrowser_InteractiveSelectionAndFlags(t *testing.T) {
	profiles := []auth.Profile{
		{Browser: auth.BrowserBrave, Name: "Default", Family: "chromium", CookiesPath: "/fake/brave/Default/Cookies"},
		{Browser: auth.BrowserFirefox, Name: "dev-edition", Family: "firefox", CookiesPath: "/fake/ff/cookies.sqlite"},
	}

	t.Run("interactive choice selects second profile", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.toml")
		secret := "interactive_ff_secret_token_30ch"
		var out bytes.Buffer

		err := RunLogin(context.Background(), LoginOptions{
			FromBrowser: true,
			ConfigPath:  cfgPath,
			Input:       strings.NewReader("2\n"),
			Output:      &out,
			ListProfiles: func() ([]auth.Profile, error) {
				return profiles, nil
			},
			ExtractToken: func(p auth.Profile) (string, error) {
				if p.Browser != auth.BrowserFirefox || p.Name != "dev-edition" {
					t.Fatalf("expected firefox / dev-edition, got %+v", p)
				}
				return secret, nil
			},
			Validate: func(ctx context.Context, token string) (auth.Validation, error) {
				return auth.Validation{
					Login:     "pedrosatin",
					Scopes:    []string{"chat:read", "chat:edit"},
					ExpiresIn: 7200,
				}, nil
			},
		})
		if err != nil {
			t.Fatalf("RunLogin failed: %v", err)
		}
		assertNoTokenLeak(t, out.String(), secret)
		if !strings.Contains(out.String(), "[1] brave / Default") || !strings.Contains(out.String(), "[2] firefox / dev-edition") {
			t.Fatalf("expected numbered profile list in output, got: %q", out.String())
		}
		saved, err := config.LoadFrom(cfgPath)
		if err != nil {
			t.Fatalf("LoadFrom: %v", err)
		}
		if saved.AccessToken != secret {
			t.Fatalf("saved token mismatch")
		}
		assertFileMode0600AndNoTmp(t, cfgPath)
	})

	t.Run("flags --browser and --profile skip interactive choice", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.toml")
		secret := "flagged_brave_secret_token_30chr"
		var out bytes.Buffer

		err := RunLogin(context.Background(), LoginOptions{
			FromBrowser: true,
			Browser:     "brave",
			Profile:     "Default",
			ConfigPath:  cfgPath,
			Input:       strings.NewReader(""), // must not read from input
			Output:      &out,
			ListProfiles: func() ([]auth.Profile, error) {
				return profiles, nil
			},
			ExtractToken: func(p auth.Profile) (string, error) {
				if p.Browser != auth.BrowserBrave || p.Name != "Default" {
					t.Fatalf("unexpected profile: %+v", p)
				}
				return secret, nil
			},
			Validate: func(ctx context.Context, token string) (auth.Validation, error) {
				return auth.Validation{
					Login:     "pedrosatin",
					Scopes:    []string{"chat:read", "chat:edit"},
					ExpiresIn: 7200,
				}, nil
			},
		})
		if err != nil {
			t.Fatalf("RunLogin failed: %v", err)
		}
		assertNoTokenLeak(t, out.String(), secret)
		if strings.Contains(out.String(), "Escolha um perfil") {
			t.Fatalf("expected prompt to be skipped when flags are provided, got: %q", out.String())
		}
		assertFileMode0600AndNoTmp(t, cfgPath)
	})
}

func TestRunLogin_ImportTwt_PrefixNoPrefixMissingAndInvalid(t *testing.T) {
	t.Run("with oauth: prefix", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.toml")
		twtPath := filepath.Join(dir, "twt.toml")
		secret := "twt_secret_token_with_prefix_30"

		rawTwt := fmt.Sprintf("[twitch]\nusername = \"pedrosatin\"\ntoken = \"oauth:%s\"\n", secret)
		if err := os.WriteFile(twtPath, []byte(rawTwt), 0o600); err != nil {
			t.Fatal(err)
		}

		var out bytes.Buffer
		err := RunLogin(context.Background(), LoginOptions{
			Import:        "twt",
			ConfigPath:    cfgPath,
			TwtConfigPath: twtPath,
			Output:        &out,
			Validate: func(ctx context.Context, token string) (auth.Validation, error) {
				if token != secret {
					t.Fatalf("expected normalized token without oauth:, got %q", token)
				}
				return auth.Validation{
					Login:     "pedrosatin",
					Scopes:    []string{"chat:read", "chat:edit"},
					ExpiresIn: 7200,
				}, nil
			},
		})
		if err != nil {
			t.Fatalf("RunLogin --import twt failed: %v", err)
		}
		assertNoTokenLeak(t, out.String(), secret)

		saved, err := config.LoadFrom(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		if saved.AccessToken != secret || saved.Username != "pedrosatin" {
			t.Fatalf("unexpected saved config: username=%q token_match=%v", saved.Username, saved.AccessToken == secret)
		}
		assertFileMode0600AndNoTmp(t, cfgPath)
	})

	t.Run("without oauth: prefix", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.toml")
		twtPath := filepath.Join(dir, "twt.toml")
		secret := "twt_secret_token_no_prefix_30ch"

		rawTwt := fmt.Sprintf("[twitch]\nusername = \"pedrosatin\"\ntoken = \"%s\"\n", secret)
		if err := os.WriteFile(twtPath, []byte(rawTwt), 0o600); err != nil {
			t.Fatal(err)
		}

		var out bytes.Buffer
		err := RunLogin(context.Background(), LoginOptions{
			Import:        "twt",
			ConfigPath:    cfgPath,
			TwtConfigPath: twtPath,
			Output:        &out,
			Validate: func(ctx context.Context, token string) (auth.Validation, error) {
				if token != secret {
					t.Fatalf("token = %q, want %q", token, secret)
				}
				return auth.Validation{
					Login:     "pedrosatin",
					Scopes:    []string{"chat:read", "chat:edit"},
					ExpiresIn: 7200,
				}, nil
			},
		})
		if err != nil {
			t.Fatalf("RunLogin --import twt failed: %v", err)
		}
		assertNoTokenLeak(t, out.String(), secret)

		saved, err := config.LoadFrom(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		if saved.AccessToken != secret {
			t.Fatalf("saved token mismatch")
		}
		assertFileMode0600AndNoTmp(t, cfgPath)
	})

	t.Run("missing twt file does not write config", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.toml")
		twtPath := filepath.Join(dir, "nonexistent.toml")

		var out bytes.Buffer
		err := RunLogin(context.Background(), LoginOptions{
			Import:        "twt",
			ConfigPath:    cfgPath,
			TwtConfigPath: twtPath,
			Output:        &out,
		})
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected os.ErrNotExist, got %v", err)
		}
		if _, statErr := os.Stat(cfgPath); !os.IsNotExist(statErr) {
			t.Fatalf("expected config file not to be created on failed import")
		}
	})

	t.Run("invalid token in twt fails validation and does not write config", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.toml")
		twtPath := filepath.Join(dir, "twt.toml")
		secret := "invalid_twt_token_value_30chars"

		rawTwt := fmt.Sprintf("[twitch]\nusername = \"pedrosatin\"\ntoken = \"oauth:%s\"\n", secret)
		if err := os.WriteFile(twtPath, []byte(rawTwt), 0o600); err != nil {
			t.Fatal(err)
		}

		var out bytes.Buffer
		err := RunLogin(context.Background(), LoginOptions{
			Import:        "twt",
			ConfigPath:    cfgPath,
			TwtConfigPath: twtPath,
			Output:        &out,
			Validate: func(ctx context.Context, token string) (auth.Validation, error) {
				return auth.Validation{}, &auth.Error{
					Op:      "validate",
					Err:     auth.ErrTokenInvalid,
					Message: "token rejeitado (401): " + secret,
				}
			},
		})
		if !errors.Is(err, auth.ErrTokenInvalid) {
			t.Fatalf("expected ErrTokenInvalid, got %v", err)
		}
		assertNoTokenLeak(t, out.String(), secret)
		assertNoTokenLeak(t, err.Error(), secret)
		if _, statErr := os.Stat(cfgPath); !os.IsNotExist(statErr) {
			t.Fatalf("config file must not be written when token validation fails")
		}
	})
}

func TestRunLogin_Warnings_ScopesExpiryAndOverwrite(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	oldSecret := "old_secret_token_to_be_replaced"
	newSecret := "new_secret_token_replacing_old1"

	// Pre-populate config with loose 0644 permissions and an existing token.
	if err := os.WriteFile(cfgPath, []byte(fmt.Sprintf("language = \"pt-BR\"\nusername = \"olduser\"\naccess_token = %q\n", oldSecret)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cfgPath, 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err := RunLogin(context.Background(), LoginOptions{
		FromBrowser: true,
		Browser:     "brave",
		Profile:     "Default",
		ConfigPath:  cfgPath,
		Output:      &out,
		ListProfiles: func() ([]auth.Profile, error) {
			return []auth.Profile{
				{Browser: auth.BrowserBrave, Name: "Default", Family: "chromium", CookiesPath: "/fake/Cookies"},
			}, nil
		},
		ExtractToken: func(p auth.Profile) (string, error) {
			return newSecret, nil
		},
		Validate: func(ctx context.Context, token string) (auth.Validation, error) {
			return auth.Validation{
				Login:     "pedrosatin",
				Scopes:    []string{"user:read:email"}, // missing chat:read and chat:edit
				ExpiresIn: 1200,                        // < 1h
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("RunLogin failed: %v", err)
	}

	output := out.String()
	assertNoTokenLeak(t, output, oldSecret)
	assertNoTokenLeak(t, output, newSecret)

	if !strings.Contains(output, "chat:read") || !strings.Contains(output, "chat:edit") {
		t.Fatalf("expected warning about missing chat:read/chat:edit scopes, got: %q", output)
	}
	if !strings.Contains(output, "fim da validade") {
		t.Fatalf("expected warning about low expires_in (<1h), got: %q", output)
	}
	if !strings.Contains(output, "substituído") {
		t.Fatalf("expected warning about replacing existing token, got: %q", output)
	}

	saved, err := config.LoadFrom(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Language != "pt-BR" || saved.Username != "pedrosatin" || saved.AccessToken != newSecret {
		t.Fatalf("saved config mismatch: %+v", saved)
	}
	assertFileMode0600AndNoTmp(t, cfgPath)
}

func TestRunLogin_DistinctActionableErrors(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantSub  string
		wantSent error
	}{
		{
			name:     "keyring locked or unavailable",
			err:      auth.ErrKeyringUnavailable,
			wantSub:  "chaveiro bloqueado ou ausente",
			wantSent: auth.ErrKeyringUnavailable,
		},
		{
			name:     "profile without twitch cookie",
			err:      auth.ErrCookieNotFound,
			wantSub:  "perfil sem cookie do twitch.tv",
			wantSent: auth.ErrCookieNotFound,
		},
		{
			name:     "browser not found",
			err:      auth.ErrBrowserNotFound,
			wantSub:  "navegador ausente",
			wantSent: auth.ErrBrowserNotFound,
		},
		{
			name:     "sqlite3 not in PATH",
			err:      auth.ErrSQLiteUnavailable,
			wantSub:  "sqlite3 não encontrado no PATH",
			wantSent: auth.ErrSQLiteUnavailable,
		},
		{
			name:     "invalid token 401",
			err:      auth.ErrTokenInvalid,
			wantSub:  "token inválido ou expirado (401)",
			wantSent: auth.ErrTokenInvalid,
		},
		{
			name:     "validation timeout",
			err:      auth.ErrValidateTimeout,
			wantSub:  "tempo esgotado ao validar token",
			wantSent: auth.ErrValidateTimeout,
		},
	}

	seenMessages := map[string]string{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "config.toml")
			var out bytes.Buffer

			err := RunLogin(context.Background(), LoginOptions{
				FromBrowser: true,
				Browser:     "brave",
				Profile:     "Default",
				Lang:        "pt-BR",
				ConfigPath:  cfgPath,
				Output:      &out,
				ListProfiles: func() ([]auth.Profile, error) {
					return []auth.Profile{
						{Browser: auth.BrowserBrave, Name: "Default", Family: "chromium", CookiesPath: "/fake/Cookies"},
					}, nil
				},
				ExtractToken: func(p auth.Profile) (string, error) {
					if errors.Is(tc.err, auth.ErrTokenInvalid) || errors.Is(tc.err, auth.ErrValidateTimeout) {
						return "secret_token_for_error_test_30c", nil
					}
					return "", tc.err
				},
				Validate: func(ctx context.Context, token string) (auth.Validation, error) {
					return auth.Validation{}, tc.err
				},
			})
			if !errors.Is(err, tc.wantSent) {
				t.Fatalf("err = %v, want errors.Is(%v)", err, tc.wantSent)
			}
			msg := err.Error()
			assertNoTokenLeak(t, msg, "secret_token_for_error_test_30c")
			assertNoTokenLeak(t, out.String(), "secret_token_for_error_test_30c")
			if !strings.Contains(msg, tc.wantSub) {
				t.Fatalf("error message %q does not contain expected actionable phrase %q", msg, tc.wantSub)
			}
			if prev, dup := seenMessages[msg]; dup {
				t.Fatalf("duplicate error message between %q and %q: %q", prev, tc.name, msg)
			}
			seenMessages[msg] = tc.name
		})
	}
}

func assertNoTokenLeak(t *testing.T, text, secret string) {
	t.Helper()
	if secret != "" && strings.Contains(text, secret) {
		t.Fatalf("SECURITY VIOLATION: secret token %q leaked in output: %q", secret, text)
	}
}

func assertFileMode0600AndNoTmp(t *testing.T, cfgPath string) {
	t.Helper()
	st, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("stat %s: %v", cfgPath, err)
	}
	// Windows has no mode bits: Go reports 0666 whatever the ACL says.
	if perm := st.Mode().Perm(); runtime.GOOS != "windows" && perm != 0o600 {
		t.Fatalf("config permissions = %#o, want 0600", perm)
	}
	entries, err := os.ReadDir(filepath.Dir(cfgPath))
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Fatalf("leftover temp file found after atomic save: %s", e.Name())
		}
	}
}

func TestRunLogin_ScopeWarnings(t *testing.T) {
	cases := []struct {
		name        string
		scopes      []string
		wantWarning bool
	}{
		{
			name:        "chat_login legacy scope does not emit warning",
			scopes:      []string{"chat_login"},
			wantWarning: false,
		},
		{
			name:        "no relevant chat scopes emits warning",
			scopes:      []string{"user:read:email"},
			wantWarning: true,
		},
		{
			name:        "chat:read and chat:edit scopes do not emit warning",
			scopes:      []string{"chat:read", "chat:edit"},
			wantWarning: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "config.toml")
			var out bytes.Buffer

			err := RunLogin(context.Background(), LoginOptions{
				FromBrowser: true,
				Browser:     "brave",
				Profile:     "Default",
				Lang:        "pt-BR",
				ConfigPath:  cfgPath,
				Output:      &out,
				ListProfiles: func() ([]auth.Profile, error) {
					return []auth.Profile{
						{Browser: auth.BrowserBrave, Name: "Default", Family: "chromium", CookiesPath: "/fake/Cookies"},
					}, nil
				},
				ExtractToken: func(p auth.Profile) (string, error) {
					return "valid_token_30chars_for_scope_test", nil
				},
				Validate: func(ctx context.Context, token string) (auth.Validation, error) {
					return auth.Validation{
						Login:     "pedrosatin",
						Scopes:    tc.scopes,
						ExpiresIn: 7200,
					}, nil
				},
			})
			if err != nil {
				t.Fatalf("RunLogin failed: %v", err)
			}

			output := out.String()
			hasWarning := strings.Contains(output, "o envio de mensagens não vai funcionar")
			if hasWarning != tc.wantWarning {
				t.Fatalf("warning present = %v, want %v; output:\n%s", hasWarning, tc.wantWarning, output)
			}
			if tc.wantWarning {
				if !strings.Contains(output, "logar de novo no navegador") {
					t.Fatalf("warning must tell user to login again in browser, got: %q", output)
				}
			}
		})
	}
}

func TestRunLogin_EnglishLanguageOutput(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	secret := "secret_token_for_english_test_123"

	var out bytes.Buffer
	err := RunLogin(context.Background(), LoginOptions{
		FromBrowser: true,
		Lang:        "en",
		ConfigPath:  cfgPath,
		Output:      &out,
		ListProfiles: func() ([]auth.Profile, error) {
			return []auth.Profile{
				{Browser: auth.BrowserBrave, Name: "Default", Family: "chromium", CookiesPath: "/fake/Cookies"},
			}, nil
		},
		ExtractToken: func(p auth.Profile) (string, error) {
			return secret, nil
		},
		Validate: func(ctx context.Context, token string) (auth.Validation, error) {
			return auth.Validation{
				Login:     "pedrosatin",
				Scopes:    []string{"chat:read", "chat:edit"},
				ExpiresIn: 7200,
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("RunLogin failed: %v", err)
	}

	output := out.String()
	assertNoTokenLeak(t, output, secret)
	if !strings.Contains(output, "Using single profile: brave / Default") {
		t.Fatalf("expected English single profile output, got: %q", output)
	}
	if !strings.Contains(output, "Login completed for pedrosatin") {
		t.Fatalf("expected English login completed output, got: %q", output)
	}

	saved, err := config.LoadFrom(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Language != "en" {
		t.Fatalf("expected saved.Language == en, got %q", saved.Language)
	}
}
