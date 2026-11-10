package app

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/pedrosatin/ttv-tui/internal/twitch"
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
		{"any other error", errors.New("502 bad gateway"), auth.ErrTokenInvalid},
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
	cfg.AccessToken, cfg.Username = "token-from-other-terminal", "latecomer"
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
	start := config.Config{Language: "en", AccessToken: "token-at-startup", Username: "early", MinViewers: 10}
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
		Language: "en", AccessToken: "token-from-other-terminal", Username: "latecomer", MinViewers: 10,
		ListColumns: 30, ChatColumns: 40, ChatOverlay: true, PanelsSwapped: true, ChatOverlayOpacity: 70,
	}
	if got := savedConfig(t, path); got != fmt.Sprintf("%+v", want) {
		t.Fatalf("after saveColumns:\n got %s\nwant %+v", got, want)
	}

	// The next save starts from the file again, not from what the first one kept.
	want.AccessToken = "token-renewed"
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

// twoLanguageSource offers one category whose channels speak two languages, so
// the filter menu has something to toggle.
type twoLanguageSource struct{}

func (twoLanguageSource) Categories(context.Context, twitch.PageRequest) (twitch.Page[twitch.Category], error) {
	return twitch.Page[twitch.Category]{Items: []twitch.Category{{ID: "cat", Name: "Cat"}}}, nil
}

func (twoLanguageSource) Channels(context.Context, string, twitch.PageRequest) (twitch.Page[twitch.Channel], error) {
	return twitch.Page[twitch.Channel]{Items: []twitch.Channel{
		{ID: "pt", Login: "ptlive", DisplayName: "PtLive", Language: twitch.LanguagePtr("PT"), Viewers: twitch.IntPtr(750), Availability: twitch.AvailabilityLive},
		{ID: "en", Login: "enlive", DisplayName: "EnLive", Language: twitch.LanguagePtr("EN"), Viewers: twitch.IntPtr(2000), Availability: twitch.AvailabilityLive},
	}}, nil
}

func (twoLanguageSource) Search(context.Context, string) (twitch.Page[twitch.Channel], error) {
	return twitch.Page[twitch.Channel]{}, nil
}

func (twoLanguageSource) SearchCategories(context.Context, string) ([]twitch.Category, error) {
	return nil, nil
}

func (twoLanguageSource) ChannelsByLogins(context.Context, []string) ([]twitch.Channel, error) {
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
	cfg := config.Config{Language: "en", AccessToken: "token-at-startup", ListColumns: 30}
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
	if got.AccessToken != "token-from-other-terminal" {
		t.Fatalf("the filter change undid the login: token %q", got.AccessToken)
	}
	if got.ListColumns != 30 || got.Language != "en" {
		t.Fatalf("unrelated settings changed: %+v", got)
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
			t.Skip("permission 000 does not stop root or Windows from reading")
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
	if err := config.SaveTo(path, config.Config{Language: "en", AccessToken: "token-at-startup", MinViewers: 10}); err != nil {
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
			m := toggleEnglishFilter(t, configOptions(config.Config{Language: "en", AccessToken: "token-at-startup"}, path))
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
	start := config.Config{Language: "en", AccessToken: "token-at-startup", Username: "early", MinViewers: 10}
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
	want := config.Config{Language: i18n.LangPTBR, AccessToken: "token-from-other-terminal", Username: "latecomer", MinViewers: 10}
	if got := savedConfig(t, path); got != fmt.Sprintf("%+v", want) {
		t.Fatalf("after persistLang:\n got %s\nwant %+v", got, want)
	}
}

func TestPersistLangLeavesAConfigItCannotReadAlone(t *testing.T) {
	for _, b := range brokenConfigs {
		t.Run(b.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			want := breakConfig(t, path, b)
			cfg := config.Config{Language: "en", AccessToken: "token-at-startup", MinViewers: 10}
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
			t.Skip("permission 000 does not stop root or Windows from reading")
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
