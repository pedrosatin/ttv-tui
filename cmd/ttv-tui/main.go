package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/pedrosatin/ttv-tui/internal/app"
	"github.com/pedrosatin/ttv-tui/internal/config"
	"github.com/pedrosatin/ttv-tui/internal/doctor"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

var version = "dev"

func main() {
	os.Exit(runWithIO(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, app.LoginOptions{}))
}

func run(args []string, stdout, stderr io.Writer) int {
	return runWithIO(args, os.Stdin, stdout, stderr, app.LoginOptions{})
}

func findLangFlag(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--lang" || arg == "-lang" {
			if i+1 < len(args) {
				return args[i+1]
			}
		} else if strings.HasPrefix(arg, "--lang=") {
			return strings.TrimPrefix(arg, "--lang=")
		} else if strings.HasPrefix(arg, "-lang=") {
			return strings.TrimPrefix(arg, "-lang=")
		}
	}
	return ""
}

func runWithIO(args []string, stdin io.Reader, stdout, stderr io.Writer, baseLoginOpts app.LoginOptions) int {
	earlyLang := findLangFlag(args)
	if earlyLang == "" {
		earlyLang = baseLoginOpts.Lang
	}
	cfgPath := baseLoginOpts.ConfigPath
	var cfg config.Config
	if cfgPath != "" {
		cfg, _ = config.LoadFrom(cfgPath)
	} else {
		cfg, _ = config.Load()
	}
	resolvedLang := i18n.Resolve(earlyLang, cfg.Language, i18n.EnvLocale())
	cat, _ := i18n.New(resolvedLang)

	fs := flag.NewFlagSet("ttv-tui", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		showVersion bool
		showHelp    bool
		lang        string
		importSrc   string
		video       string
	)
	fs.BoolVar(&showVersion, "version", false, cat.T("cli.flag.version"))
	fs.BoolVar(&showHelp, "help", false, cat.T("cli.flag.help"))
	fs.BoolVar(&showHelp, "h", false, cat.T("cli.flag.help"))
	fs.StringVar(&video, "video", "", cat.T("cli.flag.video"))
	fs.StringVar(&lang, "lang", "", cat.T("cli.flag.lang"))
	fs.StringVar(&importSrc, "import", "", cat.T("cli.flag.import"))

	printRootUsage := func(w io.Writer) {
		_, _ = fmt.Fprint(w, cat.T("cli.usage_root"))
		fs.SetOutput(w)
		fs.PrintDefaults()
	}

	fs.Usage = func() {
		printRootUsage(stderr)
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if showHelp {
		printRootUsage(stdout)
		return 0
	}

	if showVersion {
		_, _ = fmt.Fprintf(stdout, "ttv-tui %s\n", version)
		return 0
	}

	rest := fs.Args()
	if len(rest) > 0 {
		if rest[0] == "doctor" {
			return runDoctorSubcommand(rest[1:], resolvedLang, cfgPath, stdin, stdout, stderr)
		}
		if rest[0] == "login" {
			return runLoginSubcommand(rest[1:], lang, importSrc, stdin, stdout, stderr, baseLoginOpts, cfg)
		}
		_, _ = fmt.Fprintf(stderr, cat.T("cli.unknown_command")+"\n", rest[0])
		printRootUsage(stderr)
		return 2
	}

	if importSrc != "" {
		return runLoginSubcommand(nil, lang, importSrc, stdin, stdout, stderr, baseLoginOpts, cfg)
	}

	if err := app.Run(app.Options{
		Video:      video,
		VideoSet:   flagGiven(fs, "video"),
		Lang:       lang,
		ConfigPath: cfgPath,
		Input:      stdin,
		Output:     stdout,
		Log:        stderr,
	}); err != nil {
		var sigErr *app.SignalError
		if !errors.As(err, &sigErr) {
			_, _ = fmt.Fprintf(stderr, "ttv-tui: %v\n", err)
		}
		return app.ExitCode(err)
	}
	return 0
}

// flagGiven tells a flag passed empty from one left out, which the value cannot.
func flagGiven(fs *flag.FlagSet, name string) bool {
	given := false
	fs.Visit(func(f *flag.Flag) { given = given || f.Name == name })
	return given
}

func runLoginSubcommand(
	args []string,
	rootLang string,
	rootImport string,
	stdin io.Reader,
	stdout, stderr io.Writer,
	baseOpts app.LoginOptions,
	cfg config.Config,
) int {
	loginEarlyLang := findLangFlag(args)
	if loginEarlyLang == "" {
		loginEarlyLang = rootLang
	}
	if loginEarlyLang == "" {
		loginEarlyLang = baseOpts.Lang
	}
	resolved := i18n.Resolve(loginEarlyLang, cfg.Language, i18n.EnvLocale())
	cat, _ := i18n.New(resolved)

	lfs := flag.NewFlagSet("ttv-tui login", flag.ContinueOnError)
	lfs.SetOutput(stderr)

	var (
		fromBrowser bool
		browser     string
		profile     string
		importSrc   string
		lang        string
		showHelp    bool
	)
	lfs.BoolVar(&fromBrowser, "from-browser", false, cat.T("cli.flag.from_browser"))
	lfs.StringVar(&browser, "browser", "", cat.T("cli.flag.browser"))
	lfs.StringVar(&browser, "b", "", cat.T("cli.flag.browser_shorthand"))
	lfs.StringVar(&profile, "profile", "", cat.T("cli.flag.profile"))
	lfs.StringVar(&profile, "p", "", cat.T("cli.flag.profile_shorthand"))
	lfs.StringVar(&importSrc, "import", rootImport, cat.T("cli.flag.import_login"))
	lfs.StringVar(&lang, "lang", rootLang, cat.T("cli.flag.lang"))
	lfs.BoolVar(&showHelp, "help", false, cat.T("cli.flag.help"))
	lfs.BoolVar(&showHelp, "h", false, cat.T("cli.flag.help"))

	printLoginUsage := func(w io.Writer) {
		_, _ = fmt.Fprint(w, cat.T("cli.usage_login"))
		lfs.SetOutput(w)
		lfs.PrintDefaults()
	}

	lfs.Usage = func() {
		printLoginUsage(stderr)
	}

	if err := lfs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if showHelp {
		printLoginUsage(stdout)
		return 0
	}

	if len(lfs.Args()) > 0 {
		_, _ = fmt.Fprintf(stderr, cat.T("cli.unexpected_argument")+"\n", lfs.Args()[0])
		printLoginUsage(stderr)
		return 2
	}

	opts := baseOpts
	opts.FromBrowser = fromBrowser
	opts.Browser = browser
	opts.Profile = profile
	opts.Import = importSrc
	if lang != "" {
		opts.Lang = lang
	}
	if opts.Input == nil {
		opts.Input = stdin
	}
	if opts.Output == nil {
		opts.Output = stdout
	}

	if err := app.RunLogin(context.Background(), opts); err != nil {
		_, _ = fmt.Fprintf(stderr, "ttv-tui login: %v\n", err)
		return 1
	}
	return 0
}

func runDoctorSubcommand(args []string, rootLang, configPath string, stdin io.Reader, stdout, stderr io.Writer) int {
	cat, _ := i18n.New(i18n.Resolve(findLangFlag(args), rootLang, i18n.EnvLocale()))
	fs := flag.NewFlagSet("ttv-tui doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	lang := rootLang
	fs.StringVar(&lang, "lang", rootLang, cat.T("cli.flag.lang"))
	fs.Usage = func() { _, _ = fmt.Fprint(fs.Output(), cat.T("doctor.usage")); fs.PrintDefaults() }
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(fs.Args()) != 0 {
		fs.Usage()
		return 2
	}
	return doctor.Run(stdout, doctor.Options{
		Version:    version,
		Lang:       lang,
		ConfigPath: configPath,
		Terminal:   app.TerminalProbe(stdin, stdout),
	})
}
