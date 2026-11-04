# ttv-tui

[![CI](https://github.com/pedrosatin/ttv-tui/actions/workflows/ci.yml/badge.svg)](https://github.com/pedrosatin/ttv-tui/actions/workflows/ci.yml)

[English](../README.md)

Twitch no terminal para Linux: navegue por categorias e canais ao vivo, assista à live via `streamlink` + `mpv` com filtro de anúncios e controles de reprodução pela própria TUI, e converse no chat IRC sem abrir o navegador.

![Assistindo a um canal ao vivo com chat lado a lado](img/playing-chat.svg)

## Instalação

### Binário da release (v0.1.0, Linux amd64 / arm64)

Baixe o tarball da sua arquitetura (`linux_amd64` ou `linux_arm64`) e o arquivo `checksums.txt` da [release v0.1.0](https://github.com/pedrosatin/ttv-tui/releases/tag/v0.1.0), confira o hash SHA-256 e instale em `~/.local/bin`:

```bash
ARCH=amd64 # ou arm64
curl -LO "https://github.com/pedrosatin/ttv-tui/releases/download/v0.1.0/ttv-tui_0.1.0_linux_${ARCH}.tar.gz"
curl -LO "https://github.com/pedrosatin/ttv-tui/releases/download/v0.1.0/checksums.txt"
sha256sum --ignore-missing -c checksums.txt

mkdir -p ~/.local/bin
tar -xzf "ttv-tui_0.1.0_linux_${ARCH}.tar.gz" -C ~/.local/bin ttv-tui
```

### Com `go install`

Requer Go 1.27 ou superior:

```bash
go install github.com/pedrosatin/ttv-tui/cmd/ttv-tui@v0.1.0
```

### Arch Linux (AUR)

O pacote `ttv-tui-bin` no AUR está planejado, mas ainda não foi publicado. Enquanto isso, é possível gerar e instalar o pacote localmente a partir de [`packaging/PKGBUILD`](../packaging/PKGBUILD):

```bash
cd packaging
makepkg -si
```

### Compilar a partir do código-fonte

Requer Go 1.27:

```bash
git clone https://github.com/pedrosatin/ttv-tui.git
cd ttv-tui
CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=0.1.0" -o ~/.local/bin/ttv-tui ./cmd/ttv-tui
```

## Requisitos

### Obrigatórios (reprodução de vídeo)

- Linux (testado no Omarchy/Hyprland).
- `streamlink` no `PATH` para negociar o stream e filtrar anúncios (sem ele não há vídeo).
- `mpv` no `PATH` para decodificar e exibir o vídeo com controle IPC (sem ele não há vídeo).

### Opcionais (login e chat)

- `sqlite3` no `PATH`: necessário apenas para `ttv-tui login --from-browser` (o app executa o binário `sqlite3` para copiar e consultar o banco de cookies do navegador sem disputar lock). Não é necessário para navegar/assistir anonimamente nem para `ttv-tui login --import twt`.
- Um navegador suportado logado na Twitch (Brave, Chrome, Chromium, Edge, Vivaldi, Opera ou Firefox) apenas para enviar mensagens no chat; navegar pelas categorias, assistir aos vídeos e ler o chat funcionam de forma anônima. Em navegadores da família Chromium, `secret-tool` (`libsecret`) também é necessário no `PATH` para descriptografar o cookie via keyring.

## Uso rápido

```bash
ttv-tui login --from-browser   # extrai e valida o cookie auth-token do navegador
ttv-tui login --import twt     # alternativa: importa o token do twitch-tui (~/.config/twt/config.toml)
ttv-tui                        # abre a TUI
```

Veja também o [guia passo a passo em inglês (walkthrough)](walkthrough.md).

## Atalhos de teclado

| Tecla | Ação |
| --- | --- |
| `↑` / `↓` ou `j` / `k` | Mover seleção na lista |
| `Enter` | Abrir categoria / assistir canal / enviar mensagem no chat |
| `f` | Adicionar ou remover canal destacado dos favoritos |
| `/` | Filtrar a lista atual localmente |
| `Ctrl+S` | Buscar canais na Twitch inteira |
| `r` | Tentar carregar novamente a lista ou o player |
| `Esc` | Sair da escrita, do chat, do filtro ou voltar para as categorias |
| `Ctrl+R` | Voltar para as categorias de qualquer lugar, até digitando |
| `Tab` | Alternar foco entre a lista e o chat |
| `i` | Focar no chat e escrever mensagem |
| `Ctrl+T` | Reconectar ao chat IRC do canal |
| `Space` | Pausar ou retomar o vídeo no `mpv` |
| `+` / `=` e `-` / `_` | Aumentar / diminuir o volume do `mpv` em 5% |
| `Ctrl+X` | Encerrar o vídeo sem sair do aplicativo |
| `?` ou `F1` | Mostrar ou ocultar a ajuda de atalhos |
| `Ctrl+C` ou `Ctrl+Q` | Encerrar o vídeo, desconectar do chat e sair |

## Licença

MIT. Consulte [LICENSE](../LICENSE).
