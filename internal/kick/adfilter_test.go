package kick

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// As duas linhas inteiras do writer HLS do streamlink 8.x que o detector de
// intervalo do player aceita (internal/player/detect.go).
const (
	holdStartLine = "[stream.hls][info] Filtering out segments and pausing stream output"
	holdEndLine   = "[stream.hls][info] Resuming stream output"
)

// fallbackLinePrefix é o começo da linha que o plugin registra quando cede ao
// plugin embutido; o player a reconhece e a manda para o log de saída.
const fallbackLinePrefix = "[plugins.kick][warning] ttv-tui plugin fallback: "

func TestStreamTargetCarriesAdFilterPlugin(t *testing.T) {
	p := NewProvider()
	for _, ch := range []catalog.Channel{
		{Platform: platform.Kick, Login: "gaules"},
		{Platform: platform.Kick, Login: "xqc/videos/0a1b", URL: "https://kick.com/xqc/videos/0a1b"},
	} {
		got, err := p.StreamTarget(ch)
		if err != nil {
			t.Fatalf("StreamTarget(%+v): %v", ch, err)
		}
		if len(got.Plugins) != 1 || got.Plugins[0].Name != "kick" || !bytes.Equal(got.Plugins[0].Source, adFilterSource) {
			t.Fatalf("plugins = %+v, want the embedded kick plugin", got.Plugins)
		}
	}

	// Cada alvo leva a própria cópia: mexer numa não muda a próxima.
	first, _ := p.StreamTarget(catalog.Channel{Platform: platform.Kick, Login: "gaules"})
	first.Plugins[0].Source[0] ^= 0xff
	second, _ := p.StreamTarget(catalog.Channel{Platform: platform.Kick, Login: "gaules"})
	if !bytes.Equal(second.Plugins[0].Source, adFilterSource) {
		t.Fatal("a target changed the embedded plugin source")
	}
}

// O plugin precisa casar com o que o resto do app espera dele: o nome do
// módulo embutido, a classe do anúncio do IVS e o prefixo da linha de fallback.
func TestAdFilterSourceContract(t *testing.T) {
	src := string(adFilterSource)
	for _, want := range []string{
		`AD_CLASS = "live-video-net-stitched-ad"`,
		`AD_ID_PREFIX = "stitched-ad-"`,
		`FALLBACK_PREFIX = "ttv-tui plugin fallback: "`,
		"__plugin__ = _plugin",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("plugin source lacks %q", want)
		}
	}
	if !strings.HasPrefix(fallbackLinePrefix, "[plugins."+adFilterPluginName+"][warning] ") {
		t.Errorf("fallback prefix %q does not name the %q plugin", fallbackLinePrefix, adFilterPluginName)
	}
}

// isolatedEnv roda Python e streamlink sem o HOME do operador: o streamlink lê
// config, cache e plugins de lá (regra 7).
func isolatedEnv(t *testing.T) []string {
	t.Helper()
	home := t.TempDir()
	env := []string{
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, "config"),
		"XDG_STATE_HOME=" + filepath.Join(home, "state"),
		"XDG_DATA_HOME=" + filepath.Join(home, "data"),
		"XDG_CACHE_HOME=" + filepath.Join(home, "cache"),
		"APPDATA=" + filepath.Join(home, "appdata"),
		"LOCALAPPDATA=" + filepath.Join(home, "localappdata"),
		"PYTHONDONTWRITEBYTECODE=1",
	}
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		switch key {
		case "PATH", "SYSTEMROOT", "TMPDIR", "TEMP", "TMP", "LANG", "LC_ALL":
			env = append(env, kv)
		}
	}
	return env
}

// pythonCandidates lists interpreters to try, the one streamlink runs on first:
// the shebang of the streamlink in PATH, then python3 and python.
func pythonCandidates() []string {
	var out []string
	if sl, err := exec.LookPath("streamlink"); err == nil {
		if f, err := os.Open(sl); err == nil {
			line, _ := bufio.NewReader(f).ReadString('\n')
			_ = f.Close()
			if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "#!"); ok {
				fields := strings.Fields(rest)
				if len(fields) > 0 && filepath.Base(fields[0]) == "env" && len(fields) > 1 {
					fields = fields[1:]
				}
				if len(fields) > 0 {
					if p, err := exec.LookPath(fields[0]); err == nil {
						out = append(out, p)
					}
				}
			}
		}
	}
	for _, name := range []string{"python3", "python"} {
		if p, err := exec.LookPath(name); err == nil && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

func runPython(t *testing.T, py string, env []string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, py, append([]string{"-I"}, args...)...)
	cmd.Env = env
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	return out.String(), errOut.String(), err
}

// anyPython returns a Python that runs, or skips with the reason.
func anyPython(t *testing.T, env []string) string {
	t.Helper()
	for _, py := range pythonCandidates() {
		if _, _, err := runPython(t, py, env, "-c", "import sys"); err == nil {
			return py
		}
	}
	t.Skip("sem Python no PATH: python3, python e o shebang do streamlink não rodaram")
	return ""
}

// streamlinkPython returns a Python that imports streamlink, or skips.
func streamlinkPython(t *testing.T, env []string) string {
	t.Helper()
	tried := pythonCandidates()
	for _, py := range tried {
		if _, _, err := runPython(t, py, env, "-c", "import streamlink, streamlink.stream.hls"); err == nil {
			return py
		}
	}
	t.Skipf("nenhum Python com o streamlink importável (tentados: %q); o AppImage gerenciado não expõe o Python dele", tried)
	return ""
}

// Um SyntaxError no plugin derruba a CLI do streamlink antes de tocar: a
// sintaxe tem de valer no Python mais antigo que o streamlink 8 aceita.
func TestAdFilterSourceParsesOnPython310(t *testing.T) {
	env := isolatedEnv(t)
	py := anyPython(t, env)
	path := filepath.Join(t.TempDir(), "kick.py")
	if err := os.WriteFile(path, adFilterSource, 0o600); err != nil {
		t.Fatal(err)
	}
	check := "import ast, sys; ast.parse(open(sys.argv[1], encoding='utf-8').read(), sys.argv[1], feature_version=(3, 10))"
	if _, stderr, err := runPython(t, py, env, "-c", check, path); err != nil {
		t.Fatalf("%s does not parse the plugin as Python 3.10: %v\n%s", py, err, stderr)
	}
}

func writePluginDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "plugins")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "kick.py"), adFilterSource, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func harnessLines(s string) []string {
	return strings.Split(strings.TrimRight(strings.ReplaceAll(s, "\r\n", "\n"), "\n"), "\n")
}

// O plugin roda dentro do streamlink instalado contra uma live sintética
// servida em 127.0.0.1, com o anúncio marcado como o IVS marca.
func TestAdFilterPluginInStreamlink(t *testing.T) {
	if testing.Short() {
		t.Skip("integração com o streamlink instalado")
	}
	env := isolatedEnv(t)
	py := streamlinkPython(t, env)
	harness, err := filepath.Abs(filepath.Join("testdata", "adfilter_harness.py"))
	if err != nil {
		t.Fatal(err)
	}

	run := func(t *testing.T, mode string, extra ...string) (stdout []string, stderr []string) {
		t.Helper()
		out, errOut, err := runPython(t, py, env, append([]string{harness, mode, writePluginDir(t)}, extra...)...)
		stdout, stderr = harnessLines(out), harnessLines(errOut)
		if last := stdout[len(stdout)-1]; strings.HasPrefix(last, "SKIP ") {
			t.Skip(strings.TrimPrefix(last, "SKIP "))
		}
		if err != nil || stdout[len(stdout)-1] != "OK" {
			t.Fatalf("harness %s: %v\nstdout:\n%s\nstderr:\n%s", mode, err, out, errOut)
		}
		return stdout, stderr
	}

	t.Run("descarta o anúncio e registra o intervalo", func(t *testing.T) {
		stdout, stderr := run(t, "filter")
		if !slices.Contains(stdout, "DATA content-0|content-1|content-2|content-3|") {
			t.Fatalf("stdout = %q, want only the content segments", stdout)
		}
		start, end := slices.Index(stderr, holdStartLine), slices.Index(stderr, holdEndLine)
		if start < 0 || end < start {
			t.Fatalf("stderr lacks %q then %q:\n%s", holdStartLine, holdEndLine, strings.Join(stderr, "\n"))
		}
		for _, l := range stderr {
			if strings.HasPrefix(l, fallbackLinePrefix) {
				t.Fatalf("the plugin fell back on this streamlink: %q", l)
			}
		}
	})

	t.Run("anúncio declarado em duas partes com o mesmo ID", func(t *testing.T) {
		stdout, stderr := run(t, "filter-split")
		if !slices.Contains(stdout, "DATA content-0|content-1|content-2|content-3|") {
			t.Fatalf("stdout = %q, want only the content segments", stdout)
		}
		start, end := slices.Index(stderr, holdStartLine), slices.Index(stderr, holdEndLine)
		if start < 0 || end < start {
			t.Fatalf("stderr lacks %q then %q:\n%s", holdStartLine, holdEndLine, strings.Join(stderr, "\n"))
		}
	})

	t.Run("anúncio sem fim conhecido não descarta nada", func(t *testing.T) {
		stdout, stderr := run(t, "filter-open")
		if !slices.Contains(stdout, "DATA content-0|content-1|ad-0|ad-1|content-2|content-3|") {
			t.Fatalf("stdout = %q, want every segment", stdout)
		}
		if slices.Contains(stderr, holdStartLine) || slices.Contains(stderr, holdEndLine) {
			t.Fatalf("an unbounded ad paused the output:\n%s", strings.Join(stderr, "\n"))
		}
	})

	t.Run("sem o plugin o anúncio passa", func(t *testing.T) {
		stdout, stderr := run(t, "builtin")
		if !slices.Contains(stdout, "DATA content-0|content-1|ad-0|ad-1|content-2|content-3|") {
			t.Fatalf("stdout = %q, want the ad segments too", stdout)
		}
		if slices.Contains(stderr, holdStartLine) || slices.Contains(stderr, holdEndLine) {
			t.Fatalf("the built-in plugin paused the output:\n%s", strings.Join(stderr, "\n"))
		}
	})

	for _, mode := range []string{"fallback-class", "fallback-none"} {
		t.Run("cede ao plugin embutido: "+mode, func(t *testing.T) {
			stdout, stderr := run(t, mode, t.TempDir())
			if !slices.ContainsFunc(stdout, func(l string) bool { return strings.HasPrefix(l, "PLUGIN ") }) {
				t.Fatalf("stdout = %q, want the built-in plugin path", stdout)
			}
			if !slices.ContainsFunc(stderr, func(l string) bool { return strings.HasPrefix(l, fallbackLinePrefix) }) {
				t.Fatalf("stderr lacks the fallback line:\n%s", strings.Join(stderr, "\n"))
			}
			for _, l := range stderr {
				if strings.Contains(l, "Traceback") || strings.Contains(l, "Failed to load plugin") {
					t.Fatalf("the fallback raised inside streamlink: %q", l)
				}
			}
		})
	}
}

// A CLI de verdade carrega o plugin pelo --plugin-dir que o player passa, sem
// rede: --can-handle-url-no-redirect só casa a URL com um plugin.
func TestStreamlinkCLILoadsAdFilterPlugin(t *testing.T) {
	if testing.Short() {
		t.Skip("integração com o streamlink instalado")
	}
	if runtime.GOOS == "windows" {
		t.Skip("o streamlink do Windows é o gerenciado; a integração em Python cobre o plugin")
	}
	sl, err := exec.LookPath("streamlink")
	if err != nil {
		t.Skip("streamlink fora do PATH")
	}
	dir := writePluginDir(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, sl, "--no-config", "--loglevel", "info", "--plugin-dir", dir, "--can-handle-url-no-redirect", "https://kick.com/xqc")
	cmd.Env = isolatedEnv(t)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s --can-handle-url-no-redirect with the plugin: %v\n%s", sl, err, out.String())
	}
	text := out.String()
	if !strings.Contains(text, "Plugin kick is being overridden by "+filepath.Join(dir, "kick.py")) {
		if strings.Contains(text, fallbackLinePrefix) {
			t.Skipf("este streamlink não tem o que o plugin estende; ele cedeu ao embutido:\n%s", text)
		}
		t.Fatalf("streamlink did not load the plugin:\n%s", text)
	}
}
