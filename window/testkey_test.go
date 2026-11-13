package main

import (
	"encoding/json"
	"testing"
)

func TestParseTestKeyComVariosModificadores(t *testing.T) {
	cases := map[string]testKey{
		"ctrl+alt+Right":   {name: "Right", ctrl: true, alt: true},
		"shift+Tab":        {name: "Tab", shift: true},
		"Alt+Shift+ctrl+x": {name: "x", ctrl: true, alt: true, shift: true},
		"PageDown":         {name: "PageDown"},
		"f":                {name: "f"},
		"+":                {name: "+"},
		"ctrl++":           {name: "+", ctrl: true},
	}
	for in, want := range cases {
		got, ok := parseTestKey(in)
		if !ok || got != want {
			t.Errorf("parseTestKey(%q) = %+v, %v; quer %+v", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "meta+x", "ctrl+", "a b"} {
		if _, ok := parseTestKey(in); ok {
			t.Errorf("parseTestKey(%q) aceitou", in)
		}
	}
}

// Letra solta é texto, como no SDL; com Ctrl ou Alt vira tecla.
func TestTestKeyLetraViraTexto(t *testing.T) {
	cases := []struct {
		in   string
		text string
		ok   bool
	}{
		{"f", "f", true}, {"shift+f", "F", true}, {"Space", " ", true}, {"ç", "ç", true},
		{"ctrl+f", "", false}, {"alt+f", "", false}, {"Return", "", false},
	}
	for _, c := range cases {
		k, _ := parseTestKey(c.in)
		if text, ok := k.text(); text != c.text || ok != c.ok {
			t.Errorf("%q: text() = %q, %v; quer %q, %v", c.in, text, ok, c.text, c.ok)
		}
	}
}

// Fora do modo de teste a janela descarta o resumo de estado da TUI; o painel
// segue decodificado como antes.
func TestEstadoDaTUISoComPortaDeTeste(t *testing.T) {
	var m layoutMsg
	if err := json.Unmarshal([]byte(`{"Row":1,"Col":2,"Rows":3,"Cols":4,"Visible":true}`), &m); err != nil {
		t.Fatal(err)
	}
	if m.TestState != nil || m.Cols != 4 || !m.Visible {
		t.Fatalf("painel decodificado como %+v", m)
	}
	if err := json.Unmarshal([]byte(`{"testState":{"Mode":"channels"}}`), &m); err != nil || m.TestState == nil {
		t.Fatalf("estado não decodificado: %v", err)
	}
	a := &app{}
	a.noteTestState(m.TestState)
	if a.testState != nil {
		t.Fatal("a janela guardou o estado sem a porta de teste")
	}
	a.testing = true
	a.noteTestState(m.TestState)
	if string(a.testState) != `{"Mode":"channels"}` {
		t.Fatalf("estado guardado = %s", a.testState)
	}
}
