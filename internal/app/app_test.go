package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/auth"
	"github.com/pedrosatin/ttv-tui/internal/config"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/store"
	"github.com/pedrosatin/ttv-tui/internal/ui"
)

func TestTokenCheckMapsValidationErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want error
	}{
		{"valid token", nil, nil},
		{"validation timeout", auth.ErrValidateTimeout, nil},
		{"wrapped validation timeout", fmt.Errorf("validate: %w", auth.ErrValidateTimeout), nil},
		{"deadline exceeded", context.DeadlineExceeded, nil},
		{"wrapped deadline exceeded", fmt.Errorf("get: %w", context.DeadlineExceeded), nil},
		{"canceled", context.Canceled, nil},
		{"wrapped canceled", fmt.Errorf("get: %w", context.Canceled), nil},
		{"rejected token", auth.ErrTokenInvalid, auth.ErrTokenInvalid},
		{"wrapped rejected token", fmt.Errorf("validate: %w", auth.ErrTokenInvalid), auth.ErrTokenInvalid},
		{"any other error", errors.New("502 bad gateway"), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotToken string
			var hadDeadline bool
			check := tokenCheck(func(ctx context.Context, token string) (auth.Validation, error) {
				gotToken = token
				_, hadDeadline = ctx.Deadline()
				return auth.Validation{}, tc.err
			}, "tok-abc")
			if got := check(context.Background()); got != tc.want {
				t.Fatalf("check() = %v, want %v", got, tc.want)
			}
			if gotToken != "tok-abc" {
				t.Fatalf("validated token %q, want the saved one", gotToken)
			}
			if !hadDeadline {
				t.Fatal("validation ran without a deadline")
			}
		})
	}
}

// loginElsewhere stands for `ttv-tui login` in another terminal: it rewrites
// the config with a token this run never read.
func loginElsewhere(t *testing.T, path string) {
	t.Helper()
	cfg, err := config.LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AccessToken, cfg.Username = "tokenfromotherterminal", "latecomer"
	if err := config.SaveTo(path, cfg); err != nil {
		t.Fatal(err)
	}
}

// savedConfig reads the config back as text, so one comparison covers every field.
func savedConfig(t *testing.T, path string) string {
	t.Helper()
	cfg, err := config.LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%+v", cfg)
}

func TestConfigWriterRereadsBeforeSaving(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	start := config.Config{Language: "en", AccessToken: "tokenatstartup", Username: "early", MinViewers: 10}
	if err := config.SaveTo(path, start); err != nil {
		t.Fatal(err)
	}
	w := &configWriter{path: path}

	// A panel width change after a login in another terminal keeps that login.
	loginElsewhere(t, path)
	if err := w.saveColumns(30, 40, true, true, 70); err != nil {
		t.Fatalf("saveColumns: %v", err)
	}
	want := config.Config{
		Language: "en", AccessToken: "tokenfromotherterminal", Username: "latecomer", MinViewers: 10,
		ListColumns: 30, ChatColumns: 40, ChatOverlay: true, PanelsSwapped: true, ChatOverlayOpacity: 70,
	}
	if got := savedConfig(t, path); got != fmt.Sprintf("%+v", want) {
		t.Fatalf("after saveColumns:\n got %s\nwant %+v", got, want)
	}

	// The next save starts from the file again, not from what the first one kept.
	want.AccessToken = "tokenrenewed"
	if err := config.SaveTo(path, want); err != nil {
		t.Fatal(err)
	}
	if err := w.saveFilters([]string{"pt"}, 500); err != nil {
		t.Fatalf("saveFilters: %v", err)
	}
	want.FilterLanguages, want.MinViewers = []string{"pt"}, 500
	if got := savedConfig(t, path); got != fmt.Sprintf("%+v", want) {
		t.Fatalf("after saveFilters:\n got %s\nwant %+v", got, want)
	}
}

func TestConfigWriterReportsARemovedConfigOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := config.SaveTo(path, config.Config{Language: "en", AccessToken: "tokenatstartup"}); err != nil {
		t.Fatal(err)
	}
	w := &configWriter{path: path, existed: true}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := w.saveColumns(30, 40, false, false, 70); !errors.Is(err, ui.ErrConfigRemoved) {
		t.Fatalf("first save after the removal = %v, want ui.ErrConfigRemoved", err)
	}
	if err := w.saveFilters([]string{"pt"}, 0); err != nil {
		t.Fatalf("second save = %v, want nil", err)
	}
	got, err := config.LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.HasToken() || got.ListColumns != 30 || !slices.Equal(got.FilterLanguages, []string{"pt"}) {
		t.Fatalf("saved config = %+v, want both changes and no token", got)
	}
}

// twoLanguageSource offers one category whose channels speak two languages, so
// the filter menu has something to toggle.
type twoLanguageSource struct{}

func (twoLanguageSource) Categories(context.Context, catalog.PageRequest) (catalog.Page[catalog.Category], error) {
	return catalog.Page[catalog.Category]{Items: []catalog.Category{{ID: "cat", Name: "Cat"}}}, nil
}

func (twoLanguageSource) Channels(context.Context, string, catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
	return catalog.Page[catalog.Channel]{Items: []catalog.Channel{
		{ID: "pt", Login: "ptlive", DisplayName: "PtLive", Language: catalog.LanguagePtr("PT"), Viewers: catalog.IntPtr(750), Availability: catalog.AvailabilityLive},
		{ID: "en", Login: "enlive", DisplayName: "EnLive", Language: catalog.LanguagePtr("EN"), Viewers: catalog.IntPtr(2000), Availability: catalog.AvailabilityLive},
	}}, nil
}

func (twoLanguageSource) Search(context.Context, string) (catalog.Page[catalog.Channel], error) {
	return catalog.Page[catalog.Channel]{}, nil
}

func (twoLanguageSource) SearchCategories(context.Context, string) ([]catalog.Category, error) {
	return nil, nil
}

func (twoLanguageSource) ChannelsByLogins(context.Context, []string) ([]catalog.Channel, error) {
	return nil, nil
}

func (twoLanguageSource) ChannelsByIDs(context.Context, []string) ([]catalog.Channel, error) {
	return nil, nil
}

// settle feeds msg to the model and then every message its commands produce.
// A command that does not answer in time is a timer and is left behind.
func settle(t *testing.T, m tea.Model, msg tea.Msg) tea.Model {
	t.Helper()
	m, cmd := m.Update(msg)
	return runCmds(t, m, cmd)
}

func runCmds(t *testing.T, m tea.Model, cmds ...tea.Cmd) tea.Model {
	t.Helper()
	for len(cmds) > 0 {
		cmd := cmds[0]
		cmds = cmds[1:]
		if cmd == nil {
			continue
		}
		answer := make(chan tea.Msg, 1)
		go func() { answer <- cmd() }()
		var msg tea.Msg
		select {
		case msg = <-answer:
		case <-time.After(2 * time.Second):
			continue
		}
		switch msg := msg.(type) {
		case nil:
		case tea.BatchMsg:
			cmds = append(cmds, msg...)
		default:
			var next tea.Cmd
			m, next = m.Update(msg)
			cmds = append(cmds, next)
		}
	}
	return m
}

// toggleEnglishFilter opens the only category, then the filter menu, and turns
// the EN language on, as a user would.
func toggleEnglishFilter(t *testing.T, opts []ui.Option) ui.Model {
	t.Helper()
	var m tea.Model = ui.New(nil, append(opts, ui.WithSource(twoLanguageSource{}))...)
	m = runCmds(t, m, m.Init())
	m = settle(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = settle(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = settle(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	if !m.(ui.Model).ChannelFilterMenuOpen() {
		t.Fatal("filter menu did not open")
	}
	m = settle(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = settle(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	model := m.(ui.Model)
	if got := model.FilterLanguages(); !slices.Equal(got, []string{"en"}) {
		t.Fatalf("filter after toggling EN = %v, want [en]", got)
	}
	return model
}

func TestConfigOptionsWithPathSaveWhatTheUIChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := config.Config{Language: "en", AccessToken: "tokenatstartup", ListColumns: 30}
	if err := config.SaveTo(path, cfg); err != nil {
		t.Fatal(err)
	}

	opts := configOptions(cfg, path)
	if len(opts) != 2 {
		t.Fatalf("got %d options, want the channel filters and the panel layout", len(opts))
	}
	loginElsewhere(t, path)
	toggleEnglishFilter(t, opts)

	got, err := config.LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.FilterLanguages, []string{"en"}) {
		t.Fatalf("filter change not saved: %+v", got)
	}
	if got.AccessToken != "tokenfromotherterminal" {
		t.Fatalf("the filter change undid the login: token %q", got.AccessToken)
	}
	if got.ListColumns != 30 || got.Language != "en" {
		t.Fatalf("unrelated settings changed: %+v", got)
	}
}

// A config removed while the app runs is a logout: the next save writes the
// change without the token and the status bar says the session is gone.
func TestConfigOptionsWarnWhenTheConfigWasRemoved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := config.Config{Language: "en", AccessToken: "tokenatstartup", Username: "early", ListColumns: 30}
	if err := config.SaveTo(path, cfg); err != nil {
		t.Fatal(err)
	}
	opts := configOptions(cfg, path)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	m := toggleEnglishFilter(t, opts)

	cat, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := m.StatusNotice(), cat.T("config.removed"); got != want {
		t.Fatalf("status notice = %q, want %q", got, want)
	}
	want := config.Default()
	want.FilterLanguages = []string{"en"}
	if got := savedConfig(t, path); got != fmt.Sprintf("%+v", want) {
		t.Fatalf("after the save:\n got %s\nwant %+v", got, want)
	}
}

// Without a config at startup the first save creates it, with no notice.
func TestConfigOptionsCreateAMissingConfigQuietly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	m := toggleEnglishFilter(t, configOptions(config.Config{Language: "en"}, path))
	if got := m.StatusNotice(); got != "" {
		t.Fatalf("status notice = %q, want none", got)
	}
	got, err := config.LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.FilterLanguages, []string{"en"}) {
		t.Fatalf("filter change not saved: %+v", got)
	}
}

func TestConfigOptionsWithoutPathNeverWrite(t *testing.T) {
	// Any write would land here: the default config path points into this
	// directory, which has to stay empty.
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Chdir(dir)

	t.Run("no saved filters", func(t *testing.T) {
		opts := configOptions(config.Config{ListColumns: 30}, "")
		if len(opts) != 1 {
			t.Fatalf("got %d options, want only the panel layout", len(opts))
		}
		if m := ui.New(nil, opts...); m.FilterLanguages() != nil || m.FilterMinViewers() != 0 {
			t.Fatalf("filters = %v/%d, want none", m.FilterLanguages(), m.FilterMinViewers())
		}
		toggleEnglishFilter(t, opts)
	})

	t.Run("saved languages", func(t *testing.T) {
		opts := configOptions(config.Config{FilterLanguages: []string{"pt"}}, "")
		if len(opts) != 2 {
			t.Fatalf("got %d options, want the channel filters and the panel layout", len(opts))
		}
		if m := ui.New(nil, opts...); !slices.Equal(m.FilterLanguages(), []string{"pt"}) {
			t.Fatalf("languages = %v, want [pt]", m.FilterLanguages())
		}
	})

	t.Run("saved audience floor", func(t *testing.T) {
		opts := configOptions(config.Config{MinViewers: 100}, "")
		if len(opts) != 2 {
			t.Fatalf("got %d options, want the channel filters and the panel layout", len(opts))
		}
		if m := ui.New(nil, opts...); m.FilterMinViewers() != 100 {
			t.Fatalf("min viewers = %d, want 100", m.FilterMinViewers())
		}
	})

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("something was written without a config path: %v", entries)
	}
}

// brokenConfig is a config.toml that exists but that LoadFrom rejects. apply
// breaks a valid config at path and returns the bytes that have to survive.
type brokenConfig struct {
	name  string
	apply func(t *testing.T, path string) []byte
}

var brokenConfigs = []brokenConfig{
	{"unreadable", func(t *testing.T, path string) []byte {
		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("permissão 000 não impede leitura como root ou no Windows")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
		return data
	}},
	{"invalid syntax", func(t *testing.T, path string) []byte {
		data := []byte("access_token = \"token-in-broken-file\nlanguage = [\n")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return data
	}},
}

// breakConfig saves a valid config at path and breaks it as b says.
func breakConfig(t *testing.T, path string, b brokenConfig) []byte {
	t.Helper()
	if err := config.SaveTo(path, config.Config{Language: "en", AccessToken: "tokenatstartup", MinViewers: 10}); err != nil {
		t.Fatal(err)
	}
	return b.apply(t, path)
}

// assertUntouched checks that path still holds want, byte for byte.
func assertUntouched(t *testing.T, path string, want []byte) {
	t.Helper()
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("config was rewritten:\n got %q\nwant %q", got, want)
	}
}

func TestConfigWriterLeavesAConfigItCannotReadAlone(t *testing.T) {
	for _, b := range brokenConfigs {
		t.Run(b.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			want := breakConfig(t, path, b)
			w := &configWriter{path: path}
			if err := w.saveColumns(30, 40, true, true, 70); err == nil {
				t.Fatal("saveColumns over a config it could not read reported success")
			}
			if err := w.saveFilters([]string{"pt"}, 500); err == nil {
				t.Fatal("saveFilters over a config it could not read reported success")
			}
			assertUntouched(t, path, want)
		})
	}
}

func TestConfigOptionsWarnWhenTheConfigCannotBeRead(t *testing.T) {
	for _, b := range brokenConfigs {
		t.Run(b.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			want := breakConfig(t, path, b)
			m := toggleEnglishFilter(t, configOptions(config.Config{Language: "en", AccessToken: "tokenatstartup"}, path))
			cat, err := i18n.New(i18n.LangEN)
			if err != nil {
				t.Fatal(err)
			}
			if got, wantNotice := m.StatusNotice(), cat.T("config.save_failed"); got != wantNotice {
				t.Fatalf("status notice = %q, want %q", got, wantNotice)
			}
			assertUntouched(t, path, want)
		})
	}
}

func TestPersistLangRereadsBeforeSaving(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	start := config.Config{Language: "en", AccessToken: "tokenatstartup", Username: "early", MinViewers: 10}
	if err := config.SaveTo(path, start); err != nil {
		t.Fatal(err)
	}
	cfg := start
	loginElsewhere(t, path)
	if err := persistLang("pt-BR", &cfg, path); err != nil {
		t.Fatalf("persistLang: %v", err)
	}
	if cfg.Language != i18n.LangPTBR {
		t.Fatalf("cfg.Language = %q, want %q", cfg.Language, i18n.LangPTBR)
	}
	want := config.Config{Language: i18n.LangPTBR, AccessToken: "tokenfromotherterminal", Username: "latecomer", MinViewers: 10}
	if got := savedConfig(t, path); got != fmt.Sprintf("%+v", want) {
		t.Fatalf("after persistLang:\n got %s\nwant %+v", got, want)
	}
}

func TestPersistLangLeavesAConfigItCannotReadAlone(t *testing.T) {
	for _, b := range brokenConfigs {
		t.Run(b.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			want := breakConfig(t, path, b)
			cfg := config.Config{Language: "en", AccessToken: "tokenatstartup", MinViewers: 10}
			if err := persistLang("pt-BR", &cfg, path); err == nil {
				t.Fatal("persistLang over a config it could not read reported success")
			}
			if cfg.Language != i18n.LangPTBR {
				t.Fatalf("cfg.Language = %q, want %q", cfg.Language, i18n.LangPTBR)
			}
			assertUntouched(t, path, want)
		})
	}
}

func TestLoadStateWarnsWhenTheStoreCannotBeRead(t *testing.T) {
	cat, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("missing file", func(t *testing.T) {
		st, notice := loadState(store.New(filepath.Join(t.TempDir(), "state.json")), cat)
		if notice != "" || len(st.Favorites) != 0 {
			t.Fatalf("missing store: notice %q, favorites %v", notice, st.Favorites)
		}
	})

	t.Run("unreadable file", func(t *testing.T) {
		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("permissão 000 não impede leitura como root ou no Windows")
		}
		path := filepath.Join(t.TempDir(), "state.json")
		saved := store.Channel{ID: "1", Login: "saved", DisplayName: "Saved"}
		if err := store.SaveTo(path, store.State{Favorites: []store.Channel{saved}}); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

		appStore := store.New(path)
		st, notice := loadState(appStore, cat)
		if want := cat.T("store.load_failed_notice"); notice != want {
			t.Fatalf("notice = %q, want %q", notice, want)
		}
		if notice == cat.T("store.corrupt_notice") {
			t.Fatal("a store that could not be read is not a corrupt one")
		}

		// A favorite added while the file is still unreadable is not saved.
		added := store.Channel{ID: "2", Login: "added", DisplayName: "Added"}
		st.ToggleFavorite(added)
		if err := appStore.Save(st); err == nil {
			t.Fatal("saving over a store that could not be read reported success")
		}
		// Once the file is readable again, the next save keeps what it held.
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := appStore.Save(st); err != nil {
			t.Fatalf("save after the store became readable: %v", err)
		}
		got, err := store.LoadFrom(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Favorites) != 2 || !got.Favorites[0].Matches(saved) || !got.Favorites[1].Matches(added) {
			t.Fatalf("favorites on disk = %+v, want the saved one and the added one", got.Favorites)
		}
	})
}

// O carregamento descarta o recente de login inválido e a saída registra só a
// contagem, sem o login recusado nem byte de controle.
func TestLogDiscardedChannelsSaiSoComAContagem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	const fixture = `{"favorites":[],"recents":[
		{"platform":"twitch","id":"x","login":"dummychan\r\nprivmsg #victimchan :y\u001b[2J"},
		{"platform":"twitch","id":"gaules","login":"gaules"}
	]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	for lang, want := range map[string]string{
		i18n.LangEN:   "ttv-tui: favorites and recents dropped for an invalid identifier: 1\n",
		i18n.LangPTBR: "ttv-tui: favoritos e recentes descartados por identificador inválido: 1\n",
	} {
		cat, err := i18n.New(lang)
		if err != nil {
			t.Fatal(err)
		}
		st, notice := loadState(store.New(path), cat)
		if notice != "" || len(st.Recents) != 1 || st.Recents[0].Login != "gaules" {
			t.Fatalf("%s: notice %q, recentes %+v", lang, notice, st.Recents)
		}
		var log bytes.Buffer
		logDiscardedChannels(&log, st.Discarded(), cat)
		if log.String() != want {
			t.Fatalf("%s: log = %q want %q", lang, log.String(), want)
		}
	}
	var log bytes.Buffer
	logDiscardedChannels(&log, 0, nil)
	if log.Len() != 0 {
		t.Fatalf("sem descarte o log fica vazio, veio %q", log.String())
	}
}

func TestConfigWriterDoesNotWriteStderr(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := config.SaveTo(path, config.Config{}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = old }()
	writer := &configWriter{path: path, existed: true}
	updateErr := writer.saveFilters([]string{"pt"}, 10)
	_ = w.Close()
	output, readErr := io.ReadAll(r)
	_ = r.Close()
	if updateErr != nil || readErr != nil {
		t.Fatal("update failed")
	}
	if len(output) != 0 {
		t.Fatal("update wrote to stderr")
	}
}

func TestConfigPermissionWarning(t *testing.T) {
	for _, lang := range []string{"en", "pt-BR"} {
		cat, err := i18n.New(lang)
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			notice config.PermissionNotice
			key    string
		}{
			{config.PermissionNotice{}, ""},
			{config.PermissionNotice{Fixed: true}, "app.warn_config_permissions_fixed"},
			{config.PermissionNotice{Failed: true}, "app.warn_config_permissions_failed"},
			{config.PermissionNotice{Fixed: true, Failed: true}, "app.warn_config_permissions_failed"},
		} {
			var log bytes.Buffer
			logConfigPermissions(&log, "/config.toml", tc.notice, cat)
			want := ""
			if tc.key != "" {
				want = fmt.Sprintf(cat.T(tc.key), "/config.toml") + "\n"
			}
			if log.String() != want {
				t.Fatal("aviso de permissões inesperado")
			}
		}
	}
}

func TestRunReportsPermissionsBeforeTerminalCheck(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permissões Unix")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := config.SaveTo(path, config.Config{}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	var log, output bytes.Buffer
	err := Run(Options{ConfigPath: path, Lang: "en", Terminal: true, Input: &bytes.Buffer{}, Output: &output, Log: &log})
	if err == nil {
		t.Fatal("expected terminal check to stop startup")
	}
	cat, err := i18n.New("en")
	if err != nil {
		t.Fatal(err)
	}
	if log.String() != fmt.Sprintf(cat.T("app.warn_config_permissions_fixed"), path)+"\n" {
		t.Fatal("startup warning missing or repeated")
	}
}

// TestRunVideoHostChildDoesNotSecure verifica o fluxo real do filho sem conectar ao host.
func TestRunVideoHostChildDoesNotSecure(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv(videoHostEnv, filepath.Join(t.TempDir(), "host-ausente"))
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := config.SaveTo(path, config.Config{}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	var log, output bytes.Buffer
	err := Run(Options{ConfigPath: path, Terminal: true, Input: &bytes.Buffer{}, Output: &output, Log: &log})
	if err == nil || log.Len() != 0 {
		t.Fatal("filho deve parar no terminal sem emitir aviso de permissões")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o644 {
			t.Fatal("filho restringiu o config novamente")
		}
	}
}
