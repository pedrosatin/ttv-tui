package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/auth"
	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/config"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/player"
	"github.com/pedrosatin/ttv-tui/internal/store"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
	"github.com/pedrosatin/ttv-tui/internal/ui"
)

// Options configures a single invocation of the application.
type Options struct {
	Lang string
	// Terminal keeps the app in the terminal even where the window could open.
	Terminal   bool
	ConfigPath string
	StatePath  string
	Source     twitch.Source
	Validate   func(ctx context.Context, token string) (auth.Validation, error)
	Player     *player.Manager
	Chat       *chat.Manager
	Input      io.Reader
	Output     io.Writer
	// Log receives warnings printed outside the TUI (nil: os.Stderr).
	Log io.Writer
}

// earlyCatalog serves errors raised before the config is read and the final
// language is known: the language comes from the environment alone. A nil
// catalog is safe (T returns the key), so a broken embed never panics here.
func earlyCatalog() *i18n.Catalog {
	cat, _ := i18n.New(i18n.Resolve("", "", i18n.EnvLocale()))
	return cat
}

// Run composes config, i18n, Source, player, chat, and the UI model and runs the TUI program.
//
// Each stage lives in its own function, but every defer stays here: cleanup
// has to wait for the program to end, and its order is part of the shutdown.
func Run(opts Options) error {
	if opts.Lang != "" && !i18n.IsSupportedFlag(opts.Lang) {
		return fmt.Errorf(earlyCatalog().T("app.err_unsupported_lang"), opts.Lang, i18n.LangEN, i18n.LangPTBR)
	}

	cfg, cfgPath, err := loadConfig(opts.ConfigPath)
	if err != nil {
		return fmt.Errorf(earlyCatalog().T("app.err_load_config"), err)
	}
	if openWindow(opts) {
		return nil
	}
	var hostOption ui.Option
	if host := connectVideoHost(); host != nil {
		defer host.Close()
		hostOption = ui.WithVideoHost(host)
	}

	resolvedLang := i18n.Resolve(opts.Lang, cfg.Language, i18n.EnvLocale())
	catalog, err := i18n.New(resolvedLang)
	if err != nil {
		// No catalog exists to localize this one: the embedded files failed to load.
		return fmt.Errorf("initialize i18n catalog: %w", err)
	}

	input, output, logw := streams(opts)
	if !isTerminal(input) || !isTerminal(output) {
		return errors.New(catalog.T("app.err_tty_required"))
	}

	// Only after the terminal check: a run that cannot start must not write.
	if err := persistLang(opts.Lang, &cfg, cfgPath); err != nil {
		return fmt.Errorf(catalog.T("app.err_persist_lang"), err)
	}

	source := opts.Source
	if source == nil {
		source = twitch.NewGQLSource(twitch.GQLConfig{})
	}

	statePath, err := resolveStatePath(opts.StatePath)
	if err != nil {
		return fmt.Errorf(catalog.T("app.err_resolve_state_path"), err)
	}
	registryDir := playerRegistryDir(statePath)

	// A player left by a run that died without cleanup would play alongside
	// the next one; deal with it before anything can start playback.
	handleOrphans(registryDir, input, output, logw, isInteractive(input), catalog)

	// From here on a player may exist. Catch the termination signals ourselves
	// (Bubble Tea's handler ignores SIGHUP, whose default action would kill us
	// before the deferred cleanup) and turn them into an orderly shutdown. The
	// subscription is released last, so a second signal during cleanup is
	// absorbed instead of aborting it.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	caught := cancelOnSignal(ctx, cancel, sigCh)

	playerMgr, stopPlayer := playerManager(opts.Player, registryDir)
	defer func() {
		// After the last Stop: it can note a group that outlived SIGKILL.
		stopPlayer()
		logPlayerNotes(logw, playerMgr.Notes(), catalog)
	}()
	chatMgr, stopChat := chatManager(opts.Chat, cfg)
	defer stopChat()

	appStore := store.New(statePath)
	st, corruptNotice := loadState(appStore, catalog)

	uiOpts := []ui.Option{
		hostOption,
		ui.WithSource(source),
		ui.WithPlayback(newPlaybackBridge(playerMgr)),
		ui.WithChat(newChatBridge(chatMgr)),
		ui.WithStore(appStore),
		ui.WithFavorites(store.ToTwitchChannels(st.Favorites)),
		ui.WithRecents(store.ToTwitchChannels(st.Recents)),
	}
	uiOpts = append(uiOpts, configOptions(cfg, cfgPath)...)
	if corruptNotice != "" {
		uiOpts = append(uiOpts, ui.WithCorruptNotice(corruptNotice))
	}
	if cfg.AccessToken != "" {
		uiOpts = append(uiOpts, ui.WithTokenCheck(tokenCheck(opts.Validate, cfg.AccessToken)))
	}

	model := ui.New(catalog, uiOpts...)
	p := tea.NewProgram(model,
		tea.WithAltScreen(),
		tea.WithInput(input),
		tea.WithOutput(output),
		tea.WithContext(ctx),
		tea.WithoutSignalHandler(),
	)
	go forwardPlayerEvents(ctx, p, playerMgr)
	go forwardChatEvents(ctx, p, chatMgr)
	_, runErr := p.Run()
	// Stop the forwarders before the deferred player/chat shutdown.
	cancel()
	logInputConfErr(logw, playerMgr, catalog)
	if sig, ok := caught.Load().(os.Signal); ok {
		return &SignalError{Signal: sig}
	}
	if runErr != nil {
		return fmt.Errorf(catalog.T("app.err_run_tui"), runErr)
	}
	return nil
}

// loadConfig reads the config from path, or from the default location when path
// is empty, and returns the path later saves go to. That path is empty when the
// default location cannot be resolved.
func loadConfig(path string) (config.Config, string, error) {
	if path != "" {
		cfg, err := config.LoadFrom(path)
		return cfg, path, err
	}
	cfg, err := config.Load()
	if err != nil {
		return cfg, "", err
	}
	path, _ = config.DefaultPath()
	return cfg, path, nil
}

// connectVideoHost dials the window hosting this TUI. It returns nil in an
// ordinary terminal and when the window does not answer.
func connectVideoHost() *videoHost {
	path := os.Getenv(videoHostEnv)
	if path == "" {
		return nil
	}
	host, err := dialVideoHost(path)
	if err != nil {
		return nil
	}
	return host
}

// streams returns the input, the output and the log, each falling back to the
// process stream.
func streams(opts Options) (input io.Reader, output, logw io.Writer) {
	input, output, logw = opts.Input, opts.Output, opts.Log
	if input == nil {
		input = os.Stdin
	}
	if output == nil {
		output = os.Stdout
	}
	if logw == nil {
		logw = os.Stderr
	}
	return input, output, logw
}

// persistLang saves a --lang that differs from the configured language, on top
// of the config as it is on disk now (see rewriteConfig). cfg takes the new
// language even with no path to save it to and when the save fails.
func persistLang(lang string, cfg *config.Config, cfgPath string) error {
	if lang == "" {
		return nil
	}
	norm := i18n.NormalizeLocale(lang)
	if norm == "" || cfg.Language == norm {
		return nil
	}
	cfg.Language = norm
	if cfgPath == "" {
		return nil
	}
	return rewriteConfig(cfgPath, func(c *config.Config) { c.Language = norm })
}

// rewriteConfig rereads the config at path, applies change and saves it, so a
// `ttv-tui login` in another terminal since this run read the config is kept.
// A config that exists but cannot be read or parsed is left as it is and the
// error returned: saving over it would lose what it holds.
func rewriteConfig(path string, change func(*config.Config)) error {
	cfg, err := config.LoadFrom(path)
	if err != nil {
		return err
	}
	change(&cfg)
	return config.SaveTo(path, cfg)
}

func resolveStatePath(path string) (string, error) {
	if path != "" {
		return path, nil
	}
	return store.DefaultPath()
}

// cancelOnSignal cancels the run on the first signal from sigCh and keeps that
// signal in the returned value. The goroutine ends with ctx.
func cancelOnSignal(ctx context.Context, cancel context.CancelFunc, sigCh <-chan os.Signal) *atomic.Value {
	caught := new(atomic.Value)
	go func() {
		select {
		case sig := <-sigCh:
			caught.Store(sig)
			cancel()
		case <-ctx.Done():
		}
	}()
	return caught
}

// playerManager returns the player and what ends it when Run returns. A manager
// the caller injected is only stopped: closing it is the caller's job.
func playerManager(injected *player.Manager, registryDir string) (*player.Manager, func()) {
	if injected != nil {
		return injected, injected.Stop
	}
	mgr := player.New(player.Options{RegistryDir: registryDir})
	return mgr, mgr.Close
}

// chatManager is playerManager for the chat.
func chatManager(injected *chat.Manager, cfg config.Config) (*chat.Manager, func()) {
	if injected != nil {
		return injected, injected.Stop
	}
	mgr := chat.New(chat.Options{
		Token:    cfg.AccessToken,
		Username: cfg.Username,
	})
	return mgr, mgr.Close
}

// loadState reads the saved favorites and recents. A missing state file starts
// empty in silence; one that is corrupt or cannot be read starts empty with a
// notice. The store refuses to save over a file it cannot read, so the
// favorites in it are not lost.
func loadState(s *store.Store, catalog *i18n.Catalog) (store.State, string) {
	st, err := s.Load()
	switch {
	case err == nil:
		return st, ""
	case errors.Is(err, store.ErrCorrupt):
		return st, catalog.T("store.corrupt_notice")
	default:
		return st, catalog.T("store.load_failed_notice")
	}
}

// configOptions hands the UI the saved channel filters and panel layout. With a
// config path the UI can also save them; without one they last for this run.
func configOptions(cfg config.Config, cfgPath string) []ui.Option {
	if cfgPath == "" {
		var opts []ui.Option
		if len(cfg.FilterLanguages) > 0 || cfg.MinViewers > 0 {
			opts = append(opts, ui.WithChannelFilters(cfg.FilterLanguages, cfg.MinViewers, nil))
		}
		return append(opts, ui.WithHostedColumns(cfg.ListColumns, cfg.ChatColumns, cfg.ChatOverlay, cfg.PanelsSwapped, cfg.ChatOverlayOpacity, nil))
	}
	_, err := os.Stat(cfgPath)
	w := &configWriter{path: cfgPath, existed: err == nil}
	return []ui.Option{
		ui.WithChannelFilters(cfg.FilterLanguages, cfg.MinViewers, w.saveFilters),
		ui.WithHostedColumns(cfg.ListColumns, cfg.ChatColumns, cfg.ChatOverlay, cfg.PanelsSwapped, cfg.ChatOverlayOpacity, w.saveColumns),
	}
}

// configWriter saves the settings the UI changes while it runs. The lock keeps
// a filter save and a layout save from rereading the same file and each
// dropping the other's change. existed tells whether the file was there at
// startup or after the last save.
type configWriter struct {
	mu      sync.Mutex
	path    string
	existed bool
}

// update applies change to the config on disk (see rewriteConfig): a `ttv-tui
// login` in another terminal must not be undone by a change of panel width
// here. If the config cannot be read, nothing is saved and the UI shows the
// error; the change still holds for this run. A config deleted while the app
// runs is a logout: the save recreates it without the token and returns
// ui.ErrConfigRemoved, once per deletion.
func (w *configWriter) update(change func(*config.Config)) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, statErr := os.Stat(w.path)
	removed := w.existed && errors.Is(statErr, os.ErrNotExist)
	if err := rewriteConfig(w.path, change); err != nil {
		return err
	}
	w.existed = true
	if removed {
		return ui.ErrConfigRemoved
	}
	return nil
}

func (w *configWriter) saveFilters(languages []string, minViewers int) error {
	return w.update(func(c *config.Config) {
		c.FilterLanguages = languages
		c.MinViewers = minViewers
	})
}

func (w *configWriter) saveColumns(list, chat int, overlay, swapped bool, opacity int) error {
	return w.update(func(c *config.Config) {
		c.ListColumns = list
		c.ChatColumns = chat
		c.ChatOverlay = overlay
		c.PanelsSwapped = swapped
		c.ChatOverlayOpacity = opacity
	})
}

// tokenCheck builds the startup check of the saved token. A nil error means
// valid, ErrTokenInvalid means refused, and any other error is indeterminate
// (network, timeout, HTTP 5xx/429 or malformed response). Only refusal shows
// the renewal notice. A nil validate means auth.Validate.
func tokenCheck(validate func(ctx context.Context, token string) (auth.Validation, error), token string) ui.TokenCheckFunc {
	if validate == nil {
		validate = auth.Validate
	}
	return func(ctx context.Context) error {
		vCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		_, err := validate(vCtx, token)
		if errors.Is(err, auth.ErrTokenInvalid) {
			return auth.ErrTokenInvalid
		}
		return nil
	}
}
