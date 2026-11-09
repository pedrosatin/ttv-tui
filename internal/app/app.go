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
	// Video is the --video flag. It overrides the config when not empty or when
	// VideoSet says the flag was given empty, which turns the mode off for this run.
	Video      string
	VideoSet   bool
	SixelProbe player.SixelProbe
	Lang       string
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
func Run(opts Options) error {
	if opts.Video != "" && opts.Video != "sixel" {
		return errors.New(earlyCatalog().T("video.invalid_backend"))
	}
	if opts.Lang != "" && !i18n.IsSupportedFlag(opts.Lang) {
		return fmt.Errorf(earlyCatalog().T("app.err_unsupported_lang"), opts.Lang, i18n.LangEN, i18n.LangPTBR)
	}

	var (
		cfg config.Config
		err error
	)
	if opts.ConfigPath != "" {
		cfg, err = config.LoadFrom(opts.ConfigPath)
	} else {
		cfg, err = config.Load()
	}
	if err != nil {
		return fmt.Errorf(earlyCatalog().T("app.err_load_config"), err)
	}

	cfgPath := opts.ConfigPath
	if cfgPath == "" {
		cfgPath, _ = config.DefaultPath()
	}
	video := videoBackend(opts, cfg)
	if openWindow(opts, video) {
		return nil
	}
	var hostOption ui.Option
	if path := os.Getenv(videoHostEnv); path != "" {
		// The hosting window draws the video, so no terminal backend applies.
		if host, err := dialVideoHost(path); err == nil {
			defer host.Close()
			hostOption, video = ui.WithVideoHost(host), ""
		}
	}

	resolvedLang := i18n.Resolve(opts.Lang, cfg.Language, i18n.EnvLocale())
	catalog, err := i18n.New(resolvedLang)
	if err != nil {
		// No catalog exists to localize this one: the embedded files failed to load.
		return fmt.Errorf("initialize i18n catalog: %w", err)
	}

	input := opts.Input
	if input == nil {
		input = os.Stdin
	}
	output := opts.Output
	if output == nil {
		output = os.Stdout
	}
	if !isTerminal(input) || !isTerminal(output) {
		return errors.New(catalog.T("app.err_tty_required"))
	}

	if opts.Lang != "" {
		norm := i18n.NormalizeLocale(opts.Lang)
		if norm != "" && cfg.Language != norm {
			cfg.Language = norm
			if cfgPath != "" {
				if err := config.SaveTo(cfgPath, cfg); err != nil {
					return fmt.Errorf(catalog.T("app.err_persist_lang"), err)
				}
			}
		}
	}

	source := opts.Source
	if source == nil {
		source = twitch.NewGQLSource(twitch.GQLConfig{})
	}

	statePath := opts.StatePath
	if statePath == "" {
		var err error
		statePath, err = store.DefaultPath()
		if err != nil {
			return fmt.Errorf(catalog.T("app.err_resolve_state_path"), err)
		}
	}
	registryDir := playerRegistryDir(statePath)

	logw := opts.Log
	if logw == nil {
		logw = os.Stderr
	}

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
	var caught atomic.Value
	go func() {
		select {
		case sig := <-sigCh:
			caught.Store(sig)
			cancel()
		case <-ctx.Done():
		}
	}()

	playerMgr := opts.Player
	ownsPlayer := false
	if playerMgr == nil {
		playerMgr = player.New(player.Options{RegistryDir: registryDir})
		ownsPlayer = true
	}
	if ownsPlayer {
		defer playerMgr.Close()
	} else {
		defer playerMgr.Stop()
	}

	chatMgr := opts.Chat
	ownsChat := false
	if chatMgr == nil {
		chatMgr = chat.New(chat.Options{
			Token:    cfg.AccessToken,
			Username: cfg.Username,
		})
		ownsChat = true
	}
	if ownsChat {
		defer chatMgr.Close()
	} else {
		defer chatMgr.Stop()
	}

	appStore := store.New(statePath)
	st, err := appStore.Load()
	var corruptNotice string
	if err != nil {
		if errors.Is(err, store.ErrCorrupt) {
			corruptNotice = catalog.T("store.corrupt_notice")
		}
	}

	videoReason := ""
	programOutput := output
	var videoSurface ui.Option
	if video != "" {
		videoReason = detectVideo(ctx, opts, input, output)
		if videoReason == "" {
			var stopVideo func()
			programOutput, videoSurface, stopVideo = startVideo(output, playerMgr, probeCell(ctx, input, output))
			defer stopVideo()
		}
	}

	uiOpts := []ui.Option{
		ui.WithVideo(video != "", videoReason),
		videoSurface,
		hostOption,
		ui.WithSource(source),
		ui.WithPlayback(newPlaybackBridge(playerMgr)),
		ui.WithChat(newChatBridge(chatMgr)),
		ui.WithStore(appStore),
		ui.WithFavorites(store.ToTwitchChannels(st.Favorites)),
		ui.WithRecents(store.ToTwitchChannels(st.Recents)),
		ui.WithVideoChatRows(cfg.VideoChatRows),
		ui.WithVideoChatOnTop(cfg.VideoChatOnTop()),
	}
	if cfgPath != "" {
		// Reread before each save: a `ttv-tui login` in another terminal must not be
		// undone by a change of panel width here.
		persistPath := cfgPath
		persistCfg := cfg
		var persistMu sync.Mutex
		updateConfig := func(change func(*config.Config)) error {
			persistMu.Lock()
			defer persistMu.Unlock()
			if fresh, err := config.LoadFrom(persistPath); err == nil {
				persistCfg = fresh
			}
			change(&persistCfg)
			return config.SaveTo(persistPath, persistCfg)
		}
		uiOpts = append(uiOpts, ui.WithChannelFilters(cfg.FilterLanguages, cfg.MinViewers, func(languages []string, minViewers int) error {
			return updateConfig(func(c *config.Config) {
				c.FilterLanguages = languages
				c.MinViewers = minViewers
			})
		}))
		uiOpts = append(uiOpts, ui.WithHostedColumns(cfg.ListColumns, cfg.ChatColumns, cfg.ChatOverlay, cfg.PanelsSwapped, cfg.ChatOverlayOpacity, func(list, chat int, overlay, swapped bool, opacity int) error {
			return updateConfig(func(c *config.Config) {
				c.ListColumns = list
				c.ChatColumns = chat
				c.ChatOverlay = overlay
				c.PanelsSwapped = swapped
				c.ChatOverlayOpacity = opacity
			})
		}))
	} else {
		if len(cfg.FilterLanguages) > 0 || cfg.MinViewers > 0 {
			uiOpts = append(uiOpts, ui.WithChannelFilters(cfg.FilterLanguages, cfg.MinViewers, nil))
		}
		uiOpts = append(uiOpts, ui.WithHostedColumns(cfg.ListColumns, cfg.ChatColumns, cfg.ChatOverlay, cfg.PanelsSwapped, cfg.ChatOverlayOpacity, nil))
	}
	if corruptNotice != "" {
		uiOpts = append(uiOpts, ui.WithCorruptNotice(corruptNotice))
	}
	if cfg.AccessToken != "" {
		validateFn := opts.Validate
		if validateFn == nil {
			validateFn = auth.Validate
		}
		tok := cfg.AccessToken
		uiOpts = append(uiOpts, ui.WithTokenCheck(func(ctx context.Context) error {
			vCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			_, err := validateFn(vCtx, tok)
			if err != nil {
				if errors.Is(err, auth.ErrValidateTimeout) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
					return nil
				}
				return auth.ErrTokenInvalid
			}
			return nil
		}))
	}

	model := ui.New(catalog, uiOpts...)
	var progOpts []tea.ProgramOption
	progOpts = append(progOpts, tea.WithAltScreen())
	progOpts = append(progOpts,
		tea.WithInput(input),
		tea.WithOutput(programOutput),
		tea.WithContext(ctx),
		tea.WithoutSignalHandler(),
	)

	p := tea.NewProgram(model, progOpts...)
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
