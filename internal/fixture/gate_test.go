package fixture

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// newTestGate abre um gate num diretório curto: o caminho de um socket Unix
// tem limite de 104 bytes no macOS, e o t.TempDir de lá passa perto disso.
func newTestGate(t *testing.T) *Gate {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("o gate serve aos cenários da janela, que só rodam no Linux")
	}
	dir, err := os.MkdirTemp("", "gate-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	g, err := NewGate(filepath.Join(dir, "g.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(g.Close)
	return g
}

// waitGate roda WaitGate do caminho que o gate põe no ambiente.
func waitGate(t *testing.T, g *Gate) <-chan error {
	t.Helper()
	path := g.Env()[len(OpenGateEnv)+1:]
	done := make(chan error, 1)
	go func() { done <- WaitGate(path) }()
	return done
}

// O streamlink falso fica no gate até o cenário liberar, e o cenário sabe
// quando ele chegou.
func TestGateSeguraAteLiberar(t *testing.T) {
	g := newTestGate(t)
	if err := g.Release(); err == nil {
		t.Fatal("Release antes de o streamlink chegar não falhou")
	}
	done := waitGate(t, g)
	if err := g.Arrived(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	// Ausência: sem Release, o streamlink continua esperando.
	select {
	case err := <-done:
		t.Fatalf("WaitGate voltou sem Release: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	if err := g.Release(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("WaitGate: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("WaitGate não voltou depois do Release")
	}
}

// Um cenário que falha antes de liberar fecha o gate, e o streamlink segue em
// vez de ficar preso.
func TestGateFechadoSoltaOStreamlink(t *testing.T) {
	g := newTestGate(t)
	done := waitGate(t, g)
	if err := g.Arrived(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	g.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("WaitGate depois do Close: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("WaitGate não voltou depois do Close")
	}
}

// Sem o streamlink, Arrived reprova no prazo em vez de esperar para sempre.
func TestGateSemStreamlinkEsgotaOPrazo(t *testing.T) {
	g := newTestGate(t)
	if err := g.Arrived(50 * time.Millisecond); err == nil {
		t.Fatal("Arrived sem streamlink não falhou")
	}
}
