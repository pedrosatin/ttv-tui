# ttv-tui

[![CI](https://github.com/pedrosatin/ttv-tui/actions/workflows/ci.yml/badge.svg)](https://github.com/pedrosatin/ttv-tui/actions/workflows/ci.yml)

Twitch no terminal: navegue por categorias e canais, assista à live e converse no
chat, tudo sem abrir o navegador.

- **Navegar**: categorias ao vivo e canais por categoria, com busca por nome.
- **Assistir**: streamlink + mpv em qualidade cheia, com anúncios já filtrados pelo
  streamlink e controle de pause/volume pela própria TUI (IPC do mpv).
- **Conversar**: chat IRC integrado, leitura e envio de mensagens.

## Requisitos

- Linux (testado no Omarchy/Hyprland)
- `streamlink`, `mpv` e `sqlite3` no `PATH`
- Um navegador logado na Twitch (Chrome, Chromium, Brave, Edge, Vivaldi, Opera ou
  Firefox) — só para enviar mensagem no chat; ler o chat é anônimo

## Instalação

```bash
go install github.com/pedrosatin/ttv-tui/cmd/ttv-tui@latest
```

Pacote do AUR (`ttv-tui-bin`) e binários de release chegam junto com a primeira versão.

## Uso

```bash
ttv-tui login    # uma vez: abre o OAuth da Twitch e grava o token
ttv-tui          # abre a TUI
```

Teclas principais:

| Tecla | Ação |
| --- | --- |
| `/` | filtrar a lista |
| `Enter` | abrir categoria / assistir canal |
| `Tab` | alternar entre lista e chat |
| `i` | escrever no chat |
| `Ctrl+S` | buscar canal no Twitch inteiro |
| `Ctrl+R` | voltar para as categorias |
| `Ctrl+Q` | sair |

## Estado

Projeto em construção: spec e tickets são acompanhados localmente, fora do
repositório. `docs/prototype/twtui.py` é um esboço em Python/Textual usado para validar
o desenho da interface; não é o produto.

---

# English

Twitch in your terminal: browse live categories and channels, watch streams, and
participate in chat without opening a web browser.

- **Browse**: live categories and streams per category, with search.
- **Watch**: full-quality video via streamlink + mpv, with ads filtered and
  in-TUI controls for pause/volume (mpv IPC).
- **Chat**: integrated IRC chat client for reading and sending messages.

Continuous Integration status: [GitHub Actions CI](https://github.com/pedrosatin/ttv-tui/actions/workflows/ci.yml).

## Requirements

- Linux
- `streamlink`, `mpv`, and `sqlite3` installed and available in `PATH`
- A supported browser logged in to Twitch (Chrome, Chromium, Brave, Edge, Vivaldi, Opera, or Firefox) — required only for sending chat messages (reading chat is anonymous)

## Installation

### Using `go install`

```bash
go install github.com/pedrosatin/ttv-tui/cmd/ttv-tui@latest
```

### Arch Linux (AUR)

Once published, install the prebuilt binary package:

```bash
yay -S ttv-tui-bin
```

### Building from Source

```bash
git clone https://github.com/pedrosatin/ttv-tui.git
cd ttv-tui
go build -ldflags "-s -w" -o ttv-tui ./cmd/ttv-tui
```

## Login

Authenticate once to allow chat interactions:

```bash
ttv-tui login
```

This opens Twitch OAuth in your browser and stores credentials securely.

## Keybindings

| Key | Action |
| --- | --- |
| `/` | Filter list |
| `Enter` | Open category / Watch channel |
| `Tab` | Switch focus between list and chat |
| `i` | Focus chat input |
| `Ctrl+S` | Global search across Twitch |
| `Ctrl+R` | Return to categories |
| `Ctrl+Q` | Quit |

## License

MIT — see [LICENSE](LICENSE).
