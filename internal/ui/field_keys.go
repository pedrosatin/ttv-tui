package ui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

// As primitivas de edição são as únicas teclas que a TUI trata fora do registro
// de ações (actions.go). Elas pertencem ao campo de texto (busca, filtro e
// mensagem do chat), não a uma ação, e passam na frente dos atalhos enquanto o
// campo tem o foco:
//
//   - o que se digita: runas, texto colado e espaço;
//   - Backspace e Delete apagam o caractere anterior;
//   - Alt+Backspace (a janela manda os mesmos bytes para Ctrl+Backspace) e Ctrl+W
//     apagam a palavra anterior, como no shell;
//   - Ctrl+H não faz nada no campo, para não esconder a lista no meio da frase;
//   - no filtro /, ↑ e ↓ andam pela lista, porque k e j são letras do filtro.
//
// O Tab, mesmo como runa, continua sendo a ação de trocar o painel.

// isFieldEditKey diz se a tecla é uma primitiva do campo de texto, a que passa
// na frente dos atalhos globais e de layout.
func isFieldEditKey(msg tea.KeyMsg) bool {
	switch msg.Type {
	case tea.KeyRunes:
		return len(msg.Runes) != 1 || msg.Runes[0] != '\t'
	case tea.KeySpace, tea.KeyBackspace, tea.KeyDelete, tea.KeyCtrlH, tea.KeyCtrlW:
		return true
	}
	return false
}

// editText aplica uma primitiva de edição ao texto do campo. ok é falso para uma
// tecla que não edita, que o modo do campo procura no registro.
func editText(text string, msg tea.KeyMsg) (string, bool) {
	switch {
	case isWordBackspace(msg):
		return trimLastWord(text), true
	case msg.Type == tea.KeyBackspace || msg.Type == tea.KeyDelete:
		return trimLastRune(text), true
	case msg.Type == tea.KeySpace:
		return text + " ", true
	case msg.Type == tea.KeyRunes:
		return text + sanitizeChatText(string(msg.Runes)), true
	}
	return text, false
}

// fieldArrow é o passo de ↑ (-1) ou ↓ (+1) no filtro, e 0 para outra tecla.
func fieldArrow(msg tea.KeyMsg) int {
	switch msg.Type {
	case tea.KeyUp:
		return -1
	case tea.KeyDown:
		return 1
	}
	return 0
}

// isWordBackspace é a tecla que apaga a palavra anterior: Alt+Backspace (os
// bytes ESC DEL que a janela também manda para Ctrl+Backspace) e Ctrl+W, como
// no shell.
func isWordBackspace(msg tea.KeyMsg) bool {
	return (msg.Type == tea.KeyBackspace && msg.Alt) || msg.Type == tea.KeyCtrlW
}

// trimLastWord apaga a palavra antes do fim e os espaços depois dela, e mantém
// o espaço que a separa da anterior: "fix ab " vira "fix ".
func trimLastWord(s string) string {
	s = strings.TrimRightFunc(s, unicode.IsSpace)
	return s[:strings.LastIndexFunc(s, unicode.IsSpace)+1]
}

func trimLastRune(s string) string {
	if s == "" {
		return ""
	}
	_, size := utf8.DecodeLastRuneInString(s)
	return s[:len(s)-size]
}
