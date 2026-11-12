package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// allowedExceptions documents literals permitted in UI, login, and CLI files:
// - Symbols, layout separators, formatting prefixes, and punctuation marks.
// - Technical format verbs and composite layout format strings.
// - Keyboard shortcut identifiers and key symbols (e.g. up, down, esc, j, k).
// - Protocol, service, and external tool identifiers (e.g. IRC tokens, twitch.tv URLs).
// - CLI command names, flag names, and flag prefixes.
// - Internal model and state identifiers (e.g. category, channel, playback states).
var allowedExceptions = map[string]bool{
	// Whitespace, punctuation, borders and markers
	"":           true,
	" ":          true,
	"\n":         true,
	"\r":         true,
	"\t":         true,
	" · ":        true,
	" | ":        true,
	" │ ":        true,
	" — ":        true,
	" - ":        true,
	": ":         true,
	" > ":        true,
	"  > ":       true,
	"  ! ":       true,
	"  * ":       true,
	"▸ ":         true,
	"▸":          true,
	"★ ":         true,
	"★":          true,
	"●":          true,
	"▶":          true,
	"‖":          true,
	"✖":          true,
	"!":          true,
	"▌ ":         true,
	"▌★":         true,
	"▌* ":        true,
	"  ":         true,
	"   ":        true,
	"── %s ──":   true,
	"[REDACTED]": true,
	"...":        true,
	"█":          true,
	",":          true,
	", ":         true,
	"/":          true,
	".":          true,
	"[":          true,
	"]":          true,
	" [":         true,
	" (":         true,
	"(":          true,
	")":          true,
	"…":          true,
	"=== ":       true,
	" ===":       true,

	// Format strings and specifiers
	"%s":                    true,
	"%d":                    true,
	"%s (%s)":               true,
	"%s %s":                 true,
	"%s: %s%s":              true,
	"%s %s: %s":             true,
	"  %s: %s%s":            true,
	"  * %s %s%s":           true,
	"★ %s %s: %s%s":         true,
	"ui-%d-%d":              true,
	"%s %7s  %s":            true,
	"%s %7s  %s — %s":       true,
	"%s %7s  %s — %s%s":     true,
	"%.1fM":                 true,
	"%.1f%c":                true,
	"%5s":                   true,
	"%d%c":                  true,
	"%.1fk":                 true,
	"%s | [%s] ":            true,
	" %s | [%s] ":           true,
	" [%s] ":                true,
	" | %s · %s ":           true,
	" | %s · %s · %s ":      true,
	" | %s · %s · %s · %s ": true,
	"%s\n":                  true,
	"%v\n":                  true,
	"%q":                    true,
	"  [%d] %s / %s\n":      true,

	// Keyboard shortcut names and keys
	"up":         true,
	"down":       true,
	"pgup":       true,
	"pgdown":     true,
	"home":       true,
	"end":        true,
	"enter":      true,
	"tab":        true,
	"esc":        true,
	"ctrl+q":     true,
	"ctrl+f":     true,
	"ctrl+s":     true,
	"ctrl+r":     true,
	"ctrl+t":     true,
	"ctrl+x":     true,
	"ctrl+w":     true,
	"ctrl+h":     true,
	"ctrl+o":     true,
	"ctrl+p":     true,
	"alt+up":     true,
	"alt+down":   true,
	"ctrl+left":  true,
	"ctrl+right": true,
	"alt+left":   true,
	"alt+right":  true,
	"ctrl+l":     true,
	"space":      true,
	"+":          true,
	"-":          true,
	"?":          true,
	"f1":         true,
	"f":          true,
	"q":          true,
	"j":          true,
	"k":          true,
	"l":          true,
	"i":          true,
	"y":          true,
	"c":          true,
	"r":          true,
	"G":          true,
	"=":          true,
	"_":          true,
	// LayoutMode identifiers (internal; the screen shows layout.mode.* labels)
	"split": true,
	"focus": true,

	// Twitch, IRC, browser, and external tool protocols / tokens
	// IRC badge identifiers; displayed labels come from chat.author.*.
	"moderator":             true,
	"broadcaster":           true,
	"vip":                   true,
	"#":                     true,
	"chat_login":            true,
	"chat:read":             true,
	"chat:edit":             true,
	"oauth:":                true,
	"Default":               true,
	"https://www.twitch.tv": true,
	"id.twitch.tv":          true,
	"libsecret":             true,
	"secret-tool":           true,
	"sqlite3":               true,
	"twt":                   true,
	"tmux":                  true,
	"TMUX":                  true,
	"load-buffer":           true,
	"-w":                    true,
	"SSH_TTY":               true,
	"SSH_CONNECTION":        true,
	"wl-copy":               true,
	"xclip":                 true,
	"OSC 52":                true,
	"-selection":            true,
	"clipboard":             true,
	"WAYLAND_DISPLAY":       true,
	"DISPLAY":               true,
	"NO_COLOR":              true,
	"CLICOLOR":              true,
	"CLICOLOR_FORCE":        true,
	"TERM":                  true,
	"dumb":                  true,
	"0":                     true,
	"moderation":            true,
	"reconnect_gap":         true,
	"%7s":                   true,
	"%d/%d":                 true,
	// help.colors legend term matching (not user-facing copy; paints i18n text)
	"(?i)vermelho|amarelo|magenta|mention|menção|yellow|green|verde|ciano|cyan|red": true,
	"amarelo":  true,
	"vermelho": true,
	"magenta":  true,
	"menção":   true,
	"mention":  true,
	"yellow":   true,
	"green":    true,
	"verde":    true,
	"ciano":    true,
	"cyan":     true,
	"red":      true,

	// CLI binary, command, and flag names
	"ttv-tui":             true,
	"login":               true,
	"doctor":              true,
	"ttv-tui doctor":      true,
	"version":             true,
	"help":                true,
	"h":                   true,
	"lang":                true,
	"import":              true,
	"from-browser":        true,
	"browser":             true,
	"profile":             true,
	"b":                   true,
	"p":                   true,
	"ttv-tui login":       true,
	"ttv-tui %s\n":        true,
	"ttv-tui: %s":         true,
	"ttv-tui login: %s":   true,
	"ttv-tui: %v\n":       true,
	"ttv-tui login: %v\n": true,
	"--lang":              true,
	"terminal":            true,
	"-lang":               true,
	"--lang=":             true,
	"-lang=":              true,

	// Internal model modes, item types, and playback states
	"categories":    true,
	"channels":      true,
	"search":        true,
	"list":          true,
	"chat":          true,
	"Categoria":     true,
	"Canal":         true,
	"CanalFavorito": true,
	"CanalRecente":  true,
	"CanalExato":    true,
	"iniciando":     true,
	"tocando":       true,
	"pausado":       true,
	"intervalo":     true,
	"erro":          true,
	"encerrado":     true,
	// Streamlink log severity token; protocol data, never localized.
	"[info]": true,
	// Player failure classes (mirror internal/player.Failure)
	"offline":            true,
	"network":            true,
	"timeout":            true,
	"streamlink_missing": true,
	"mpv_missing":        true,
	"no_plugin":          true,
	"unknown":            true,
	"dev":                true,

	// OS identifiers
	"windows": true,
	"darwin":  true,
	"linux":   true,

	// Raised when the embedded catalogs themselves fail to load: no catalog
	// exists to localize it.
	"initialize i18n catalog: %w": true,

	// Auth error operation names
	"validate": true,
	"find":     true,
}

func TestNoLooseStringsInUIAndLogin(t *testing.T) {
	cat, err := New(LangEN)
	if err != nil {
		t.Fatalf("New(en): %v", err)
	}
	enCatalog := cat.messages[LangEN]

	// Determine repo paths to scan relative to internal/i18n
	repoRoot := filepath.Clean(filepath.Join(".", "..", ".."))
	var targetFiles []string

	// 1. internal/ui/*.go (non-test)
	uiDir := filepath.Join(repoRoot, "internal", "ui")
	entries, err := os.ReadDir(uiDir)
	if err != nil {
		t.Fatalf("read internal/ui dir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			targetFiles = append(targetFiles, filepath.Join(uiDir, name))
		}
	}

	// 2. internal/app/login.go
	targetFiles = append(targetFiles, filepath.Join(repoRoot, "internal", "app", "login.go"))

	// 3. internal/app/app.go (errors that reach the terminal)
	targetFiles = append(targetFiles, filepath.Join(repoRoot, "internal", "app", "app.go"))

	// 4. cmd/ttv-tui/main.go
	targetFiles = append(targetFiles, filepath.Join(repoRoot, "cmd", "ttv-tui", "main.go"))

	fset := token.NewFileSet()
	for _, file := range targetFiles {
		node, parseErr := parser.ParseFile(fset, file, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", file, parseErr)
		}

		ast.Inspect(node, func(n ast.Node) bool {
			// Skip import specs
			if _, isImport := n.(*ast.ImportSpec); isImport {
				return false
			}

			lit, isLit := n.(*ast.BasicLit)
			if !isLit || lit.Kind != token.STRING {
				return true
			}

			val, unquoteErr := strconv.Unquote(lit.Value)
			if unquoteErr != nil {
				return true
			}

			// Catalog keys are valid
			if enCatalog[val] != "" {
				return true
			}

			// Allowed technical exceptions
			if allowedExceptions[val] {
				return true
			}

			rel, _ := filepath.Rel(repoRoot, file)
			pos := fset.Position(lit.Pos())
			t.Errorf("loose string literal %q found in %s:%d; all user-visible strings must come from the i18n catalog", val, rel, pos.Line)
			return true
		})
	}
}
