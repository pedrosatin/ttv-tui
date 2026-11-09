package player

import (
	"os"
	"strings"
	"testing"
)

// show-text enviado como lista simples aparece na tela como ${volume} literal.
func TestVolumeOSDAsksMpvToExpandTheText(t *testing.T) {
	src, err := os.ReadFile("session.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), `"command": []any{"show-text"`) {
		t.Fatal("show-text em lista simples mostra o texto cru")
	}
	if !strings.Contains(string(src), `"_flags": []any{"expand-properties"}`) {
		t.Fatal("show-text precisa de _flags expand-properties")
	}
}
