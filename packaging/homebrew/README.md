# Homebrew tap

Decisão (2026-11-19): a fórmula publicada usa os **binários** Darwin e Linux
das releases. A v0.10.2 já inclui `ttv-tui_*_darwin_{arm64,amd64}.tar.gz` (e os
equivalentes Linux). Compilar do fonte, como este README sugeria quando as
releases ainda não tinham Darwin, fica de fora. O GoReleaser só gera a fórmula
em `dist/homebrew/Formula/ttv-tui.rb` (`skip_upload: true`). Depois da
attestation e da promoção do rascunho, o workflow publica no tap
`pedrosatin/homebrew-tap` com a deploy key `HOMEBREW_TAP_DEPLOY_KEY`. O ensaio
`workflow_dispatch` gera e confere a fórmula e não escreve no tap.

Instalação:

```bash
brew install pedrosatin/tap/ttv-tui
```

A fórmula declara `mpv` e `streamlink` como dependências. O arquivo
`ttv-tui.rb` neste diretório é só referência local; a fonte publicada é o tap.
