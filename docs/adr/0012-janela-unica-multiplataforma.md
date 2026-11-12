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
  `SDL_GL_GetProcAddress`. Sem driver de GPU (driver básico de vídeo, RDP, VM sem aceleração 3D),
  sobra o OpenGL 1.1 genérico do Windows ("GDI Generic"), sem essas funções: o vídeo falha ao criar
  o render context e o painel continua.
- O painel só usa OpenGL 1.1. Esse OpenGL recusa textura com lado fora de potência de 2 e desenha
  um retângulo branco no lugar dela. Sem OpenGL 2.0 nem `GL_ARB_texture_non_power_of_two`, a
  textura do painel é alocada com lados potência de 2 e desenhada com coordenadas que param onde o
  painel acaba. Um painel maior que o `GL_MAX_TEXTURE_SIZE` (1024 no genérico) vira
  uma grade de texturas. Sem OpenGL 1.2 nem `GL_EXT_bgra`, os pixels vão em `GL_RGBA`. Com OpenGL
  2.0 ou mais, o painel segue em uma textura do tamanho exato. O cálculo fica em
  `window/paneltex.go`, e um erro do OpenGL nesse caminho sai no stderr da janela.
- A janela cria a TUI no ConPTY com `STARTF_USESTDHANDLES` e os três handles nulos, como o
  Windows Terminal faz. Sem isso, a TUI pode ficar com os handles redirecionados da janela, que o
  `ttv-tui` abre com stdin e stdout em `NUL`: o stdin deixa de ser console, o Bubble Tea falha no
  `GetConsoleMode` (issue #44) e a saída não chega à janela. Com o console do ConPTY, o
  `isTerminal` passa sem caso especial. O CI do Windows confere que o filho recebe o console.
- O bloco de ambiente do `CreateProcess` com `CREATE_UNICODE_ENVIRONMENT` sai sem nomes repetidos
  (sem diferenciar maiúsculas) e ordenado. Com o `PATH` herdado repetido, o Windows ignorava o
  diretório do shim.
- O streamlink acha o player com `shutil.which`, que segue o `PATHEXT` e pode devolver `mpv.EXE`
  ou `mpv.COM`. Por isso o shim existe com as duas extensões e o nome é comparado em minúsculas.
- As cópias do shim ficam numa pasta temporária, sem as DLLs do SDL3 e do libmpv. O `PATH` da TUI
  inclui a pasta do `ttv-tui-window.exe` logo depois da do shim.
- O Windows não tem SDL3 nem libmpv do sistema. A release leva um zip com a pasta
  `ttv-tui-window`: o executável, a `libmpv-2.dll` oficial LGPL do projeto mpv (ADR 0013),
  `SDL3.dll` e `libiconv-2.dll` do MSYS2 UCRT64 (`packaging/window-windows.sh`), `THIRD-PARTY.txt`
  e os textos de licença (LGPL-2.1 do mpv, LGPL-3 e GPL-3 do FFmpeg embutido e LGPL do libiconv).
  O `install.ps1` põe essa pasta dentro da `bin` do `ttv-tui`, fora do `PATH` do usuário, e o `ttv-tui`
  procura a janela ali também. Ele passa o próprio caminho em `TTV_WINDOW_TUI`, e a janela
  roda essa mesma TUI. O CI confere que o zip carrega sem o MSYS2 no `PATH`.
- O shim cria o named pipe, manda o nome à janela pelo socket de controle e copia o stdin do
  streamlink para o pipe quando o libmpv conecta. Se a janela fechar o controle antes disso, o
  shim desiste e sai, e o streamlink vê o player encerrar.

## Limites

- Validado à mão: Linux com Wayland (ADR 0011) e a prova de conceito no Windows 11 amd64. O código
  ajustado depois dela, o zip da release, o X11 e o macOS ainda não rodaram numa máquina real.
- O painel em OpenGL 1.1 rodou no Linux, com o Mesa em software anunciando 1.1 sem
  `GL_ARB_texture_non_power_of_two`, e numa VM com Windows 11 sem aceleração 3D: o genérico
  (`1.1.0 / GDI Generic`) aceitou `GL_BGRA`, limitou a textura a 1024 e desenhou o painel de
  1280x720 em duas texturas, sem erro do OpenGL. O vídeo dessa VM não foi testado.
- A release publica o `ttv-tui-window` para Linux amd64 e Windows amd64. No macOS ele é compilado
  a partir do código, com `brew install sdl3 mpv`.
- No Windows, a janela continua presa ao console que abriu o `ttv-tui`: fechar esse terminal fecha
  a janela. No Unix, o `setsid` a separa.
- No macOS, o SDL cria um contexto OpenGL legado, que é o que o desenho de pipeline fixa do painel
  usa. Se o libmpv não aceitar esse contexto, o vídeo falha ao abrir e a TUI continua.
