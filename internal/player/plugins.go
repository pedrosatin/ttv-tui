package player

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Plugin is the source of a streamlink plugin module the session sideloads.
// A Name equal to a built-in plugin's replaces that plugin for the playback.
type Plugin struct {
	// Name is the module name, without ".py".
	Name   string
	Source []byte
}

// pluginDirName is the directory, inside the session directory, that streamlink
// receives in --plugin-dir. It is created per session and removed with it.
const pluginDirName = "streamlink-plugins"

// maxPluginNameLen bounds a plugin module name.
const maxPluginNameLen = 64

// checkPluginName accepts what streamlink can load as a module of
// streamlink.plugins and nothing that is a path: lowercase ASCII letters, digits
// and underscores, starting with a letter.
func checkPluginName(name string) error {
	if name == "" || len(name) > maxPluginNameLen {
		return fmt.Errorf("player: plugin name %q: want 1 to %d characters", name, maxPluginNameLen)
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
		case i > 0 && (c >= '0' && c <= '9' || c == '_'):
		default:
			return fmt.Errorf("player: plugin name %q: only a-z, 0-9 and _, starting with a letter", name)
		}
	}
	return nil
}

// writePlugins writes plugins to a new directory inside sessionDir and returns
// it. Python runs these files, so the session directory has to be private, the
// plugin directory must not exist yet, and each file is created fresh (0600)
// rather than opened through whatever was left at the path. On error nothing is
// left behind and the session plays with streamlink's own plugins. No plugins
// returns "" and touches nothing.
func writePlugins(sessionDir string, plugins []Plugin) (string, error) {
	if len(plugins) == 0 {
		return "", nil
	}
	seen := make(map[string]bool, len(plugins))
	for _, p := range plugins {
		if err := checkPluginName(p.Name); err != nil {
			return "", err
		}
		if seen[p.Name] {
			return "", fmt.Errorf("player: plugin %q given twice", p.Name)
		}
		seen[p.Name] = true
		if len(p.Source) == 0 {
			return "", fmt.Errorf("player: plugin %q has no source", p.Name)
		}
	}
	if err := checkPrivateDir(sessionDir); err != nil {
		return "", err
	}
	dir := filepath.Join(sessionDir, pluginDirName)
	if err := os.Mkdir(dir, 0o700); err != nil {
		return "", fmt.Errorf("player: create plugin directory: %w", err)
	}
	if err := writePluginFiles(dir, plugins); err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	return dir, nil
}

func writePluginFiles(dir string, plugins []Plugin) error {
	// Mkdir applies the umask; the directory has to end up private on Unix.
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("player: plugin directory: %w", err)
	}
	for _, p := range plugins {
		path := filepath.Join(dir, p.Name+".py")
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return fmt.Errorf("player: create plugin %q: %w", p.Name, err)
		}
		_, err = f.Write(p.Source)
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return fmt.Errorf("player: write plugin %q: %w", p.Name, err)
		}
	}
	return nil
}

// pluginDirArgs is the streamlink argument that loads the session's plugins.
// --plugin-dir exists since streamlink 6.7.0, before the kick plugin (6.8.0);
// the older --plugin-dirs is deprecated and prints a warning.
func pluginDirArgs(dir string) []string {
	if dir == "" {
		return nil
	}
	return []string{"--plugin-dir", dir}
}

// pluginFallbackMarker opens the warning a sideloaded ttv-tui plugin logs when
// it cannot extend streamlink's own plugin and steps aside for it.
const pluginFallbackMarker = "ttv-tui plugin fallback: "

// isPluginFallbackLine reports the whole warning line a ttv-tui plugin logs
// when it steps aside: "[plugins.<name>][warning] ttv-tui plugin fallback: ...".
// The video still plays through the built-in plugin, so the line goes to the
// exit log and is not a failure of the stream or of streamlink.
func isPluginFallbackLine(line string) bool {
	rest, ok := strings.CutPrefix(line, "[plugins.")
	if !ok {
		return false
	}
	name, rest, ok := strings.Cut(rest, "]")
	if !ok || checkPluginName(name) != nil {
		return false
	}
	return strings.HasPrefix(rest, "[warning] "+pluginFallbackMarker)
}
