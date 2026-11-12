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
   GNU Lesser General Public License versão 2.1 ou posterior (LGPL-2.1+). O FFmpeg embutido
   estaticamente nessa DLL é compilado com `--enable-version3` (sem `--enable-gpl`), tornando o
   FFmpeg embutido LGPL-3.0 ou posterior (LGPL-3.0+), cuja licença incorpora a GNU GPL-3 por
   referência. A ligação dinâmica do `ttv-tui-window.exe` à `libmpv-2.dll` permite a distribuição
   conforme a seção 6 da LGPL-2.1 e termos da LGPL-3 sem contaminar o restante do executável com
   termos GPL-3, desde que fornecidos os textos de licença, oferta escrita de código-fonte e
   garantia de substituição da DLL pelo usuário.

2. **Tamanho e isolamento**: A biblioteca MSYS2 ligava dinamicamente dezenas de dependências
   individuais de codecs e utilitários. O zip empacotava 134 arquivos (191 MiB descompactados,
   74 MiB compactados). A `libmpv-2.dll` LGPL oficial é estática com relação ao FFmpeg e demais
   codecs, dependendo em runtime apenas de DLLs padrão do sistema Windows, do UCRT e do `libiconv-2.dll`
   (trazido pelo SDL3). O zip mede 58.900.088 bytes (~56 MiB; 58.805.770 bytes antes da inclusão das
   licenças) e contém apenas 9 arquivos: `ttv-tui-window.exe`, `libmpv-2.dll`, `SDL3.dll`,
   `libiconv-2.dll`, `LICENSE.LGPL`, `COPYING.LGPLv3`, `COPYING.GPLv3`, `COPYING.LIB` e `THIRD-PARTY.txt`.

3. **Garantia de código-fonte**: Pela GPL e LGPL, o distribuidor precisa fornecer ou apontar com
   segurança o código-fonte correspondente exato. Os espelhos do MSYS2 removem versões antigas
   de pacotes após atualizações, o que quebrava os apontadores de `packages.msys2.org`. As
   releases oficiais do mpv mantêm tags Git e tarballs imutáveis no GitHub. O link do FFmpeg aponta
   para o commit exato identificado nas strings da DLL (`git-2026-11-04-baa68e6` em
   `https://github.com/FFmpeg/FFmpeg/commit/baa68e68b34e2de912eb7e94342cebd2545f05ed`).
   Limitação conhecida: o build oficial do mpv utiliza a imagem de container
   `ghcr.io/btbn/ffmpeg-builds/win64-lgpl:latest`, sem tag ou digest fixado, de modo que outras
   bibliotecas estáticas embutidas na DLL (como libass, libplacebo e dependências) não possuem
   versão exata identificada.

## Decisão

- O script de empacotamento (`packaging/window-windows.sh`) baixa a release oficial da libmpv do
  projeto mpv (`v0.42.0`, asset `libmpv-v0.42.0-x86_64-w64-mingw32-lgpl.zip`), confere o SHA-256
  do asset antes do uso e valida a marca de versão do commit do FFmpeg na DLL (`git-2026-11-04-baa68e6`).
- A janela compila contra os cabeçalhos (`include/mpv/`) e a biblioteca de importação
  (`libmpv.dll.a`) dessa mesma release via `PKG_CONFIG_PATH`, com conferência de que o `.pc`
  usado pelo `pkg-config` provém dessa release e não do MSYS2.
- A lista de DLLs do zip é estritamente restrita a uma lista branca (`libmpv-2.dll`, `SDL3.dll` e
  `libiconv-2.dll`); qualquer DLL adicional faz o empacotamento e o CI falharem.
- O pacote inclui os textos de licença `LICENSE.LGPL` (mpv, LGPL-2.1), `COPYING.LGPLv3` e
  `COPYING.GPLv3` (FFmpeg embutido, LGPL-3), bem como o texto de licença LGPL do `libiconv` do MSYS2.
- O arquivo `THIRD-PARTY.txt` traz o aviso de uso da libmpv sob LGPL-2.1+, do FFmpeg sob LGPL-3.0+,
  links para o código-fonte do mpv na tag correspondente e do commit exato do FFmpeg, a nota sobre
  a limitação das bibliotecas estáticas da imagem BtbN, oferta escrita do código-fonte válida por
  três anos, a autorização expressa para substituição da `libmpv-2.dll` por versão compatível
  (LGPL-2.1 §6), e a relação dos pacotes MSYS2 que fornecem as demais DLLs.
- O CI do Windows testa o `--probe` com o zip descompactado e sem o MSYS2 no `PATH`, garantindo
  que o carregador resolveu as DLLs empacotadas e todos os imports de símbolos sem erros de runtime.
