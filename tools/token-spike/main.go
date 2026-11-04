// Command token-spike is a debug tool for ticket 06: list browser profiles,
// extract the twitch.tv auth-token cookie, validate it, and print login+scopes.
// It never prints the token itself. Not part of the product binary.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/auth"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "list":
		err = cmdList()
	case "extract":
		err = cmdExtract(os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "comando desconhecido: %s\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "erro: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `uso:
  token-spike list
  token-spike extract --browser <nome> [--profile <nome>]

Nunca imprime o token. Imprime login e scopes após validar em id.twitch.tv.
`)
}

func cmdList() error {
	profiles, err := auth.ListProfiles()
	if err != nil {
		return err
	}
	if len(profiles) == 0 {
		fmt.Println("nenhum perfil com banco de cookies encontrado")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "BROWSER\tPROFILE\tFAMILY\tCOOKIES")
	for _, p := range profiles {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.Browser, p.Name, p.Family, p.CookiesPath)
	}
	return w.Flush()
}

func cmdExtract(args []string) error {
	browser, profile, err := parseExtractFlags(args)
	if err != nil {
		return err
	}
	if browser == "" {
		return errors.New("informe --browser (ex.: brave, chromium, firefox)")
	}

	p, err := auth.FindProfile(browser, profile)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "extraindo de %s / %s (%s)\n", p.Browser, p.Name, p.CookiesPath)

	token, err := auth.ExtractToken(p)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	v, err := auth.Validate(ctx, token)
	if err != nil {
		return err
	}

	fmt.Printf("login:   %s\n", v.Login)
	fmt.Printf("user_id: %s\n", v.UserID)
	fmt.Printf("expires: %ds\n", v.ExpiresIn)
	fmt.Printf("scopes:  %s\n", strings.Join(v.Scopes, ", "))
	return nil
}

func parseExtractFlags(args []string) (browser, profile string, err error) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--browser", "-b":
			if i+1 >= len(args) {
				return "", "", errors.New("--browser requer um valor")
			}
			i++
			browser = args[i]
		case "--profile", "-p":
			if i+1 >= len(args) {
				return "", "", errors.New("--profile requer um valor")
			}
			i++
			profile = args[i]
		default:
			return "", "", fmt.Errorf("flag desconhecida: %s", args[i])
		}
	}
	return browser, profile, nil
}
