# libmpv LGPL oficial no zip do Windows

O [ADR 0012](0012-janela-unica-multiplataforma.md) documentou a inclusão da janela única no
Windows por meio de um pacote zip contendo `ttv-tui-window.exe` e as DLLs coletadas pelo `ntldd -R`
a partir de `/ucrt64/bin` do MSYS2 UCRT64. Esse pacote incluía os binários do `mingw-w64-mpv` e do
`mingw-w64-ffmpeg` do MSYS2. Este ADR registra a substituição dessas DLLs pela `libmpv-2.dll`
oficial compilada em modo LGPL pelo projeto mpv.

## Motivações

1. **Licenciamento**: O pacote `mingw-w64-ffmpeg` do MSYS2 é compilado com `--enable-gpl` e
   `--enable-version3`, aplicando os termos da GPL-3.0 ao FFmpeg e a quem o carrega. A distribuição
   da biblioteca compartilhada sob esses termos impunha obrigações da GPL-3 a todo o pacote. O
   projeto mpv distribui uma versão oficial da `libmpv-2.dll` compilada com `-Dgpl=false` sob a
   GNU Lesser General Public License versão 2.1 ou posterior (LGPL-2.1+), permitindo a ligação
   dinâmica conforme a seção 6 da LGPL sem contaminar o restante da distribuição com termos GPL-3.

2. **Tamanho e isolamento**: A biblioteca MSYS2 ligava dinamicamente dezenas de dependências
   individuais de codecs e utilitários. O zip empacotava 134 arquivos (191 MiB descompactados,
   74 MiB compactados). A `libmpv-2.dll` LGPL oficial é estática com relação ao FFmpeg e demais
   codecs, dependendo em runtime apenas de DLLs padrão do sistema Windows (e do UCRT). O zip
   cai para cerca de 54 MiB e contém apenas o executável, `libmpv-2.dll`, `SDL3.dll` e os arquivos
   de licença.

3. **Garantia de código-fonte**: Pela GPL e LGPL, o distribuidor precisa fornecer ou apontar com
   segurança o código-fonte correspondente exato. Os espelhos do MSYS2 removem versões antigas
   de pacotes após atualizações, o que quebrava os apontadores de `packages.msys2.org`. As
   releases oficiais do mpv mantêm tags Git e tarballs imutáveis no GitHub.

## Decisão

- O script de empacotamento (`packaging/window-windows.sh`) baixa a release oficial da libmpv do
  projeto mpv (`v0.42.0`, asset `libmpv-v0.42.0-x86_64-w64-mingw32-lgpl.zip`) e confere o SHA-256
  do asset antes do uso.
- A janela compila contra os cabeçalhos (`include/mpv/`) e a biblioteca de importação
  (`libmpv.dll.a`) dessa mesma release via `PKG_CONFIG_PATH`.
- Apenas a `libmpv-2.dll` oficial e a `LICENSE.LGPL` entram no pacote para o player; nenhuma DLL
  do mpv ou do FFmpeg do MSYS2 é incluída no zip (o CI falha explicitamente se alguma for
  encontrada).
- O SDL3 continua vindo do pacote do MSYS2 UCRT64 (licença Zlib).
- O arquivo `THIRD-PARTY.txt` traz o aviso de uso da libmpv sob LGPL-2.1+, links para o
  código-fonte do mpv na tag correspondente e para a origem do FFmpeg do build oficial, a cópia
  integral da `LICENSE.LGPL` e a relação dos pacotes MSYS2 que fornecem as demais DLLs.
- O CI do Windows testa o `--probe` com o zip descompactado e sem o MSYS2 no `PATH`, garantindo
  que todas as chamadas de inicialização da ABI carreguem diretamente do `libmpv-2.dll` estático.
