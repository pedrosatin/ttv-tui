# Cenários de UX

Um cenário descreve o que o usuário faz (as teclas) e o que ele vê (a tela inteira). Ele roda no `go test ./...`, sem
rede, sem relógio e sem terminal, e reprova quando a tela muda sem querer. Mudança de tecla, de tela ou de fluxo vem
com um cenário. Bug de UX entra primeiro como cenário que reprova e só depois ganha a correção.

O motor fica em [`internal/ui/cenarios_motor_test.go`](../internal/ui/cenarios_motor_test.go), os cenários de
referência em [`cenarios_test.go`](../internal/ui/cenarios_test.go) e as telas gravadas em
[`internal/ui/testdata/cenarios/`](../internal/ui/testdata/cenarios/).

## Escrever um cenário

```go
// O usuário busca "gaules" com Ctrl+F e abre o primeiro resultado.
func TestCenarioBuscaEAbreCanal(t *testing.T) {
	eachSize(t, "busca-e-abre-canal", func(t *testing.T, width, height int) Model {
		m, w := newScenario(t, width, height)
		m = play(t, m, "ctrl+f gaules enter enter")
		if !slices.Equal(w.playback.started, []string{"gaules"}) {
			t.Fatalf("player abriu %q, quer gaules", w.playback.started)
		}
		return m
	})
}
```

- `newScenario(t, largura, altura, opções...)` abre o app em pt-BR com o catálogo de fixture (três categorias, dois
  canais por categoria, dois resultados de busca), o player e o chat falsos, e as categorias já carregadas. As opções
  são as de `New`, como `WithFavorites`.
- `play(t, m, "...")` aplica as teclas uma a uma e resolve os comandos de cada tecla antes da próxima.
- `feed(t, m, msg)` entrega uma mensagem de fora, como `PlayerStatusMsg`. `chatLines(t, m, ...)` conecta a sala e
  põe mensagens no chat.
- `eachSize` roda o cenário em 80x24 e 120x40 e compara a tela final com
  `testdata/cenarios/<nome>-<largura>x<altura>.golden`.
- O comentário do teste conta o cenário na voz do usuário. As verificações extras conferem o que a tela não mostra: o
  que foi buscado, qual canal o player abriu.

## A notação das teclas

Os itens se separam por espaço:

| Item | Exemplo | O que o terminal entrega |
|---|---|---|
| Nome de tecla do bubbletea | `enter`, `esc`, `tab`, `shift+tab`, `ctrl+f`, `alt+up`, `pgdown`, `home`, `f1` | a tecla |
| `space` | `space`, `alt+space` | a barra de espaço |
| Um caractere | `g`, `/`, `?`, `+` | o caractere |
| Texto solto | `gaules` | uma tecla por letra |
| Texto entre aspas | `"enter"` | as letras, mesmo sendo nome de tecla |

O nome é o de `tea.KeyMsg.String()`, o mesmo do mapa de teclas do config. Um nome com `+` que não existe, como
`ctrl+ç`, reprova o teste em vez de virar texto.

## Comandos e tempo

Os comandos que o modelo devolve (`tea.Cmd`) rodam na hora, em ordem, até não sobrar nenhum. O que vai à rede chega
pelos fakes do cenário. Os temporizadores do modelo passam por `m.after`: num cenário eles não disparam, e a tela
gravada é a de logo depois da tecla, com o aviso ainda à vista. Um comando que não volta em 10 segundos reprova o
teste com o nome do problema. Na prática, é um `tea.Tick` chamado fora de `m.after` ou um fake que falta.

Golden só para tela estável: dados de fixture, nada de hora nem contagem ao vivo. O que varia vira verificação por
propriedade.

## Invariantes

[`invariantes_test.go`](../internal/ui/invariantes_test.go) leva o app a cada modo principal (categorias, canais,
prompt de busca, busca, filtro, chat, escrita e ajuda) em 60, 79, 80, 100 e 120 colunas e confere:

- nenhuma linha passa da largura e a tela tem a altura do terminal;
- o foco aparece: a linha destacada com `▸`, o cursor `█` da escrita, a mensagem marcada com `▌` ou o título da
  ajuda;
- o rodapé mostra a dica de ajuda.

Um modo novo entra na lista `uxModes`, com o caminho de teclas até ele.

## Atualizar um golden de propósito

Quando a tela muda por decisão, regrave só os cenários afetados e leia o diff antes de commitar:

```bash
go test ./internal/ui -run 'TestCenarioBuscaEAbreCanal' -update
git diff internal/ui/testdata/cenarios/
```

Sem `-update`, a falha lista só as linhas diferentes, numeradas, com a do golden e a da tela. Para gravar um cenário
novo, o mesmo comando cria o arquivo. O golden entra no mesmo commit da mudança que o explica.
