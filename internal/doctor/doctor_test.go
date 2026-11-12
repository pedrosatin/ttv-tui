package doctor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/config"
	"github.com/pedrosatin/ttv-tui/internal/player"
)

func TestMain(m *testing.M) {
	if os.Getenv("TTV_DOCTOR_HELPER") == "1" {
		if slices.Contains(os.Args[1:], "--vo=help") {
			if os.Getenv("TTV_DOCTOR_FAKE_VO_WAIT") == "1" {
				time.Sleep(time.Minute)
			}
			fmt.Print(os.Getenv("TTV_DOCTOR_FAKE_VO"))
			if os.Getenv("TTV_DOCTOR_FAKE_VO_FAIL") == "1" {
				os.Exit(1)
			}
			os.Exit(0)
		}
		fmt.Println("mpv v9.8.7")
		fmt.Println(os.Getenv("TTV_DOCTOR_FAKE_SECRET"))
		_, _ = fmt.Fprintln(os.Stderr, os.Getenv("TTV_DOCTOR_FAKE_SECRET"))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func isolate(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_RUNTIME_DIR"} {
		t.Setenv(key, filepath.Join(base, key))
	}
	// Fake tools only print fixtures. Skip the race runtime's one-second exit
	// grace period so tool startup/exit does not consume the probe deadline.
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	t.Setenv("TTV_DOCTOR_HELPER", "1")
	return base
}

func fakeTool(t *testing.T, dir, name string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestReport(t *testing.T) {
	for _, lang := range []string{"en", "pt-BR"} {
		t.Run(lang, func(t *testing.T) {
			base := isolate(t)
			fakeTool(t, base, "streamlink")
			fakeTool(t, base, "mpv")
			t.Setenv("PATH", base)
			secret := "doctor-test-credential-never-display"
			t.Setenv("TTV_DOCTOR_FAKE_SECRET", secret)
			if err := config.Save(config.Config{AccessToken: secret}); err != nil {
				t.Fatal("save fixture failed")
			}
			var out bytes.Buffer
			code := Run(&out, Options{Version: "test", Lang: lang})
			// Never include captured output in failure messages: it might contain a secret.
			if strings.Contains(out.String(), secret) || strings.Contains(out.String(), "credential-never") {
				t.Fatal("credential leaked")
			}
			want := "Token configured: YES"
			if lang == "pt-BR" {
				want = "Token configurado: SIM"
			}
			for _, text := range []string{want, "streamlink: 9.8.7", "mpv: 9.8.7", runtime.GOOS + "/" + runtime.GOARCH, filepath.Join(base, "XDG_RUNTIME_DIR", "ttv-tui")} {
				if !strings.Contains(out.String(), text) {
					t.Fatal("report missing expected field")
				}
			}
			if code != 0 {
				t.Fatalf("exit = %d", code)
			}
		})
	}
}

func TestMissingDependencyAndNoToken(t *testing.T) {
	for _, lang := range []string{"en", "pt-BR"} {
		t.Run(lang, func(t *testing.T) {
			base := isolate(t)
			fakeTool(t, base, "mpv")
			t.Setenv("PATH", base)
			var out bytes.Buffer
			if Run(&out, Options{Lang: lang}) != 1 {
				t.Fatal("missing dependency must fail")
			}
			want, token := "streamlink: MISSING", "Token configured: NO"
			if lang == "pt-BR" {
				want, token = "streamlink: AUSENTE", "Token configurado: NÃO"
			}
			if !strings.Contains(out.String(), want) || !strings.Contains(out.String(), token) {
				t.Fatal("incorrect missing dependency or token status")
			}
		})
	}
}

func TestRegistryReadOnly(t *testing.T) {
	base := isolate(t)
	dir := filepath.Join(base, "players")
	if got := registryStatus(dir); got != "doctor.clean" {
		t.Fatal(got)
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "player-1-2.json")
	if err := os.WriteFile(path, []byte("invalid record"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := registryStatus(dir); got != "doctor.pending" {
		t.Fatal(got)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "invalid record" {
		t.Fatal("registry modified")
	}
	if got := registryStatus(path); got != "doctor.unreadable" {
		t.Fatal(got)
	}
}

func TestMalformedConfigDoesNotLeak(t *testing.T) {
	base := isolate(t)
	t.Setenv("PATH", base)
	path := filepath.Join(base, "config.toml")
	secret := "malformed-sensitive-value"
	if err := os.WriteFile(path, []byte("min_viewers = "+secret), 0o600); err != nil {
		t.Fatal("write fixture failed")
	}
	var out bytes.Buffer
	if Run(&out, Options{Lang: "en", ConfigPath: path}) != 1 {
		t.Fatal("unreadable config must fail")
	}
	if strings.Contains(out.String(), secret) {
		t.Fatal("credential leaked from error")
	}
	if !strings.Contains(out.String(), "Token configured: unreadable") {
		t.Fatal("missing config error")
	}
}

func TestRegistryFinishedSessionIsPending(t *testing.T) {
	base := isolate(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe)
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	// Use an exited, reaped process rather than a guessed unused PID, on every OS.
	dead := cmd.Process.Pid
	dir := filepath.Join(base, "players")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(base, "input.conf")
	if err := os.WriteFile(conf, []byte("session residue"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(player.Record{OwnerPID: dead, PGID: dead, Socket: filepath.Join(base, "i"), Dir: base})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "player-1-2.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := registryStatus(dir); got != "doctor.pending" {
		t.Fatalf("finished session = %s, want doctor.pending", got)
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, data) {
		t.Fatal("doctor modified the record")
	}
	if got, err := os.ReadFile(conf); err != nil || string(got) != "session residue" {
		t.Fatal("doctor modified session files")
	}
	if orphans, err := player.FindOrphans(dir); err != nil || len(orphans) != 0 {
		t.Fatalf("FindOrphans = %v, %v", orphans, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("FindOrphans did not remove stale record: %v", err)
	}
	if got := registryStatus(dir); got != "doctor.clean" {
		t.Fatalf("after cleanup = %s, want doctor.clean", got)
	}
}

func TestReportStreamlinkOrigins(t *testing.T) {
	// Case 1: Config origin
	t.Run("config", func(t *testing.T) {
		base := isolate(t)
		cfgBin := filepath.Join(base, "custom-streamlink")
		if runtime.GOOS == "windows" {
			cfgBin += ".exe" // fakeTool adds the extension there
		}
		fakeTool(t, base, "custom-streamlink")
		fakeTool(t, base, "mpv")
		t.Setenv("PATH", base)
		if err := config.Save(config.Config{StreamlinkPath: cfgBin}); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		_ = Run(&out, Options{Version: "test", Lang: "pt-BR"})
		if !strings.Contains(out.String(), "streamlink: 9.8.7 (config)") {
			t.Fatalf("expected streamlink with config origin, got: %s", out.String())
		}
	})

	// Case 2: Managed origin
	if runtime.GOOS != "darwin" {
		t.Run("managed", func(t *testing.T) {
			base := isolate(t)
			fakeTool(t, base, "mpv")
			// Create managed streamlink under XDG_DATA_HOME
			dataDir := filepath.Join(base, "XDG_DATA_HOME")
			t.Setenv("XDG_DATA_HOME", dataDir)
			managedDir := filepath.Join(dataDir, "ttv-tui", "components", "streamlink", "8.6.3-1")
			if err := os.MkdirAll(managedDir, 0o700); err != nil {
				t.Fatal(err)
			}
			fakeTool(t, managedDir, "streamlink")
			t.Setenv("PATH", base)

			var out bytes.Buffer
			_ = Run(&out, Options{Version: "test", Lang: "pt-BR"})
			if !strings.Contains(out.String(), "streamlink: 9.8.7 (gerenciado)") {
				t.Fatalf("expected streamlink with gerenciado origin, got: %s", out.String())
			}

			var outEN bytes.Buffer
			_ = Run(&outEN, Options{Version: "test", Lang: "en"})
			if !strings.Contains(outEN.String(), "streamlink: 9.8.7 (managed)") {
				t.Fatalf("expected streamlink with managed origin, got: %s", outEN.String())
			}
		})
	}

	// Case 3: PATH origin
	t.Run("path", func(t *testing.T) {
		base := isolate(t)
		fakeTool(t, base, "streamlink")
		fakeTool(t, base, "mpv")
		t.Setenv("PATH", base)
		var out bytes.Buffer
		_ = Run(&out, Options{Version: "test", Lang: "pt-BR"})
		if !strings.Contains(out.String(), "streamlink: 9.8.7 (PATH)") {
			t.Fatalf("expected streamlink with PATH origin, got: %s", out.String())
		}
	})
}
