package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
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
	Lang       string
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

// Run composes config, i18n, Source, player, chat, and the UI model and runs the TUI program.
func Run(opts Options) error {
	if opts.Lang != "" && !i18n.IsSupportedFlag(opts.Lang) {
		return fmt.Errorf("unsupported --lang %q (want %q or %q)", opts.Lang, i18n.LangEN, i18n.LangPTBR)
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
		return fmt.Errorf("load config: %w", err)
	}

	cfgPath := opts.ConfigPath
	if cfgPath == "" {
		cfgPath, _ = config.DefaultPath()
	}

	resolvedLang := i18n.Resolve(opts.Lang, cfg.Language, i18n.EnvLocale())
	catalog, err := i18n.New(resolvedLang)
	if err != nil {
		return fmt.Errorf(catalog.T("app.err_init_catalog"), err)
	}

	if opts.Lang != "" {
		norm := i18n.NormalizeLocale(opts.Lang)
		if norm != "" && cfg.Language != norm {
			cfg.Language = norm
			if cfgPath != "" {
				if err := config.SaveTo(cfgPath, cfg); err != nil {
					return fmt.Errorf("persist language to config: %w", err)
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
			return fmt.Errorf("resolve state path: %w", err)
		}
	}
	registryDir := playerRegistryDir(statePath)

	input := opts.Input
	if input == nil {
		input = os.Stdin
	}
	output := opts.Output
	if output == nil {
		output = os.Stdout
	}
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

	uiOpts := []ui.Option{
		ui.WithSource(source),
		ui.WithPlayback(newPlaybackBridge(playerMgr)),
		ui.WithChat(newChatBridge(chatMgr)),
		ui.WithStore(appStore),
		ui.WithFavorites(store.ToTwitchChannels(st.Favorites)),
		ui.WithRecents(store.ToTwitchChannels(st.Recents)),
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
		tea.WithOutput(output),
		tea.WithContext(ctx),
		tea.WithoutSignalHandler(),
	)

	p := tea.NewProgram(model, progOpts...)
	go forwardPlayerEvents(ctx, p, playerMgr)
	go forwardChatEvents(ctx, p, chatMgr)
	_, runErr := p.Run()
	// Stop the forwarders before the deferred player/chat shutdown.
	cancel()
	if sig, ok := caught.Load().(os.Signal); ok {
		return &SignalError{Signal: sig}
	}
	if runErr != nil {
		return fmt.Errorf("run tui: %w", runErr)
	}
	return nil
}
