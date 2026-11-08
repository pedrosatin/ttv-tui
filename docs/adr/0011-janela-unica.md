# Janela única com libmpv

O ADR 0010 trouxe o vídeo para o terminal com sixel, e o operador o reprovou: cerca de 5 fps. O
gargalo é estrutural, porque cada quadro é codificado na CPU e redesenhado pelo terminal. A meta
continuou sendo vídeo, lista e chat na mesma tela, sem janela separada do mpv.

## Medição

Mesma amostra de 1080p60 a 8,3 Mbps, em % de um núcleo e quadros perdidos em 30 s:

| Candidato | CPU | Perdidos |
|---|---|---|
| SDL3 + libmpv, com a TUI real | 15 a 17 | 0 |
| Janela do mpv, referência | 15 a 16 | 0 |
| mpv como tela, TUI em overlay | 27 a 52 | 0 |
| Chromium + hls.js | 30 a 64 | 0 a 31, com 1,25 GB de RAM |
| kitty com `--vo=kitty` | 143 | 465, com áudio 24 s fora de sincronia |

## Decisão

`ttv-tui-window` é um executável separado, em módulo Go próprio (`window/`), porque liga SDL3 e
libmpv por cgo. O `ttv-tui` continua sem cgo e nas três plataformas. Ele roda dentro da janela sem
saber de SDL: num PTY, com um emulador de terminal desenhando as células.

Dois contratos ligam os processos:

- `TTV_TUI_VIDEO_HOST` aponta para um socket em que o TUI publica, em JSON, as células do vídeo.
  Enquanto há reprodução, essas células ficam transparentes e o libmpv desenha por baixo.
- Com o chat sobre o vídeo (`Ctrl+O`), o TUI publica quantas colunas finais do retângulo são do
  chat. A janela as desenha com fundo translúcido e texto opaco, e o vídeo não perde largura.
- Um executável chamado `mpv` entra primeiro no `PATH` do TUI. Ele entrega o stdin do streamlink ao
  core embutido por `SCM_RIGHTS` e fica vivo enquanto a sessão existir. O IPC JSON, o streamlink e
  o filtro de anúncio continuam iguais (regra 1).

O `ttv-tui` abre a janela quando está em Linux com Wayland, tem `ttv-tui-window` ao lado e o
`--probe` dela passa. Qualquer outro caso fica no terminal, assim como `--terminal` e `video` no
config. A janela só existe em Linux amd64 por enquanto.

## Limites

- Windows e macOS não têm janela: o `SCM_RIGHTS` e o OpenGL de pipeline fixa não se aplicam lá.
- X11 fica no terminal.
- Emoji colorido aparece como caixa, pois o rasterizador não lê fontes CBDT.
- O binário é gerado em um container Arch, então exige glibc e SDL3 recentes. Onde o carregamento
  falha, o `--probe` falha e o app fica no terminal.
