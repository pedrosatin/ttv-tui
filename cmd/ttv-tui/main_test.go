package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/app"
	"github.com/pedrosatin/ttv-tui/internal/auth"
	"github.com/pedrosatin/ttv-tui/internal/config"
)

func TestCLIVersionAndLoginNeverPrintToken(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	twtPath := filepath.Join(dir, "twt.toml")
	secretBrowser := "cli_browser_secret_token_30chrs"
	secretTwt := "cli_twt_secret_token_30chars_ok"

	if err := os.WriteFile(twtPath, []byte(fmt.Sprintf("[twitch]\nusername = \"pedrosatin\"\ntoken = \"oauth:%s\"\n", secretTwt)), 0o600); err != nil {
		t.Fatal(err)
	}

	baseOpts := app.LoginOptions{
		Lang:          "pt-BR",
		ConfigPath:    cfgPath,
		TwtConfigPath: twtPath,
		ListProfiles: func() ([]auth.Profile, error) {
			return []auth.Profile{
				{Browser: auth.BrowserBrave, Name: "Default", Family: "chromium", CookiesPath: "/fake/Cookies"},
			}, nil
		},
		ExtractToken: func(p auth.Profile) (string, error) {
			return secretBrowser, nil
		},
		Validate: func(ctx context.Context, token string) (auth.Validation, error) {
			return auth.Validation{
				Login:     "pedrosatin",
				Scopes:    []string{"chat:read", "chat:edit"},
				ExpiresIn: 7200,
			}, nil
		},
	}

	// 1. ttv-tui login --from-browser --browser brave --profile Default
	var stdout, stderr bytes.Buffer
	code := runWithIO(
		[]string{"login", "--from-browser", "--browser", "brave", "--profile", "Default"},
		strings.NewReader(""),
		&stdout,
		&stderr,
		baseOpts,
	)
	if code != 0 {
		t.Fatalf("login --from-browser exit code = %d, stderr = %s", code, stderr.String())
	}
	if strings.Contains(stdout.String()+stderr.String(), secretBrowser) {
		t.Fatalf("token leaked in CLI output: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}

	saved, err := config.LoadFrom(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.AccessToken != secretBrowser {
		t.Fatalf("saved token mismatch after browser login")
	}

	// 2. ttv-tui login --import twt (replaces existing token)
	stdout.Reset()
	stderr.Reset()
	code = runWithIO(
		[]string{"login", "--import", "twt"},
		strings.NewReader(""),
		&stdout,
		&stderr,
		baseOpts,
	)
	if code != 0 {
		t.Fatalf("login --import twt exit code = %d, stderr = %s", code, stderr.String())
	}
	combined := stdout.String() + stderr.String()
	if strings.Contains(combined, secretBrowser) || strings.Contains(combined, secretTwt) {
		t.Fatalf("token leaked in CLI import output: %q", combined)
	}
	if !strings.Contains(stdout.String(), "substituído") {
		t.Fatalf("expected overwrite warning in stdout, got: %q", stdout.String())
	}

	// 3. ttv-tui --version never prints token even when config exists
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("--version exit code = %d", code)
	}
	if strings.Contains(stdout.String()+stderr.String(), secretTwt) {
		t.Fatalf("--version leaked token")
	}
}

func TestCLILanguageResolutionAndHelpOutput(t *testing.T) {
	// 1. --lang en prints English help
	var stdout, stderr bytes.Buffer
	code := run([]string{"--lang", "en", "--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("--lang en --help exit code = %d", code)
	}
	out := stdout.String()
	if !strings.Contains(out, "Usage:") || !strings.Contains(out, "Flags:") || !strings.Contains(out, "interface language (en|pt-BR)") {
		t.Fatalf("expected English help, got: %s", out)
	}

	// 2. --lang pt-BR prints Portuguese help
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--lang", "pt-BR", "--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("--lang pt-BR --help exit code = %d", code)
	}
	out = stdout.String()
	if !strings.Contains(out, "Uso:") || !strings.Contains(out, "Opções:") || !strings.Contains(out, "idioma da interface (en|pt-BR)") {
		t.Fatalf("expected Portuguese help, got: %s", out)
	}

	// 3. login --lang en --help
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"login", "--lang", "en", "--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("login --lang en --help exit code = %d", code)
	}
	out = stdout.String()
	if !strings.Contains(out, "Usage:") || !strings.Contains(out, "extract Twitch auth-token cookie") {
		t.Fatalf("expected English login help, got: %s", out)
	}

	// 4. login --lang pt-BR --help
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"login", "--lang", "pt-BR", "--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("login --lang pt-BR --help exit code = %d", code)
	}
	out = stdout.String()
	if !strings.Contains(out, "Uso:") || !strings.Contains(out, "extrair cookie auth-token da Twitch") {
		t.Fatalf("expected Portuguese login help, got: %s", out)
	}

	// 5. Unknown command in en vs pt-BR
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--lang", "en", "foo"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("unknown command exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), `ttv-tui: unknown command "foo"`) {
		t.Fatalf("expected English unknown command, got: %s", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--lang", "pt-BR", "foo"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("unknown command exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), `ttv-tui: comando desconhecido "foo"`) {
		t.Fatalf("expected Portuguese unknown command, got: %s", stderr.String())
	}
}

func TestCLILangFlagPersistsToConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	secret := "test_secret_token_1234567890"

	baseOpts := app.LoginOptions{
		ConfigPath: cfgPath,
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
	}

	// Run login with --lang pt-BR
	var stdout, stderr bytes.Buffer
	code := runWithIO(
		[]string{"login", "--lang", "pt-BR", "--from-browser", "--browser", "brave", "--profile", "Default"},
		strings.NewReader(""),
		&stdout,
		&stderr,
		baseOpts,
	)
	if code != 0 {
		t.Fatalf("login failed: %s", stderr.String())
	}

	saved, err := config.LoadFrom(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Language != "pt-BR" {
		t.Fatalf("expected saved.Language to be pt-BR, got %q", saved.Language)
	}

	// Now run login with --lang en, should update config to en
	stdout.Reset()
	stderr.Reset()
	code = runWithIO(
		[]string{"login", "--lang", "en", "--from-browser", "--browser", "brave", "--profile", "Default"},
		strings.NewReader(""),
		&stdout,
		&stderr,
		baseOpts,
	)
	if code != 0 {
		t.Fatalf("login failed: %s", stderr.String())
	}

	saved, err = config.LoadFrom(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Language != "en" {
		t.Fatalf("expected saved.Language to be en, got %q", saved.Language)
	}
}

func TestDoctorCLI(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(dir, "runtime"))
	t.Setenv("PATH", dir)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"doctor", "--lang", "en"}, "Token configured: NO"},
		{[]string{"--lang", "pt-BR", "doctor"}, "Token configurado: NÃO"},
		{[]string{"doctor", "--lang=pt-BR"}, "Token configurado: NÃO"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(tc.args, &stdout, &stderr); code != 1 {
			t.Fatalf("doctor exit = %d", code)
		}
		if !strings.Contains(stdout.String(), tc.want) || stderr.Len() != 0 {
			t.Fatal("doctor dispatch or language failed")
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"doctor", "--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("help exit = %d", code)
	}
	if code := run([]string{"doctor", "extra"}, &stdout, &stderr); code != 2 {
		t.Fatalf("argument exit = %d", code)
	}
}
