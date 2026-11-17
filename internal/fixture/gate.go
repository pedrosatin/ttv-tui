package fixture

import (
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// OpenGateEnv é a variável com que um cenário segura o streamlink falso antes
// de ele abrir o player: o valor é o socket Unix de um Gate. O streamlink
// conecta, avisa assim que chegou à espera e só segue quando o cenário libera.
const OpenGateEnv = "TTV_FIXTURE_OPEN_GATE"

// Gate é o lado do cenário da espera do streamlink falso. Arrived diz que o
// streamlink está parado nela; Release o deixa abrir o player.
type Gate struct {
	ln      net.Listener
	arrived chan net.Conn
	mu      sync.Mutex
	conn    net.Conn
	closed  bool
}

// NewGate escuta em path. Close fecha o socket e libera quem estiver esperando.
func NewGate(path string) (*Gate, error) {
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("socket do gate: %w", err)
	}
	g := &Gate{ln: ln, arrived: make(chan net.Conn, 1)}
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.closed {
			_ = c.Close() // o cenário já fechou o gate: o streamlink segue
			return
		}
		g.arrived <- c
	}()
	return g, nil
}

// Env é a variável que leva o streamlink falso a esperar por este gate.
func (g *Gate) Env() string {
	return OpenGateEnv + "=" + g.ln.Addr().String()
}

// Arrived espera o streamlink falso chegar à espera, até timeout.
func (g *Gate) Arrived(timeout time.Duration) error {
	g.mu.Lock()
	got := g.conn != nil
	g.mu.Unlock()
	if got {
		return nil
	}
	select {
	case c := <-g.arrived:
		g.mu.Lock()
		g.conn = c
		g.mu.Unlock()
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("o streamlink falso não chegou ao gate em %v", timeout)
	}
}

// Release deixa o streamlink falso abrir o player. Sem Arrived antes, falha.
func (g *Gate) Release() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.conn == nil {
		return errors.New("gate liberado antes de o streamlink falso chegar")
	}
	if _, err := io.WriteString(g.conn, "abre\n"); err != nil {
		return fmt.Errorf("liberar o gate: %w", err)
	}
	return nil
}

// Close fecha o socket e a conexão: o streamlink que ainda espera segue.
func (g *Gate) Close() {
	_ = g.ln.Close()
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return
	}
	g.closed = true
	if g.conn != nil {
		_ = g.conn.Close()
	}
	select {
	case c := <-g.arrived:
		_ = c.Close()
	default:
	}
}

// WaitGate é o lado do streamlink falso: conecta ao gate em path e espera a
// liberação, ou o fim da conexão se o cenário fechar o gate.
func WaitGate(path string) error {
	c, err := net.Dial("unix", path)
	if err != nil {
		return fmt.Errorf("conectar ao gate %s: %w", path, err)
	}
	defer func() { _ = c.Close() }()
	var b [1]byte
	if _, err := c.Read(b[:]); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("esperar o gate %s: %w", path, err)
	}
	return nil
}
