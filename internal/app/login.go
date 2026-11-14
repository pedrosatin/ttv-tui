package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/auth"
	"github.com/pedrosatin/ttv-tui/internal/config"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/termtext"
)

const lowExpiryThresholdSeconds = 3600

// LoginOptions configures a login command execution (--from-browser or --import twt).
type LoginOptions struct {
	FromBrowser   bool
	Browser       string
	Profile       string
	Import        string
	Lang          string
	ConfigPath    string
	TwtConfigPath string
	Input         io.Reader
	Output        io.Writer

	// Optional dependency overrides for tests.
	ListProfiles func() ([]auth.Profile, error)
	ExtractToken func(auth.Profile) (string, error)
	Validate     func(ctx context.Context, token string) (auth.Validation, error)
}

// RunLogin executes the login workflow (--from-browser or --import twt),
// validates the token against Twitch, emits actionable warnings, and writes
// the configuration atomically with 0600 permissions. It never prints the token.
func RunLogin(ctx context.Context, opts LoginOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	in := opts.Input
	if in == nil {
		in = os.Stdin
	}
	out := opts.Output
	if out == nil {
		out = os.Stdout
	}

	listProfiles := opts.ListProfiles
	if listProfiles == nil {
		listProfiles = auth.ListProfiles
	}
	extractToken := opts.ExtractToken
	if extractToken == nil {
		extractToken = auth.ExtractToken
	}
	validateFn := opts.Validate
	if validateFn == nil {
		validateFn = auth.Validate
	}

	cat, _ := i18n.New(i18n.LangEN)
	cfgPath := opts.ConfigPath
	if cfgPath == "" {
		var err error
		cfgPath, err = config.DefaultPath()
		if err != nil {
			return fmt.Errorf(cat.T("login.err_resolve_config"), err)
		}
	}

	existingCfg, _ := config.LoadFrom(cfgPath)
	resolvedLang := i18n.Resolve(opts.Lang, existingCfg.Language, i18n.EnvLocale())
	if activeCat, err := i18n.New(resolvedLang); err == nil {
		cat = activeCat
	}

	importSrc := strings.TrimSpace(opts.Import)
	browserFlag := strings.TrimSpace(opts.Browser)
	profileFlag := strings.TrimSpace(opts.Profile)

	var (
		rawToken         string
		importedUsername string
		err              error
	)

	if importSrc != "" {
		if opts.FromBrowser || browserFlag != "" || profileFlag != "" {
			return errors.New(cat.T("login.err_combine_flags"))
		}
		if !strings.EqualFold(importSrc, "twt") {
			return fmt.Errorf(cat.T("login.err_unsupported_import"), importSrc)
		}
		rawToken, importedUsername, err = loadTokenFromTwt(cat, opts.TwtConfigPath, out)
		if err != nil {
			return err
		}
	} else {
		rawToken, err = extractTokenFromBrowser(cat, listProfiles, extractToken, browserFlag, profileFlag, in, out)
		if err != nil {
			return err
		}
	}

	token := config.NormalizeToken(rawToken)
	if token == "" {
		return formatAuthErr(cat, &auth.Error{
			Op:      "validate",
			Err:     auth.ErrTokenInvalid,
			Message: cat.T("login.err_empty_token"),
		}, "")
	}

	vCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		vCtx, cancel = context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
	}

	validation, err := validateFn(vCtx, token)
	if err != nil {
		return formatAuthErr(cat, err, token)
	}

	hasChatScope := hasScope(validation.Scopes, "chat_login") ||
		hasScope(validation.Scopes, "chat:edit") ||
		hasScope(validation.Scopes, "chat:read")
	if !hasChatScope {
		scopesDesc := strings.Join(validation.Scopes, ", ")
		if scopesDesc == "" {
			scopesDesc = cat.T("login.scopes_none")
		}
		_, _ = fmt.Fprintln(out, fmt.Sprintf(cat.T("login.warn_scopes"), termtext.Line(scopesDesc)))
	}

	if validation.ExpiresIn <= 0 || validation.ExpiresIn < lowExpiryThresholdSeconds {
		_, _ = fmt.Fprintln(out, fmt.Sprintf(cat.T("login.warn_expiry"), validation.ExpiresIn))
	}

	existingCfg, err = config.LoadFrom(cfgPath)
	if err != nil {
		return fmt.Errorf(cat.T("login.err_read_config"), withRecoveryHint(cat, err))
	}
	if existingCfg.HasToken() {
		_, _ = fmt.Fprintln(out, cat.T("login.warn_overwrite"))
	}

	if opts.Lang != "" {
		normFlag := i18n.NormalizeLocale(opts.Lang)
		if normFlag != "" {
			existingCfg.Language = normFlag
		}
	}

	if validation.Login != "" {
		existingCfg.Username = validation.Login
	} else if importedUsername != "" && existingCfg.Username == "" {
		existingCfg.Username = importedUsername
	}
	existingCfg.AccessToken = token

	if err := config.SaveTo(cfgPath, existingCfg); err != nil {
		return fmt.Errorf(cat.T("login.err_save_config"), termtext.Line(cfgPath), err)
	}

	_, _ = fmt.Fprintln(out, fmt.Sprintf(cat.T("login.success"), termtext.Line(existingCfg.Username), termtext.Line(cfgPath)))
	return nil
}

func loadTokenFromTwt(cat *i18n.Catalog, twtPath string, out io.Writer) (token string, username string, err error) {
	if twtPath == "" {
		twtPath, err = config.DefaultTwtPath()
		if err != nil {
			return "", "", fmt.Errorf(cat.T("login.err_resolve_twt_path"), err)
		}
	}
	// The path can come from XDG_CONFIG_HOME or the HOME directory name, so it
	// is neutralized wherever it is printed. The wrapped errors keep their %w
	// chain; the caller prints them through termtext.Line.
	shownTwtPath := termtext.Line(twtPath)
	twtCfg, err := config.LoadTwtFrom(twtPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", "", fmt.Errorf(cat.T("login.err_twt_not_found"), shownTwtPath, err)
		}
		return "", "", fmt.Errorf(cat.T("login.err_read_twt"), shownTwtPath, err)
	}
	if twtCfg.AccessToken == "" {
		return "", "", formatAuthErr(cat, &auth.Error{
			Op:      "import",
			Err:     auth.ErrTokenInvalid,
			Message: fmt.Sprintf(cat.T("login.err_twt_no_token"), shownTwtPath),
		}, "")
	}
	_, _ = fmt.Fprintln(out, fmt.Sprintf(cat.T("login.importing_twt"), shownTwtPath))
	return twtCfg.AccessToken, twtCfg.Username, nil
}

func extractTokenFromBrowser(
	cat *i18n.Catalog,
	listProfiles func() ([]auth.Profile, error),
	extractToken func(auth.Profile) (string, error),
	browserFlag, profileFlag string,
	in io.Reader,
	out io.Writer,
) (string, error) {
	profiles, err := listProfiles()
	if err != nil {
		return "", formatAuthErr(cat, err, "")
	}

	if browserFlag != "" || profileFlag != "" {
		var matches []auth.Profile
		browserExists := false
		for _, p := range profiles {
			if browserFlag != "" && !strings.EqualFold(string(p.Browser), browserFlag) {
				continue
			}
			browserExists = true
			if profileFlag != "" && !strings.EqualFold(p.Name, profileFlag) {
				continue
			}
			matches = append(matches, p)
		}
		if len(matches) == 0 {
			if browserFlag != "" && !browserExists {
				return "", formatAuthErr(cat, &auth.Error{
					Op:      "find",
					Err:     auth.ErrBrowserNotFound,
					Message: fmt.Sprintf(cat.T("login.err_browser_flag_not_found"), browserFlag),
				}, "")
			}
			return "", formatAuthErr(cat, &auth.Error{
				Op:      "find",
				Err:     auth.ErrProfileNotFound,
				Message: fmt.Sprintf(cat.T("login.err_profile_flag_not_found"), profileFlag),
			}, "")
		}

		var lastErr error
		for i, p := range matches {
			if i == 0 || profileFlag == "" {
				tok, exErr := extractToken(p)
				if exErr == nil {
					_, _ = fmt.Fprintln(out, fmt.Sprintf(cat.T("login.profile_selected"), p.Browser, termtext.Line(p.Name)))
					return tok, nil
				}
				lastErr = exErr
				if profileFlag == "" && errors.Is(exErr, auth.ErrCookieNotFound) && i+1 < len(matches) {
					continue
				}
				return "", formatAuthErr(cat, exErr, "")
			}
		}
		return "", formatAuthErr(cat, lastErr, "")
	}

	if len(profiles) == 0 {
		return "", formatAuthErr(cat, &auth.Error{
			Op:      "list",
			Err:     auth.ErrBrowserNotFound,
			Message: cat.T("login.err_no_browsers_found"),
		}, "")
	}

	_, _ = fmt.Fprintln(out, cat.T("login.profiles_found"))
	for i, p := range profiles {
		_, _ = fmt.Fprintf(out, "  [%d] %s / %s\n", i+1, p.Browser, termtext.Line(p.Name))
	}

	var chosen auth.Profile
	if len(profiles) == 1 {
		chosen = profiles[0]
		_, _ = fmt.Fprintln(out, fmt.Sprintf(cat.T("login.profile_single"), chosen.Browser, termtext.Line(chosen.Name)))
	} else {
		_, _ = fmt.Fprintf(out, cat.T("login.choose_profile"), len(profiles))
		reader := bufio.NewReader(in)
		line, readErr := reader.ReadString('\n')
		if readErr != nil && (!errors.Is(readErr, io.EOF) || strings.TrimSpace(line) == "") {
			return "", fmt.Errorf(cat.T("login.err_read_profile_choice"), readErr)
		}
		choiceStr := strings.TrimSpace(line)
		idx, convErr := strconv.Atoi(choiceStr)
		if convErr != nil || idx < 1 || idx > len(profiles) {
			return "", fmt.Errorf(cat.T("login.err_invalid_profile_choice"), choiceStr, len(profiles))
		}
		chosen = profiles[idx-1]
		_, _ = fmt.Fprintln(out, fmt.Sprintf(cat.T("login.profile_selected"), chosen.Browser, termtext.Line(chosen.Name)))
	}

	tok, err := extractToken(chosen)
	if err != nil {
		return "", formatAuthErr(cat, err, "")
	}
	return tok, nil
}

func hasScope(scopes []string, target string) bool {
	for _, s := range scopes {
		if strings.EqualFold(strings.TrimSpace(s), target) {
			return true
		}
	}
	return false
}

type redactedError struct {
	cause error
	text  string
}

func (e *redactedError) Error() string { return e.text }
func (e *redactedError) Unwrap() error { return e.cause }

func sanitizeErr(err error, secret string) error {
	if err == nil || secret == "" {
		return err
	}
	raw := err.Error()
	if !strings.Contains(raw, secret) {
		return err
	}
	clean := strings.ReplaceAll(raw, secret, "[REDACTED]")
	for _, sentinel := range []error{
		auth.ErrKeyringUnavailable,
		auth.ErrCookieNotFound,
		auth.ErrBrowserNotFound,
		auth.ErrProfileNotFound,
		auth.ErrSQLiteUnavailable,
		auth.ErrTokenInvalid,
		auth.ErrValidateTimeout,
		context.DeadlineExceeded,
	} {
		if errors.Is(err, sentinel) {
			return &redactedError{cause: sentinel, text: clean}
		}
	}
	return errors.New(clean)
}

func formatAuthErr(cat *i18n.Catalog, err error, secret string) error {
	err = sanitizeErr(err, secret)
	if cat == nil {
		cat, _ = i18n.New(i18n.LangEN)
	}
	switch {
	case errors.Is(err, auth.ErrKeyringUnavailable):
		return fmt.Errorf(cat.T("login.err_keyring"), err)
	case errors.Is(err, auth.ErrCookieNotFound):
		return fmt.Errorf(cat.T("login.err_cookie"), err)
	case errors.Is(err, auth.ErrBrowserNotFound):
		return fmt.Errorf(cat.T("login.err_browser"), err)
	case errors.Is(err, auth.ErrProfileNotFound):
		return fmt.Errorf(cat.T("login.err_profile"), err)
	case errors.Is(err, auth.ErrSQLiteUnavailable):
		return fmt.Errorf(cat.T("login.err_sqlite"), err)
	case errors.Is(err, auth.ErrTokenInvalid):
		return fmt.Errorf(cat.T("login.err_token_invalid"), err)
	case errors.Is(err, auth.ErrValidateTimeout), errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf(cat.T("login.err_timeout"), err)
	default:
		return fmt.Errorf(cat.T("login.err_failed"), err)
	}
}
