# Janela única no Windows, no macOS e no X11

O [ADR 0011](0011-janela-unica.md) deixou a janela só no Linux com Wayland. Uma prova de conceito
no Windows 11 amd64 (issue #44) abriu a TUI, o chat e o vídeo de uma live real na mesma janela e
mostrou o que muda fora do Linux. Este ADR registra essas diferenças e o que vale em cada sistema.

## Decisão

O `ttv-tui` tenta a janela em qualquer sistema em que o `ttv-tui-window` (`ttv-tui-window.exe` no
Windows) esteja ao lado dele e o `--probe` passe. No Linux e nos BSDs, ele só roda o probe com
`WAYLAND_DISPLAY` ou `DISPLAY` definido, para não testar uma janela num console de texto. No
Windows e no macOS, o probe decide. A janela só força o driver Wayland do SDL quando há
`WAYLAND_DISPLAY`; fora disso, o SDL escolhe X11, Win32 ou Cocoa.

Os contratos do ADR 0011 continuam iguais: `TTV_TUI_VIDEO_HOST`, o shim chamado `mpv` no `PATH`,
o IPC JSON e o filtro de anúncio do streamlink (regra 1). O que muda é o transporte de cada sistema:

| Peça | Linux e macOS | Windows |
|---|---|---|
| Terminal da TUI | PTY (`creack/pty`) | ConPTY (`CreatePseudoConsole`) |
| Shim | link simbólico `mpv` | cópias `mpv.exe` e `mpv.com` |
| Stream para o libmpv | `SCM_RIGHTS` e `fdclose://N` | named pipe `\\.\pipe\ttv-win-stream-…` |
| Consulta sem IPC (`--version`) | `exec` do mpv real | subprocesso com o código de saída |
| Fontes | `fc-match` (macOS: pastas do sistema) | `%WINDIR%\Fonts` |
| Encerrar a TUI | `SIGHUP` | fechar o ConPTY (`CTRL_CLOSE_EVENT`, `SIGTERM` no Go) |

## Windows

- O `opengl32.dll` só exporta OpenGL 1.1. As cinco funções que o `gl_reset` usa vêm de
  `SDL_GL_GetProcAddress`; sem elas (driver básico, RDP), o painel continua e o vídeo falha ao
  criar o render context.
- Sob o ConPTY, o `Stat` do stdin da TUI responde named pipe. O `isTerminal` aceita esse caso só no
  Windows e só com `TTV_TUI_VIDEO_HOST` definido; no Unix a janela dá um PTY de verdade.
- O bloco de ambiente do `CreateProcess` com `CREATE_UNICODE_ENVIRONMENT` sai sem nomes repetidos
  (sem diferenciar maiúsculas) e ordenado. Com o `PATH` herdado repetido, o Windows ignorava o
  diretório do shim.
- O streamlink acha o player com `shutil.which`, que segue o `PATHEXT` e pode devolver `mpv.EXE`
  ou `mpv.COM`. Por isso o shim existe com as duas extensões e o nome é comparado em minúsculas.
- As cópias do shim ficam numa pasta temporária, sem as DLLs do SDL3 e do libmpv. O `PATH` da TUI
  inclui a pasta do `ttv-tui-window.exe` logo depois da do shim.
- O shim cria o named pipe, manda o nome à janela pelo socket de controle e copia o stdin do
  streamlink para o pipe quando o libmpv conecta. Se a janela fechar o controle antes disso, o
  shim desiste e sai, e o streamlink vê o player encerrar.

## Limites

- Validado à mão: Linux com Wayland (ADR 0011) e a prova de conceito no Windows 11 amd64. A versão
  revisada deste ADR, o X11 e o macOS ainda não foram testados numa máquina real.
- A release só publica o `ttv-tui-window` para Linux amd64. No Windows e no macOS ele é compilado
  a partir do código: no Windows com MSYS2 UCRT64 (`gcc`, `pkgconf`, `sdl3`, `mpv`) e as DLLs ao
  lado do executável; no macOS com `brew install sdl3 mpv`. Publicar esses binários exige decidir
  como distribuir as DLLs do mpv e do FFmpeg e as licenças delas.
- No Windows, a janela continua presa ao console que abriu o `ttv-tui`: fechar esse terminal fecha
  a janela. No Unix, o `setsid` a separa.
- No macOS, o SDL cria um contexto OpenGL legado, que é o que o desenho de pipeline fixa do painel
  usa. Se o libmpv não aceitar esse contexto, o vídeo falha ao abrir e a TUI continua.
