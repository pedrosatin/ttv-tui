// Command streamlink é o streamlink falso dos cenários da janela. Ele aceita a
// linha de comando que internal/player monta, abre o player pedido com
// --player e --player-args como o streamlink faz, lendo do stdin, e entrega o
// clipe de fixture até o player fechar. Não vai à rede.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"

	"github.com/pedrosatin/ttv-tui/internal/fixture"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "[cli][error]", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	player, playerArgs, url := "", "", ""
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "--version":
			fmt.Println("streamlink 7.0.0")
			return nil
		case "--player", "--player-args", "--loglevel":
			if i+1 >= len(args) {
				return fmt.Errorf("%s sem valor", a)
			}
			i++
			switch a {
			case "--player":
				player = args[i]
			case "--player-args":
				playerArgs = args[i]
			}
		default:
			if url == "" && len(a) > 0 && a[0] != '-' {
				url = a
			}
		}
	}
	if player == "" {
		return errors.New("falta --player")
	}
	argv, err := fixture.SplitArgs(playerArgs)
	if err != nil {
		return err
	}
	fmt.Printf("[cli][info] Found matching plugin twitch for URL %s\n", url)

	// Trata SIGTERM/SIGINT como o streamlink real: depois de abrir o player,
	// repassa o sinal ao filho e só sai depois de recolhê-lo com Wait, evitando
	// órfão zumbi no grupo de processos em contêineres cujo PID 1 não recolhe.
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	cmd := exec.Command(player, append(argv, "-")...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("pipe do player: %w", err)
	}

	var childMu sync.Mutex
	var child *os.Process
	var waited bool
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case <-done:
				return
			case sig := <-sigCh:
				childMu.Lock()
				p, w := child, waited
				childMu.Unlock()
				if w {
					return
				}
				if p == nil {
					os.Exit(0)
				}
				_ = in.Close()
				_ = p.Signal(sig)
			}
		}
	}()

	// OpenGateEnv imita o streamlink que demora a achar a live: o player só
	// abre quando o cenário libera o gate.
	if gate := os.Getenv(fixture.OpenGateEnv); gate != "" {
		if err := fixture.WaitGate(gate); err != nil {
			return err
		}
	}
	fmt.Println("[cli][info] Available streams: best (fixture)")
	fmt.Println("[cli][info] Opening stream: best (fixture)")
	fmt.Printf("[cli][info] Starting player: %s\n", player)

	childMu.Lock()
	if err := cmd.Start(); err != nil {
		childMu.Unlock()
		return fmt.Errorf("abrir o player: %w", err)
	}
	child = cmd.Process
	childMu.Unlock()

	// O player fechar o pipe encerra a escrita; o erro dela não importa.
	go func() {
		_ = fixture.WriteClip(in, 0)
		_ = in.Close()
	}()
	// Player encerrado por sinal ou com erro é o fim normal de uma sessão.
	_ = cmd.Wait()
	childMu.Lock()
	waited = true
	childMu.Unlock()
	fmt.Println("[cli][info] Closing currently open stream...")
	return nil
}
