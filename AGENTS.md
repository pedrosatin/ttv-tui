# ttv-tui — regras do projeto

TUI em Go para Twitch: navegar por categorias e canais, assistir via streamlink +
mpv e conversar no chat IRC, sem navegador. Leia o [README](README.md) para o produto e os [ADRs](docs/adr/) para as decisões de
arquitetura.

## Regras que sustentam o desenho

1. **O vídeo não é reimplementado.** A TUI orquestra `streamlink` (token, HLS,
   filtro de anúncio) e `mpv` (decode/GPU), controlando o player por IPC JSON.
   Nada de HLS próprio. Com o `ttv-tui-window` ao lado e o `--probe` passando, o
   padrão é a janela única, com o libmpv embutido ([ADR 0011](docs/adr/0011-janela-unica.md),
   [ADR 0012](docs/adr/0012-janela-unica-multiplataforma.md)); fora dela, a janela
   do mpv.

2. **Acesso a dados de cada plataforma fica atrás de um `Provider`.** O catálogo
   opcional expõe capacidades granulares (`Browser`, `Searcher`, `Resolver`) e o
   chat declara as suas antes de conectar (`Chat`). A UI usa só os tipos de
   `internal/catalog` e não importa `internal/twitch`, `internal/kick`, `internal/chat` nem
   `internal/player`. O chat de cada plataforma é uma
   `catalog.Chat` entregue pelo próprio provider; a ponte de `internal/app` escolhe
   pela plataforma do canal e renumera as gerações numa sequência única. Nenhum
   pacote de UI importa `net/http` nem conhece nomes de campo de APIs de plataformas.

3. **Login é do app, não emprestado.** Token em
   `~/.config/ttv-tui/config.toml` (0600), colhido por `ttv-tui login` do cookie
   `auth-token` do navegador. `--import twt` copia o token do `twitch-tui` para
   facilitar a migração, mas o app nunca depende desse arquivo.

4. **Um processo para a TUI, o vídeo fora dela.** O player roda em processo
   separado e é encerrado quando a TUI sai ou troca de canal. Um canal por vez.

5. **Silêncio de IPC não é evidência de anúncio.** O estado de intervalo só existe
   com início e fim verificáveis; ausência de eventos do mpv por alguns segundos é
   reprodução normal e nunca vira "anúncio".

6. **Layout de dois painéis.** Lista (categorias → canais) à esquerda, chat à
   direita, foco alternando com `Tab`, entrada de mensagem fixa embaixo. O
   desenho precisa funcionar em 80 colunas.

7. **Nunca leia o config real do operador.** Testes e subagents usam
   `XDG_CONFIG_HOME`/`XDG_STATE_HOME` isolados. `~/.config/ttv-tui/config.toml`
   guarda um `access_token` de sessão em texto puro: imprimir, colar em relatório ou
   deixar em log expõe a conta.

8. **Nunca commite enquanto um subagent escreve na mesma árvore.** O commit captura
   estado intermediário (teste pela metade, código não compilando). Espere o subagent
   terminar, revise o diff e só então commite.

## Convenções de código

- Go 1.27, módulo `github.com/pedrosatin/ttv-tui`.
- `cmd/ttv-tui` só monta flags e chama `internal/app`.
- `internal/ui`: modelo/update/view do bubbletea, sem I/O direto.
- `internal/twitch`: clientes de dados (`Source`, GQL agora, Helix depois).
- `internal/chat`: cliente IRC, parser de tags, envio de mensagens.
- `internal/kick`: provider da Kick: JSON do site, chat só leitura pelo Pusher ([ADR 0016](docs/adr/0016-kick-sem-conta.md)).
- `internal/player`: streamlink, mpv e o protocolo IPC.
- `internal/config`: leitura/escrita do config e do token.
- Erros: `fmt.Errorf("...: %w", err)`, sem `panic` fora do `main`.
- Testes junto do código (`_test.go`); parser de IRC e `Source` com fixtures.
- `go vet ./...` e `golangci-lint run` limpos antes de qualquer PR.
