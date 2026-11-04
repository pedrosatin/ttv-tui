# ttv-tui — regras do projeto

TUI em Go para Twitch: navegar por categorias e canais, assistir via streamlink +
mpv e conversar no chat IRC, sem navegador. Leia o [README](README.md) para o produto e os [ADRs](docs/adr/) para as decisões de
arquitetura.

## Regras que sustentam o desenho

1. **O vídeo não é reimplementado.** A TUI orquestra `streamlink` (token, HLS,
   filtro de anúncio) e `mpv` (decode/GPU), controlando o player por IPC JSON.
   Nada de HLS próprio nem de vídeo em sixel dentro do terminal: o ganho seria
   perder qualidade e queimar CPU.

2. **Acesso a dados fica atrás de uma interface `Source`.** O v1 fala o GraphQL
   público do site; quando existir um app registrado na Twitch, o adapter Helix
   entra atrás da mesma interface sem a UI saber. Nenhum pacote de UI importa
   `net/http` nem conhece nomes de campo do GraphQL.

3. **Login é do app, não emprestado.** Token em
   `~/.config/ttv-tui/config.toml` (0600), criado por `ttv-tui login` via OAuth
   implícito no navegador. `--import twt` copia o token do `twitch-tui` para
   facilitar a migração, mas o app nunca depende desse arquivo.

4. **Um processo para a TUI, o vídeo fora dela.** O player roda em processo
   separado e é encerrado quando a TUI sai ou troca de canal. Um canal por vez.

5. **Silêncio de IPC não é evidência de anúncio.** O estado de intervalo só existe
   com início e fim verificáveis; ausência de eventos do mpv por alguns segundos é
   reprodução normal e nunca vira "anúncio".

6. **Layout de dois painéis.** Lista (categorias → canais) à esquerda, chat à
   direita, foco alternando com `Tab`, entrada de mensagem fixa embaixo. O
   desenho precisa funcionar em 80 colunas.

## Convenções de código

- Go 1.27, módulo `github.com/pedrosatin/ttv-tui`.
- `cmd/ttv-tui` só monta flags e chama `internal/app`.
- `internal/ui`: modelo/update/view do bubbletea, sem I/O direto.
- `internal/twitch`: clientes de dados (`Source`, GQL agora, Helix depois).
- `internal/chat`: cliente IRC, parser de tags, envio de mensagens.
- `internal/player`: streamlink, mpv e o protocolo IPC.
- `internal/config`: leitura/escrita do config e do token.
- Erros: `fmt.Errorf("...: %w", err)`, sem `panic` fora do `main`.
- Testes junto do código (`_test.go`); parser de IRC e `Source` com fixtures.
- `go vet ./...` e `golangci-lint run` limpos antes de qualquer PR.
