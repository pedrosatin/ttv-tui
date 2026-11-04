package auth

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Browser identifies a supported browser family.
type Browser string

const (
	BrowserChrome   Browser = "chrome"
	BrowserChromium Browser = "chromium"
	BrowserBrave    Browser = "brave"
	BrowserEdge     Browser = "edge"
	BrowserVivaldi  Browser = "vivaldi"
	BrowserOpera    Browser = "opera"
	BrowserFirefox  Browser = "firefox"
)

// Profile is an installed browser profile that may hold a twitch.tv auth-token cookie.
type Profile struct {
	Browser     Browser
	Name        string // Default, Profile 1, or Firefox profile name
	UserDataDir string // Chromium user-data root, or Firefox profiles root
	ProfileDir  string // absolute path to the profile directory
	CookiesPath string // absolute path to the Cookies / cookies.sqlite file
	Family      string // "chromium" or "firefox"
}

// browserSpec describes how to find a Chromium-family browser on Linux.
type browserSpec struct {
	Browser          Browser
	RelDir           string // under $XDG_CONFIG_HOME (or ~/.config)
	KeyringApp       string // secret-tool attribute "application"
	KeyringService   string // legacy "X Safe Storage"
	KeyringAccount   string
	SupportsProfiles bool
}

var chromiumBrowsers = []browserSpec{
	{
		Browser: BrowserBrave, RelDir: "BraveSoftware/Brave-Browser",
		KeyringApp: "brave", KeyringService: "Brave Safe Storage", KeyringAccount: "Brave",
		SupportsProfiles: true,
	},
	{
		Browser: BrowserChrome, RelDir: "google-chrome",
		KeyringApp: "chrome", KeyringService: "Chrome Safe Storage", KeyringAccount: "Chrome",
		SupportsProfiles: true,
	},
	{
		Browser: BrowserChromium, RelDir: "chromium",
		KeyringApp: "chromium", KeyringService: "Chromium Safe Storage", KeyringAccount: "Chromium",
		SupportsProfiles: true,
	},
	{
		Browser: BrowserEdge, RelDir: "microsoft-edge",
		KeyringApp: "microsoft-edge", KeyringService: "Chromium Safe Storage", KeyringAccount: "Chromium",
		SupportsProfiles: true,
	},
	{
		Browser: BrowserVivaldi, RelDir: "vivaldi",
		KeyringApp: "chrome", KeyringService: "Chrome Safe Storage", KeyringAccount: "Chrome",
		SupportsProfiles: true,
	},
	{
		Browser: BrowserOpera, RelDir: "opera",
		KeyringApp: "chromium", KeyringService: "Chromium Safe Storage", KeyringAccount: "Chromium",
		SupportsProfiles: false,
	},
}

func configHome() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return xdg, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home: %w", err)
	}
	return filepath.Join(home, ".config"), nil
}

func homeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home: %w", err)
	}
	return home, nil
}

// ListProfiles enumerates installed Chromium-family and Firefox profiles that
// have a cookies database on disk. Profiles without a Cookies file are skipped.
func ListProfiles() ([]Profile, error) {
	cfg, err := configHome()
	if err != nil {
		return nil, wrap("list", err, "não foi possível resolver o diretório de configuração")
	}
	home, err := homeDir()
	if err != nil {
		return nil, wrap("list", err, "não foi possível resolver o diretório home")
	}

	var out []Profile
	for _, spec := range chromiumBrowsers {
		userData := filepath.Join(cfg, spec.RelDir)
		profiles, err := listChromiumProfiles(spec, userData)
		if err != nil {
			return nil, err
		}
		out = append(out, profiles...)
	}

	ff, err := listFirefoxProfiles(home, cfg)
	if err != nil {
		return nil, err
	}
	out = append(out, ff...)
	return out, nil
}

func listChromiumProfiles(spec browserSpec, userData string) ([]Profile, error) {
	if _, err := os.Stat(userData); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, wrap("list", err, fmt.Sprintf("falha ao inspecionar %s", userData))
	}

	var names []string
	if !spec.SupportsProfiles {
		names = []string{""}
	} else {
		entries, err := os.ReadDir(userData)
		if err != nil {
			return nil, wrap("list", err, fmt.Sprintf("falha ao listar perfis em %s", userData))
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if name == "Default" || strings.HasPrefix(name, "Profile ") {
				names = append(names, name)
			}
		}
	}

	var out []Profile
	for _, name := range names {
		profileDir := userData
		displayName := "Default"
		if name != "" {
			profileDir = filepath.Join(userData, name)
			displayName = name
		}
		cookies := findChromiumCookies(profileDir)
		if cookies == "" {
			continue
		}
		out = append(out, Profile{
			Browser:     spec.Browser,
			Name:        displayName,
			UserDataDir: userData,
			ProfileDir:  profileDir,
			CookiesPath: cookies,
			Family:      "chromium",
		})
	}
	return out, nil
}

// findChromiumCookies returns the Cookies DB path, preferring Network/Cookies
// (newer Chromium) when present, otherwise the legacy Cookies file.
func findChromiumCookies(profileDir string) string {
	candidates := []string{
		filepath.Join(profileDir, "Network", "Cookies"),
		filepath.Join(profileDir, "Cookies"),
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

func listFirefoxProfiles(home, cfg string) ([]Profile, error) {
	roots := []string{
		filepath.Join(cfg, "mozilla", "firefox"),
		filepath.Join(home, ".mozilla", "firefox"),
		filepath.Join(home, ".var", "app", "org.mozilla.firefox", "config", "mozilla", "firefox"),
		filepath.Join(home, ".var", "app", "org.mozilla.firefox", ".mozilla", "firefox"),
		filepath.Join(home, "snap", "firefox", "common", ".mozilla", "firefox"),
	}
	var out []Profile
	seen := map[string]bool{}
	for _, root := range roots {
		profiles, err := firefoxProfilesFromRoot(root)
		if err != nil {
			return nil, err
		}
		for _, p := range profiles {
			if seen[p.CookiesPath] {
				continue
			}
			seen[p.CookiesPath] = true
			out = append(out, p)
		}
	}
	return out, nil
}

func firefoxProfilesFromRoot(root string) ([]Profile, error) {
	iniPath := filepath.Join(root, "profiles.ini")
	data, err := os.ReadFile(iniPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, wrap("list", err, fmt.Sprintf("falha ao ler %s", iniPath))
	}
	sections := parseINISections(string(data))
	var out []Profile
	for _, sec := range sections {
		if !strings.HasPrefix(sec.name, "Profile") {
			continue
		}
		path := sec.values["Path"]
		if path == "" {
			continue
		}
		var profileDir string
		if sec.values["IsRelative"] == "0" {
			profileDir = path
		} else {
			profileDir = filepath.Join(root, path)
		}
		cookies := filepath.Join(profileDir, "cookies.sqlite")
		if st, err := os.Stat(cookies); err != nil || st.IsDir() {
			continue
		}
		name := sec.values["Name"]
		if name == "" {
			name = filepath.Base(profileDir)
		}
		out = append(out, Profile{
			Browser:     BrowserFirefox,
			Name:        name,
			UserDataDir: root,
			ProfileDir:  profileDir,
			CookiesPath: cookies,
			Family:      "firefox",
		})
	}
	return out, nil
}

type iniSection struct {
	name   string
	values map[string]string
}

func parseINISections(input string) []iniSection {
	var sections []iniSection
	var cur *iniSection
	for _, line := range strings.Split(input, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			sections = append(sections, iniSection{
				name:   strings.TrimSpace(line[1 : len(line)-1]),
				values: map[string]string{},
			})
			cur = &sections[len(sections)-1]
			continue
		}
		if cur == nil {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		cur.values[strings.TrimSpace(line[:eq])] = strings.TrimSpace(line[eq+1:])
	}
	return sections
}

// FindProfile resolves a browser+profile name against ListProfiles.
// profile may be empty (first match for that browser) or a name such as "Default".
func FindProfile(browser, profile string) (Profile, error) {
	browser = strings.ToLower(strings.TrimSpace(browser))
	profile = strings.TrimSpace(profile)

	all, err := ListProfiles()
	if err != nil {
		return Profile{}, err
	}

	var matches []Profile
	for _, p := range all {
		if string(p.Browser) != browser {
			continue
		}
		if profile == "" || strings.EqualFold(p.Name, profile) {
			matches = append(matches, p)
		}
	}
	if len(matches) == 0 {
		if profile == "" {
			return Profile{}, &Error{
				Op:      "find",
				Err:     ErrBrowserNotFound,
				Message: fmt.Sprintf("nenhum perfil do navegador %q com cookies encontrado; rode list para ver o que há nesta máquina", browser),
			}
		}
		return Profile{}, &Error{
			Op:      "find",
			Err:     ErrProfileNotFound,
			Message: fmt.Sprintf("perfil %q do navegador %q não encontrado; rode list para ver os nomes disponíveis", profile, browser),
		}
	}
	return matches[0], nil
}

func specFor(browser Browser) (browserSpec, bool) {
	for _, s := range chromiumBrowsers {
		if s.Browser == browser {
			return s, true
		}
	}
	return browserSpec{}, false
}
