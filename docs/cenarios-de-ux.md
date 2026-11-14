# Cenários de UX

Um cenário descreve o que o usuário faz (as teclas) e o que ele vê (a tela inteira). Ele roda no `go test ./...`, sem
rede, sem relógio e sem terminal, e reprova quando a tela muda sem querer. Mudança de tecla, de tela ou de fluxo vem
com um cenário. Bug de UX entra primeiro como cenário que reprova e só depois ganha a correção.

O motor fica em [`internal/ui/cenarios_motor_test.go`](../internal/ui/cenarios_motor_test.go), os cenários de
referência em [`cenarios_test.go`](../internal/ui/cenarios_test.go) e as telas gravadas em
[`internal/ui/testdata/cenarios/`](../internal/ui/testdata/cenarios/). A janela única tem uma segunda camada, com o
`ttv-tui-window` de verdade: veja [Cenários na janela real](#cenários-na-janela-real).

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

## Cenários na janela real

O motor acima testa o modelo da TUI. Ele não vê o que a janela única faz por cima: a tradução das teclas do SDL em
bytes para o PTY, o vídeo desenhado pelo libmpv, o fundo do chat sobreposto, o tamanho da grade numa janela estreita.
Para isso existe uma segunda camada, em [`tools/window-scenarios/`](../tools/window-scenarios/), que abre o
`ttv-tui-window` de verdade com `SDL_VIDEO_DRIVER=offscreen`, sem compositor, sem live e sem rede.

Um cenário precisa da janela quando o defeito ou a mudança passa por `window/`, ou quando depende do que só ela
desenha: vídeo, chat sobre o vídeo, opacidade, redimensionamento, teclas com Ctrl ou Alt que o terminal entrega de
outro jeito. O que se resolve no modelo continua no motor da TUI, que é mais rápido e grava a tela num golden.

```bash
go test -tags cenarios -count=1 ./tools/window-scenarios
```

O comando compila `ttv-tui`, `ttv-tui-window` e o streamlink falso, roda os cenários e apaga tudo no fim, passe ou
falhe. Precisa de Linux com SDL3, libmpv e um OpenGL que funcione sem tela (no CI, o llvmpipe do mesa); não usa
Hyprland nem a configuração do operador. `TTV_CENARIOS_BUILD` muda onde os binários são compilados. O job
`Window (Arch)` do CI roda o mesmo comando.

### O que cada janela recebe

`openWindow(t, setup{...})` monta, só para aquele teste:

- `XDG_CONFIG_HOME`, `XDG_STATE_HOME`, `XDG_DATA_HOME`, `XDG_CACHE_HOME`, `XDG_RUNTIME_DIR` e `HOME` num diretório
  temporário, com o `config.toml` de partida que o cenário pedir em `setup.config` e o `state.json` (favoritos e
  recentes) em `setup.state`;
- o GQL de fixture ([`internal/fixture`](../internal/fixture/)): 60 categorias, a primeira com os canais
  `fixture_um`, `fixture_dois` e `fixture_tres`;
- o IRC de fixture, com `setup.irc.Messages` mensagens logo depois do JOIN e, se `Every` for maior que zero, mais
  uma a cada intervalo;
- o `streamlink` falso no `PATH`, que abre o player como o streamlink e entrega um clipe vermelho em YUV4MPEG2;
- as variáveis de `setup.env`. Com `fixture.OpenDelayEnv` (`TTV_FIXTURE_OPEN_DELAY=3s`), o streamlink falso espera
  antes de abrir o player, e o cenário vê a tela de carregamento.

Os emojis usam a fonte de emoji do sistema, que o `fc-match emoji` indica. No CI é a `noto-fonts-emoji`; sem ela, o
cenário dos emojis reprova.

A janela fecha pelo mesmo caminho do botão de fechar. O que sobrar com a marca do cenário (TUI, streamlink, shim) é
morto, e a sobra reprova o teste. Na falha, o log traz a tela, o `state` e a saída da janela.

### Ganchos de teste

O produto só usa os fixtures com as variáveis de teste. Sem elas nada muda. As de endereço aceitam só IP literal de
loopback (`127.0.0.0/8` ou `::1`); um nome, mesmo `localhost`, é recusado, porque a resolução depende do ambiente:

| Variável | Quem lê | Efeito |
|---|---|---|
| `TTV_WINDOW_TEST` | janela | abre o socket de teste no caminho dado |
| `TTV_TUI_TEST_GQL` | TUI | troca o endpoint do GQL da Twitch, por exemplo `http://127.0.0.1:4567/gql` |
| `TTV_TUI_TEST_IRC` | TUI | troca o IRC da Twitch por `host:porta` em TCP sem TLS |
| `TTV_TUI_TEST_STATE` | TUI | com `1`, a TUI manda o resumo do estado à janela, que o guarda só com `TTV_WINDOW_TEST` |

`TTV_TUI_TEST_IRC` não vale quando o config tem `access_token`: o handshake manda o token em `PASS oauth:`, e o dial
de teste não usa TLS, então quem controla o ambiente leria a credencial num servidor próprio no loopback. Com token, o
chat vai ao IRC da Twitch como sempre, e os cenários rodam sem login. O GQL não tem a mesma restrição porque o
cliente manda só o `Client-Id` público, sem token. `TTV_TUI_TEST_STATE=1` manda o resumo do estado (sem token) pelo
socket de layout, que liga a TUI à janela.

### O socket de teste

Uma linha por conexão, a resposta até o fim da conexão:

| Comando | Resposta |
|---|---|
| `key ctrl+alt+Right`, `key shift+Tab`, `key PageDown` | `ok`; a tecla passa pelo mesmo caminho do teclado (`keyBytes`) |
| `key f`, `key shift+f`, `key Space` | `ok`; letra sem Ctrl nem Alt é texto, como o SDL entrega |
| `text abc`, `raw 1b5b41` | `ok`; texto ou bytes em hex, na mesma fila das teclas |
| `screen` | o texto da grade, uma linha por linha |
| `state` | JSON com a geometria da janela (`window`) e o resumo da TUI (`tui`): foco, modo, layout, busca, aviso, item destacado, chat e o config que ela aplica |
| `resize W H` | a grade nova, `colunas linhas`, depois de a janela ter `W`x`H` pixels |
| `pixel X Y`, `fbpixel X Y` | o alfa de uma célula; a cor RGB de um pixel no quadro que a GPU desenhou |
| `stats`, `stats reset` | os contadores de desempenho; zera os da sessão |
| `quit` | fecha a janela |

`press` no teste espera o campo `Keys` do `state` subir: o estado lido depois já é o de depois das teclas.

### Escrever um cenário

```go
// O espectador abre o primeiro canal e põe o chat sobre o vídeo com Ctrl+O; o
// config guarda a escolha.
func TestJanelaChatSobreOVideo(t *testing.T) {
	t.Parallel()
	w := openWindow(t, setup{irc: fixture.IRCConfig{Messages: 5}})
	w.openFirstChannel()
	w.press("ctrl+o")
	expect(t, func() error {
		if !strings.Contains(w.savedConfig(), "chat_overlay = true") {
			return errors.New("o config não guardou o chat sobre o vídeo")
		}
		return nil
	})
}
```

A verificação final vai em `expect`, que repete a função até ela passar ou o prazo acabar. Ela confere o que o
usuário vê: o texto da grade (`w.screen()`), um pixel (`w.fbpixel`), o `state` ou o config salvo
(`w.savedConfig()`). Os passos de preparação usam `waitState` e reprovam o teste quando falham, porque uma
preparação quebrada aponta defeito no próprio cenário.

### Bug conhecido

Bug de UX entra primeiro como cenário que reprova na `main`. Para o CI não ficar vermelho até a correção, a
verificação final usa `expectKnownBug(t, "W5", check, why)`. `check` devolve `(bugPresent bool, err error)` e é
repetida por até três segundos:

- `err` diferente de nil reprova na hora. É falha de infraestrutura (socket sem resposta, `state` ilegível, mensagem
  de fixture que não chegou), e nunca vira `SKIP`;
- `bugPresent` verdadeiro encerra como `SKIP`, com o ID e o texto de `why`, que descreve o que a janela mostrou;
- `bugPresent` falso em todas as leituras até o fim do prazo reprova com "o bug conhecido W5 não aparece mais". A
  correção troca `expectKnownBug` por `expect` no mesmo commit, e o cenário vira guarda.

A ausência só conta no fim do prazo porque uma leitura cedo demais ainda pode ser o estado de antes do defeito. A
preparação usa `waitState` e `waitScreen`, que reprovam quando falham. Um `SKIP` quer dizer que a preparação chegou
ao ponto do bug e que a verificação leu o defeito sem erro; um defeito que se manifeste de outro jeito, fora do que
`check` olha, continua invisível para o cenário.

[`knownbug_test.go`](../tools/window-scenarios/knownbug_test.go) testa o mecanismo sem abrir janela e roda no
`go test ./...`.
