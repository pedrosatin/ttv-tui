# ttv-tui

[![CI](https://github.com/pedrosatin/ttv-tui/actions/workflows/ci.yml/badge.svg)](https://github.com/pedrosatin/ttv-tui/actions/workflows/ci.yml)

[English](../README.md)

Twitch no terminal para Linux, macOS e Windows. Navegue por categorias e canais ao vivo, assista às transmissões via `streamlink` e `mpv` com filtro de anúncios e controles de reprodução pela própria TUI, e converse no chat IRC sem abrir o navegador.


## Suporte na v1.8

| Sistema / arquitetura | Suporte |
| --- | --- |
| Linux amd64 / arm64 | Suportado |
| macOS arm64 (Apple Silicon) | Suportado |
| Windows x64 (amd64) | Suportado |
| macOS Intel (darwin/amd64) | Experimental; compila, sem compromisso de suporte |
| Windows ARM (windows/arm64) | Experimental; compila, sem compromisso de suporte |

A matriz define os alvos da v1.8. Compilação cruzada não comprova reprodução, áudio, login, clipboard ou encerramento em máquina real. A release v0.6.0 inclui arquivos para os três sistemas operacionais.

Navegação, busca, favoritos locais, chat IRC e vídeo externo com pausa/volume compõem o escopo nos alvos suportados. Use um terminal com Unicode e cores ANSI, com pelo menos 80 colunas para a divisão lado a lado; o modo só-chat funciona abaixo dessa largura. No Linux, vídeo exige uma sessão gráfica e áudio configurados. No Windows, use Windows Terminal; testes automatizados de ConPTY ficam fora desta versão.

## Instalação

### Linux e macOS: script de instalação

O script [`packaging/install.sh`](../packaging/install.sh) baixa o tarball da release e o arquivo `checksums.txt`, valida o hash SHA-256 com `sha256sum` ou `shasum -a 256` e instala o binário em `~/.local/bin/ttv-tui`, o atalho `.desktop` em `~/.local/share/applications/ttv-tui.desktop` e o ícone em `~/.local/share/icons/hicolor/scalable/apps/ttv-tui.svg`:

```bash
# Instalar a release mais recente ou informar uma versão como 0.6.0
./packaging/install.sh
./packaging/install.sh 0.6.0

# Exibir o plano de instalação sem baixar arquivos nem alterar o sistema
./packaging/install.sh --dry-run 0.6.0

# Instalar em um prefixo personalizado
./packaging/install.sh --prefix "$HOME/.local" 0.6.0
```

Também é possível executar o script diretamente do GitHub:

```bash
curl -fsSL https://raw.githubusercontent.com/pedrosatin/ttv-tui/main/packaging/install.sh | bash -s -- 0.6.0
```

No macOS, o script instala apenas o binário em `~/.local/bin`; atalho e ícone são exclusivos do Linux. Use o arquivo `darwin_arm64` para Apple Silicon. Binários sem notarização baixados pelo navegador podem exigir liberação em Ajustes do Sistema → Privacidade e Segurança (Gatekeeper), após conferir origem e checksum. Não há notarização paga. Downloads pelo script/Homebrew não aplicam a quarentena do navegador.

### Windows x64: PowerShell

Em um checkout, instale a versão 0.6.0:

```powershell
.\packaging\install.ps1 -DryRun
.\packaging\install.ps1 -Version 0.6.0
winget install --id shinchiro.mpv -e
winget install --id streamlink.streamlink -e
```

O script verifica SHA-256, instala em `%LOCALAPPDATA%\Programs\ttv-tui\bin` e adiciona esse diretório ao PATH do usuário. Abra um novo terminal.

### Homebrew (macOS) e Scoop (Windows)

A [fórmula Homebrew](../packaging/homebrew/README.md) usa a release v0.6.0 para macOS em Apple Silicon e Intel. Ela ainda não foi publicada nem validada no macOS. Após publicar o tap, o comando previsto é `brew install pedrosatin/tap/ttv-tui`. As dependências podem ser instaladas com `brew install mpv streamlink`.

O [manifesto Scoop](../packaging/scoop/ttv-tui.json) usa a release v0.6.0 para Windows amd64 e seu SHA-256. Para as dependências, use `scoop bucket add extras` e `scoop install extras/mpv extras/streamlink`.

### Binário da release (v0.6.0)

Baixe o arquivo do seu sistema e arquitetura e o `checksums.txt` da [release v0.6.0](https://github.com/pedrosatin/ttv-tui/releases/tag/v0.6.0). Exemplos: `ttv-tui_0.6.0_linux_amd64.tar.gz`, `ttv-tui_0.6.0_darwin_arm64.tar.gz` e `ttv-tui_0.6.0_windows_amd64.zip`. Linux e macOS também têm a outra arquitetura em tar.gz; Windows arm64 está disponível em zip. Consulte a matriz de suporte acima.

Confira o SHA-256 do arquivo com a entrada correspondente em `checksums.txt` antes de extraí-lo. No Linux use `sha256sum`, no macOS `shasum -a 256` ou, no PowerShell do Windows, `Get-FileHash -Algorithm SHA256`. Extraia `ttv-tui` (ou `ttv-tui.exe` no Windows) e coloque-o em um diretório no `PATH`.

### Com `go install`

Requer Go 1.27 ou superior:

```bash
go install github.com/pedrosatin/ttv-tui/cmd/ttv-tui@v0.6.0
```

### Arch Linux (AUR)

O pacote `ttv-tui-bin` no AUR está planejado, mas ainda não foi publicado. Enquanto ele não estiver disponível no AUR, gere e instale o pacote diretamente a partir de [`packaging/PKGBUILD`](../packaging/PKGBUILD):

```bash
cd packaging
makepkg -si
```

### Compilar a partir do código-fonte

Requer Go 1.27:

```bash
git clone https://github.com/pedrosatin/ttv-tui.git
cd ttv-tui
CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=0.6.0" -o ~/.local/bin/ttv-tui ./cmd/ttv-tui
```

### Atalho de desktop

O script [`packaging/install.sh`](../packaging/install.sh) e o [`packaging/PKGBUILD`](../packaging/PKGBUILD) já instalam [`packaging/ttv-tui.desktop`](../packaging/ttv-tui.desktop) e [`assets/ttv-tui.svg`](../assets/ttv-tui.svg). Se você instalou o binário com `go install` ou `go build`, copie os arquivos manualmente:

```bash
install -Dm644 packaging/ttv-tui.desktop ~/.local/share/applications/ttv-tui.desktop
install -Dm644 assets/ttv-tui.svg ~/.local/share/icons/hicolor/scalable/apps/ttv-tui.svg
update-desktop-database ~/.local/share/applications
```

O atalho executa `xdg-terminal-exec ttv-tui` com `Terminal=false` e `StartupWMClass=ttv-tui`. Caso `xdg-terminal-exec` não esteja instalado no sistema, edite `~/.local/share/applications/ttv-tui.desktop` para usar `Terminal=true` com `Exec=ttv-tui`, ou chame o seu emulador de terminal diretamente, como `Exec=alacritty --class ttv-tui -e ttv-tui` ou `Exec=kitty --app-id ttv-tui ttv-tui`.

## Requisitos

### Obrigatórios (reprodução de vídeo)

- Um dos sistemas da matriz de suporte e um terminal com Unicode/ANSI.
- `streamlink` no `PATH` para negociar a reprodução do stream e filtrar blocos de anúncios. Sem ele, não há reprodução de vídeo.
- `mpv` no `PATH` para decodificar e exibir o vídeo em janela própria com controle por socket JSON IPC local. Sem ele, não há reprodução de vídeo.

### Opcionais (login e chat)

- `sqlite3` no `PATH`. O `ttv-tui` executa o binário `sqlite3` apenas durante `ttv-tui login --from-browser` para copiar e consultar o banco de cookies do navegador pelo comando `.backup` do SQLite sem disputar lock com o navegador aberto. Não é necessário para navegar e assistir de forma anônima nem para importar credenciais via `ttv-tui login --import twt`.
- Um navegador compatível logado em `twitch.tv` (Brave, Chrome, Chromium, Edge, Vivaldi, Opera ou Firefox) caso queira enviar mensagens no chat via `--from-browser`. No Linux, nos navegadores da família Chromium, `secret-tool` (`libsecret`) também precisa estar no `PATH` para ler o segredo OSCrypt Safe Storage no keyring da sessão. Navegar pelas categorias, assistir aos vídeos e ler o chat funcionam de forma anônima sem login.

## Janela única (Linux, Wayland)

No Linux com Wayland, o `ttv-tui` abre uma janela só, com a lista à esquerda, o vídeo no centro e o chat à direita. Não existe janela separada do mpv. Ela precisa de SDL3 e libmpv; sem eles, ou com `--terminal`, o app fica no terminal.

| Tecla | Ação |
|---|---|
| `Ctrl+H` / `Ctrl+L` | Mostra ou esconde a lista / o chat |
| `Ctrl+→` / `Ctrl+←` | Alarga / estreita a lista |
| `Alt+←` / `Alt+→` | Alarga / estreita o chat |

As larguras ficam salvas em `list_columns` e `chat_columns` no `config.toml`. Emoji colorido aparece como caixa por enquanto. Veja o [ADR 0011](adr/0011-janela-unica.md).

## Uso rápido

1. Configure um token da Twitch se quiser enviar mensagens no chat. Se você já tem o `twitch-tui` (`twt`) configurado em `~/.config/twt/config.toml` ou prefere um token restrito ao chat sem ler cookies do navegador nem o keyring da sessão, dê preferência à importação do `twt`:

```bash
ttv-tui login --import twt
```

Caso contrário, extraia o cookie de sessão `auth-token` de um navegador instalado. Consulte [Login e segurança](#login-e-segurança) sobre o alcance do segredo do keyring e do cookie de sessão:

```bash
ttv-tui login --from-browser
```

Se houver mais de um perfil de navegador instalado, passe `--browser` e `--profile` diretamente ou escolha na lista interativa:

```bash
ttv-tui login --from-browser --browser brave --profile Default
```

2. Inicie a interface:

```bash
ttv-tui
```

O idioma da interface segue a opção `--lang` (`en` ou `pt-BR`), o campo `language` em `config.toml` ou as variáveis de ambiente `LC_ALL`, `LC_MESSAGES` e `LANG`, com padrão em inglês.

## Passo a passo

Ao iniciar, o `ttv-tui` carrega as categorias ao vivo mais assistidas na Twitch.


Selecione uma categoria com `Enter` para listar os canais ao vivo e pressione `Enter` sobre um canal para abrir a reprodução de vídeo no `mpv` e entrar no chat IRC do canal no painel direito.


Pressione `?` ou `F1` a qualquer momento para exibir o quadro de atalhos de teclado.


Para um roteiro passo a passo em inglês cobrindo instalação, autenticação, troca de canais, favoritos, chat, terminais estreitos e encerramento, consulte o [walkthrough completo](walkthrough.md).

## Atalhos de teclado

Pressione `?` ou `F1` para abrir a ajuda modal. Ela captura as teclas; use `↑`/`↓`, `PgUp`/`PgDn` ou `Home`/`End` para rolar e `?`, `Esc`, `F1` ou `q` para fechar. As tabelas abaixo trazem todos os atalhos, incluindo dois detalhes omitidos no quadro resumido da tela: as teclas `=` e `_` como alternativas para o volume e o funcionamento de `F1` durante a digitação em campos de texto.

### Navegação na lista e busca

| Tecla | Ação |
| --- | --- |
| `↑` / `↓` ou `j` / `k` | Mover a seleção para cima ou para baixo na lista e carregar mais itens no fim |
| `Enter` | Abrir a categoria destacada ou iniciar a reprodução do canal destacado |
| `f` | Adicionar ou remover o canal destacado dos Favoritos |
| `/` | Filtrar a lista atual localmente (`Enter` mantém o filtro, `Esc` limpa) |
| `Ctrl+F` | Buscar canais em toda a Twitch, reaproveitando o texto ativo de `/` se houver |
| `Ctrl+S` | Igual a `Ctrl+F` (atalho alternativo). Funciona em qualquer terminal: o `ttv-tui` roda em modo raw, o que desliga o controle de fluxo por software do terminal |
| `r` | Tentar carregar a lista novamente ou repetir a reprodução após falha no player |
| `Esc` | Limpar o filtro ativo ou voltar de canais e resultados de busca para as categorias |
| `Ctrl+R` | Voltar para Categorias ao vivo de qualquer tela, mesmo durante a digitação |
| `Tab` | Alternar o foco do teclado entre o painel da lista e o painel do chat |

### Chat

| Tecla | Ação |
| --- | --- |
| `↑` / `k`, `↓` / `j`, `PgUp`, `Home` | Rolar o histórico do chat; a seleção acompanha a mensagem na base visível |
| `End` / `G` | Voltar a acompanhar o chat ao vivo |
| `y` | Copiar a mensagem selecionada do chat (ou a mais recente ao vivo) via `wl-copy`, `xclip` ou OSC 52 |
| `i` | Focar no chat e iniciar a digitação de uma mensagem no canal atual |
| `Enter` | Enviar a mensagem escrita para o chat |
| `Esc` | Sair do campo de mensagem preservando o rascunho ou devolver o foco para a lista |
| `Ctrl+T` | Reconectar ao chat IRC do canal atual |

### Layout

| Tecla | Ação |
| --- | --- |
| `Ctrl+W` | Alternar layouts: lista + chat → só chat → só lista → foco (ambos ocultos) |
| `Ctrl+H` | Mostrar ou ocultar o painel da lista |
| `Ctrl+L` | Mostrar ou ocultar o painel do chat |

O layout vale para a sessão: a barra indica o ativo (`layout: chat`, `layout: lista` ou `layout: foco`) e ocultar o
chat nunca dispara o aviso de terminal estreito. Sem canal aberto, as teclas de layout mantêm a lista visível e avisam que é preciso abrir um canal com `Enter`. A barra de status usa `▶` para reprodução, `‖` para pausa, `✖` para erros e `!` para avisos, sem depender da cor.

### Player de vídeo (janela do `mpv`)

| Tecla | Ação |
| --- | --- |
| `Espaço` | Pausar ou retomar a reprodução do vídeo |
| `+` / `=` | Aumentar o volume do `mpv` em 5% |
| `-` / `_` | Diminuir o volume do `mpv` em 5% |
| `Ctrl+X` | Encerrar a reprodução de vídeo sem sair do aplicativo |

Pausa e volume funcionam nas duas janelas, a TUI e a janela do `mpv`, e a barra de status acompanha o volume seja qual for a janela que o alterou. O volume para em 100% nas duas. `Ctrl+X` só funciona na TUI.

O `ttv-tui` substitui os atalhos do `mpv`, então as demais teclas não fazem nada na janela do vídeo, incluindo `q`, `f` e as setas. A janela do vídeo não tem controles próprios, nem barra na tela; tudo passa pela TUI. Para fechar o vídeo, pressione `Ctrl+X` ou feche a janela dele.

### Aplicativo

| Tecla | Ação |
| --- | --- |
| `?` ou `F1` | Mostrar ou ocultar o quadro de atalhos (`F1` funciona mesmo durante a digitação) |
| `Ctrl+Q` | Encerrar o vídeo ativo, desconectar do chat e sair |

`Ctrl+S` e `Ctrl+Q` chegam ao app porque o `ttv-tui` coloca o terminal em modo raw, o que desliga o controle de fluxo por software: o terminal deixa de interceptar `Ctrl+S` para pausar a saída e `Ctrl+Q` para retomá-la. Não é preciso configuração extra. `Ctrl+F` continua sendo o atalho principal de busca.

## Layout

O `ttv-tui` organiza a tela em dois painéis quando um canal está aberto:

- **Painel esquerdo.** Lista de navegação com Favoritos, canais Recentes e Categorias ao vivo, Canais ao vivo ou Resultados da busca.
- **Painel direito.** Chat IRC ao vivo do canal ativo, com indicadores de modo da sala (`slow`, `followers-only`, `subs-only`) no cabeçalho e campo de escrita na base.
- **Barra inferior.** Barra de status com o idioma ativo (`[en]` ou `[pt-BR]`), estado da reprodução, volume, canal em foco e dicas de teclas.

Pressione `Tab` para alternar o foco entre a lista e o chat. O cabeçalho do painel ativo exibe `[foco]` (`[focus]` em inglês).

Adaptação à largura do terminal:

- **100 colunas ou mais.** Divisão lado a lado com cerca de 40% da largura para a lista e 60% para o chat.
- **80 a 99 colunas.** Divisão lado a lado com coluna de chat compacta de no mínimo 34 colunas para manter a leitura da lista.
- **Menos de 80 colunas.** Na divisão lado a lado, o chat fica oculto e aparece um aviso. Pressione `Ctrl+L` para abrir o chat em largura total ou alargue o terminal para restaurar a divisão.


## Vídeo embutido experimental

Definir `video = "sixel"` no `config.toml` acrescenta um layout que desenha o vídeo no terminal com uma faixa de chat. Ele vem desligado, e todos os outros layouts continuam com a janela do `mpv`. O modo embutido é experimental e ainda não foi verificado visualmente em todos os terminais.

A chave de config vale também para o atalho de desktop, que abre o `ttv-tui` sem flags. Valor inválido conta como desligado e o app abre normalmente. Como alternativa avançada, `--video=sixel` liga o modo em uma execução e `--video=` desliga em uma execução; a flag vence o config.

- **Requisitos.** Um terminal com sixel e um `mpv` com a saída de vídeo `sixel`. Os dois são verificados ao iniciar. Se faltar algum, o app funciona como sempre e uma linha no topo da tela diz, desde a abertura e durante toda a sessão, qual lado falhou e o que fazer. `ttv-tui doctor` informa o mesmo sem abrir a TUI.
- **Como iniciar o vídeo.** Com o modo ligado e o suporte confirmado, o app abre no layout de vídeo com a lista de canais sobre o painel. Escolha um canal e ele começa embutido, sem janela do `mpv` e sem precisar pressionar `Ctrl+W` ou `r`. Sem a chave, sem suporte ou sem TTY, o app abre em split e informa o motivo. Se um canal já estiver tocando em uma janela do `mpv` ao entrar no layout de vídeo, `r` o reinicia embutido com o mesmo volume.
- **Área de vídeo e chat.** O vídeo ocupa toda a área disponível do modo `video`, descontada a faixa de chat e a barra de status. `c` oculta ou mostra o chat sem reiniciar o player; em campos de texto, continua sendo uma letra. No `config.toml`, `video_chat_rows = 8` define a altura da faixa, de 3 a 30 linhas, limitada ao espaço disponível. `video_chat_position = "bottom"` coloca o chat abaixo do vídeo; `"top"` o coloca acima. Valores ausentes ou inválidos usam 8 e `"bottom"`. Com o chat oculto, o vídeo ocupa também as linhas da faixa.
- **Como trocar de canal.** `Esc` ou `l` abre a lista sobre o vídeo. Os frames param enquanto ela está aberta e o áudio continua. Escolher um canal ou pressionar `Esc` volta ao vídeo. `Ctrl+R` volta às categorias.
- **Como sair.** `Ctrl+W`, `Ctrl+H` ou `Ctrl+L` deixa o layout. O áudio continua sem imagem e a barra avisa: `r` reinicia o canal na janela do `mpv` e `Ctrl+X` encerra.
- **Limitações.** A 15 fps, o frame é a maior imagem 16:9 que cabe no painel, centralizada. O painel não tem mais teto de 18 linhas; seu tamanho acompanha o terminal e a faixa de chat. Na medição anterior com frames de 480x270 e 640x360, em uma live real, o uso de CPU do `mpv` passou de cerca de 105% para 125%, e a saída de 2,0 para 3,2 MB/s. Não há suporte em kitty, Alacritty, Terminal.app, Windows e tmux. Em terminal que não informa o tamanho da célula, o vídeo fica menor que o painel para a imagem não invadir o chat.
- **Inicialização do player.** Um início travado sem linha de log leva 30 s para informar erro; um socket presente que recusa conexões continua falhando rápido (cerca de 2 s). O log do streamlink vai para um arquivo na sessão.
- **Espera pela imagem.** Depois de redimensionar, ou ao fechar a ajuda, a imagem pode levar cerca de 2,5 s para voltar, porque o `mpv` espera o próximo keyframe. Se a janela encolheu, o painel fica em branco nessa espera, e a lista deixa o último frame na tela. Os frames também dividem a linha com as teclas: em um terminal ou link SSH que aguenta 300 KB/s, uma tecla levou cerca de meio segundo para aparecer.

## Chat

- **Leitura anônima.** A conexão e a leitura do chat de qualquer canal usam login IRC anônimo da Twitch (`justinfan...`) sobre TLS (`irc.chat.twitch.tv:6697`) com tags IRCv3 habilitadas. Nenhuma conta ou token é exigido para ler o chat.
- **Envio autenticado.** Pressionar `i` para redigir uma mensagem exige um token válido salvo em `config.toml`.
- **Copiar mensagem (`y`).** Com o foco no painel do chat, `y` copia a mensagem selecionada (a da base visível, ou a mais recente enquanto o chat acompanha o ao vivo) para a área de transferência do sistema. O conteúdo inclui contexto, por exemplo `canal #login · YYYY-MM-DD HH:MM · nick: texto`, com horário local. Linhas de sistema usam seu texto; mensagens sem horário de recebimento omitem o contexto. Separadores e preenchimento ficam de fora. A seleção por mouse não é usada. A cópia tenta primeiro o buffer do tmux quando está dentro dele, depois (1) `wl-copy` quando `WAYLAND_DISPLAY` existe e `wl-copy` está no `PATH` (o texto permanece depois que o app sai); (2) `xclip -selection clipboard` quando `DISPLAY` existe e `xclip` está no `PATH`; (3) OSC 52 no stderr para SSH/tmux e hosts sem esses helpers. Se um caminho falhar, o próximo é tentado. A barra de status nomeia o caminho que funcionou (`Copiado via wl-copy: …` / `xclip` / `OSC 52`), ou mostra erro claro se todos falharem. Para o caminho OSC 52 dentro do tmux, ative `set-clipboard on` (por exemplo em `~/.tmux.conf`) para a sequência chegar à área de transferência do sistema.
- **Marcadores e comandos do chat.** Mensagens de contas novas recebem `[novo]`. Mensagens apagadas continuam visíveis como `[apagada]` e seu texto pode ser copiado. Comandos com barra são bloqueados com aviso, exceto `/me`; o rascunho é preservado.
- **Limites de envio.** As mensagens enviadas passam por um limitador local alinhado aos limites da Twitch de 20 mensagens a cada 30 segundos no total e 1 mensagem por segundo por canal. Mensagens com intervalo menor que 1 segundo entram em fila automaticamente; se a janela de 20 mensagens estiver cheia, o excedente é marcado como `[discarded: rate limit exceeded (20/30s or 1/s)]`.
- **Rastreamento de entrega e restrições da sala.** Cada mensagem enviada recebe um identificador próprio do cliente e aparece como `[sending]` até a confirmação do servidor (`[sent]`). Se a sala recusar a mensagem por modo lento, modo exclusivo para seguidores, modo exclusivo para inscritos, modo apenas emotes, regra contra repetição (R9K) ou banimento/timeout, o `ttv-tui` marca a linha como `[rejected: <motivo>]`.

### Moderação

O app não oferece banimentos, timeouts ou exclusão de mensagens. A Twitch descontinuou comandos de moderação por IRC; `/ban` e similares viram mensagens públicas, por isso o app bloqueia comandos com barra, exceto `/me`.
Ações de moderação exigem a API Helix com OAuth e um app registrado na Twitch. Seu `client_id` revogável fica vinculado a uma conta, e a revogação quebraria o login de todos os usuários de uma vez.
Esse risco é desproporcional para um produto que já está em uma área cinzenta pelo vídeo sem anúncios. O chat oferece badges, cópia com contexto, evidência de mensagens apagadas e avisos de raid.

## Login e segurança

A extração de cookies descrita abaixo corresponde à implementação Linux atual. Consulte as limitações para a integração fora do Linux.

O `ttv-tui` não registra aplicação OAuth na Twitch e não embute segredo de cliente OAuth de terceiros. A autenticação pode ser feita importando um token do `twitch-tui` (`--import twt`) ou extraindo o cookie de sessão do navegador (`--from-browser`):

1. **O que `--from-browser` lê.** O `ttv-tui` localiza os bancos de cookies dos navegadores instalados: Brave, Chrome, Chromium, Edge, Vivaldi, Opera e Firefox, incluindo caminhos de Flatpak e Snap do Firefox. Ele copia o banco SQLite para um diretório temporário via `sqlite3 <db> ".backup '<tmp>'"`, com fallback para cópia de arquivo acompanhada dos sidecars `-wal` e `-shm`, consulta apenas o cookie `auth-token` de `.twitch.tv` / `twitch.tv` e apaga a cópia temporária em seguida. O arquivo original do navegador nunca é alterado.
2. **Alcance do segredo do keyring e descriptografia dos cookies.**
   - **Firefox.** Grava todos os cookies em texto puro dentro de `cookies.sqlite`.
   - **Família Chromium.** Criptografa os cookies com AES-128-CBC (`v10` / `v11`). O `ttv-tui` executa `secret-tool` (`libsecret`) para ler a senha OSCrypt Safe Storage do navegador no keyring da sessão gráfica (`application=<navegador>` no schema `chrome_libsecret_os_crypt_password_v2`, ou o par legado `service="<Browser> Safe Storage"`), deriva a chave AES de 16 bytes com PBKDF2-HMAC-SHA1 (`saltysalt`, 1 iteração) e descarta o prefixo SHA-256 de 32 bytes do `host_key` usado nas versões recentes do banco do Chromium (`meta.version >= 24`). Como o Chromium usa um único segredo Safe Storage por instalação de navegador, essa chave do keyring é capaz de decifrar **todos** os cookies salvos por aquele navegador em qualquer site, e não apenas o `auth-token` de `twitch.tv`. Embora o `ttv-tui` consulte somente a linha `auth-token` de `twitch.tv` e mantenha a chave derivada em memória apenas durante a execução de `login`, desbloquear o keyring concede ao processo acesso à chave mestra de cookies daquele navegador.
3. **Escopo de sessão completa e armazenamento em texto puro.**
   - **Sessão completa da conta.** O cookie `auth-token` de `twitch.tv` é um token de sessão web completa da sua conta na Twitch, e não um token OAuth restrito aos escopos de chat (`chat:read` / `chat:edit`). Qualquer processo ou pessoa que obtenha esse valor passa a ter acesso integral à sessão da conta até que ela seja invalidada.
   - **Texto puro em disco (`0600`).** Após validar o token em `https://id.twitch.tv/oauth2/validate` para obter nome de usuário, escopos e tempo de expiração, o `ttv-tui` grava o token em texto puro em `~/.config/ttv-tui/config.toml` (ou `$XDG_CONFIG_HOME/ttv-tui/config.toml`) com permissão `0600`, legível e gravável apenas pelo seu usuário do sistema operacional. Qualquer processo rodando sob o seu usuário consegue ler esse arquivo. O valor do token nunca é impresso em stdout, stderr ou logs de erro. No macOS, o arquivo fica em `~/Library/Application Support/ttv-tui/config.toml`; no Windows, em `%APPDATA%\ttv-tui\config.toml`, protegido pela ACL herdada do perfil, sem garantia POSIX de `0600`. XDG tem prioridade nos três sistemas.
4. **Quando preferir `--import twt`.** Se você já tem o `twitch-tui` configurado em `~/.config/twt/config.toml` ou prefere usar um token OAuth restrito ao chat sem expor o banco de cookies do navegador nem o segredo Safe Storage do keyring, execute `ttv-tui login --import twt` no lugar de `--from-browser`.
5. **Como revogar.** Apagar `config.toml` remove apenas a cópia local; o token continua válido nos servidores da Twitch até ser revogado na conta. Para invalidar a sessão nos servidores da Twitch, acesse `twitch.tv` e use **Configurações -> Segurança e privacidade -> Sair em todos os lugares** (**Settings -> Security and Privacy -> Sign Out Everywhere**), encerre a sessão do navegador de onde o cookie foi extraído ou revogue a conexão em **Configurações -> Conexões** caso tenha importado via `--import twt`. Em seguida, apague o arquivo de configuração local:

No Linux (no macOS/Windows, remova o arquivo indicado por `doctor`):

```bash
rm -f "${XDG_CONFIG_HOME:-$HOME/.config}/ttv-tui/config.toml"
```

Consulte [SECURITY.md](../SECURITY.md) para instruções de relato privado de vulnerabilidades e revogação de credenciais.

## Solução de problemas

Execute `ttv-tui doctor --lang pt-BR` (ou `--lang en`) antes de relatar uma falha. O comando mostra versão, SO/arquitetura, presença e versão numérica de `streamlink`/`mpv`, diretórios efetivos, token apenas como SIM/NÃO e se o vídeo embutido pode rodar: o terminal atual (`TERM` e o emulador, quando dá para identificar), sixel no terminal (SIM, NÃO ou NÃO TESTADO, com o motivo), sixel no `mpv` (por `--vo=help`), o veredito e o próximo passo quando falta algo, como `abra com foot ttv-tui` ou `instale um mpv com --vo=sixel`. O terminal é consultado com uma pergunta curta que nunca espera mais de um segundo; com stdin ou stdout redirecionado, ele não é consultado e aparece como NÃO TESTADO. As linhas de vídeo embutido não mudam o código de saída. Não valida o token na rede. Configuração ilegível é informada sem reproduzir o erro ou conteúdo. Dependência ausente ou diretório de estado/configuração ilegível retorna código 1; diagnóstico concluído retorna 0.

“Limpo” significa ausência de registros pendentes; “pendente” também pode ser uma instância ativa. O comando não altera registros nem testa IPC anterior, áudio ou reprodução. Ferramenta encontrada cuja versão falha é informada como versão indisponível.


- **`Canal offline: escolha outro canal` (`Channel offline: pick another channel`).** O `streamlink` informou que não há transmissão ativa para aquele canal. Escolha outro canal ao vivo ou pressione `r` para atualizar a lista.
- **`streamlink não encontrado` / `mpv não encontrado` (`streamlink not found` / `mpv not found`).** Instale `streamlink` e `mpv` pelo gerenciador de pacotes da sua distribuição e confirme que ambos estão no `PATH`.
- **`sqlite3 não encontrado no PATH` durante o login.** Instale o pacote de linha de comando `sqlite` / `sqlite3`, como `sudo pacman -S sqlite` ou `sudo apt install sqlite3`.
- **`keyring bloqueado ou ausente` durante o login.** Desbloqueie o keyring da sua sessão gráfica ou instale `libsecret` (`secret-tool`), faça login pelo Firefox usando `--browser firefox` ou importe um token com `ttv-tui login --import twt`.
- **`Token expirado ou inválido` (`Token expired or invalid`).** Os tokens de sessão do navegador expiram quando a sessão web termina. Faça login novamente em `twitch.tv` pelo navegador e execute `ttv-tui login --from-browser` ou `ttv-tui login --import twt`.
- **Pergunta sobre player órfão ao iniciar (`Um player de uma sessão anterior ainda está tocando...`).** Se um processo anterior do `ttv-tui` foi interrompido de forma abrupta com `SIGKILL`, o grupo de processos `streamlink` e `mpv` pode ter continuado em execução. Na abertura seguinte, o `ttv-tui` confere o PID e o socket registrados em `~/.local/state/ttv-tui/players/` e pergunta `[S/n]` antes de encerrar o player remanescente.
- **Chat oculto.** Na divisão lado a lado abaixo de 80 colunas, pressione `Ctrl+L` para abrir o chat em largura total ou alargue o terminal ou painel do tmux para restaurar a divisão.
- **Sem janela de vídeo ou sem áudio no `mpv`.** O `ttv-tui` inicia o `mpv` com `--no-config --no-input-default-bindings --no-osc --volume-max=100 --force-window=immediate --hwdec=auto-safe`. Verifique se a sessão gráfica Wayland ou X11 (`DISPLAY` ou `WAYLAND_DISPLAY`) e o servidor de áudio (PipeWire / PulseAudio) estão ativos, e confira se o volume na barra de status não está em `vol 0` (pressione `+` para aumentar). Pressione `?` após uma falha de reprodução para ler a linha `Último erro do player` (`Last player error`) retornada pelo `streamlink` ou pelo `mpv`.
- **Cores e `NO_COLOR`.** Defina `NO_COLOR` com qualquer valor não vazio no ambiente do terminal (por exemplo `NO_COLOR=1 ttv-tui`) para desligar a cor sem mudar o resto; `TERM=dumb` e `CLICOLOR=0` têm o mesmo efeito. Nenhuma sequência SGR de cor é emitida (negrito, reverso e sublinhado continuam), então os símbolos continuam carregando o estado: a seleção permanece em `▸`, os favoritos mantêm `★`, os canais ao vivo mantêm `●` e `[ao vivo]`, e os offline mantêm `[offline]`. `NO_COLOR` é variável de ambiente, não uma chave do `config.toml`.

## Desenvolvimento

Execute a compilação, `go vet`, a suíte de testes, o detector de condição de corrida e o `golangci-lint` `v2.15.0` conforme definido em [`.github/workflows/ci.yml`](../.github/workflows/ci.yml):

```bash
TMPDIR=/tmp go build ./...
TMPDIR=/tmp go vet ./...
TMPDIR=/tmp go test -count=1 ./...
TMPDIR=/tmp go test -race -count=1 ./internal/player ./internal/chat ./internal/ui ./internal/store
TMPDIR=/tmp go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run
```

- **Por que `TMPDIR=/tmp` é usado.** Os testes de `internal/player` criam diretórios temporários via `t.TempDir()` e abrem sockets Unix locais (`i`) do `mpv` dentro deles. O kernel Linux limita `sockaddr_un.sun_path` a 108 bytes, e o pacote `internal/player` impõe o teto de 100 bytes. Quando o ambiente define um `TMPDIR` longo, o caminho do socket dentro de `t.TempDir()` pode ultrapassar esse limite de bytes e falhar ao abrir o socket. Definir `TMPDIR=/tmp` mantém curtos os caminhos de socket durante os testes.
- **Versão fixada do `golangci-lint` (`v2.15.0`).** O CI executa `golangci-lint` na versão `v2.15.0`. Se o binário `golangci-lint` `v2.15.0` já estiver instalado no seu `PATH`, basta rodar `TMPDIR=/tmp golangci-lint run`.

### Caminhos por sistema

`XDG_CONFIG_HOME`, `XDG_STATE_HOME` e `XDG_RUNTIME_DIR` têm prioridade em todos os sistemas, cada um com o sufixo `ttv-tui`. Sem essas variáveis:

| Sistema | Configuração (token) | Estado | Runtime |
| --- | --- | --- | --- |
| Linux | `~/.config/ttv-tui/config.toml` | `~/.local/state/ttv-tui` | `<temp do SO>/ttv-tui` |
| macOS | `~/Library/Application Support/ttv-tui/config.toml` | `~/Library/Application Support/ttv-tui/state` | `$TMPDIR/ttv-tui` (senão temp do SO) |
| Windows | `%APPDATA%\ttv-tui\config.toml` | `%LOCALAPPDATA%\ttv-tui` | `%LOCALAPPDATA%\ttv-tui\run` |

O diretório de estado contém `state.json` (favoritos e até 50 recentes) e `players/player-<pid>-<gen>.json`. Cada sessão cria `<runtime>/<gen>-*/mpv-input.conf`; no Unix, cria também o socket `i` em diretório `0700`, com arquivo de teclas `0600`. No Windows, o IPC é um named pipe, sem arquivo de socket. Se o diretório não puder ser usado, o fallback é `<temp do SO>/ttv-tui-<pid>-<gen>/`. O token usa `0600` no Unix e a ACL herdada do perfil no Windows; veja [SECURITY.md](../SECURITY.md).

Se `state.json` estiver corrompido, o `ttv-tui` salva uma cópia em `state.json.bak` e inicia com listas de favoritos e recentes vazias.

As decisões de arquitetura (ADRs) estão documentadas em [`docs/adr/`](adr/).

## Limitações e próximos passos

Limitações atuais e integração da v1.8:

- **Login fora do Linux pendente neste checkout.** A descoberta de perfis ainda usa caminhos Linux e `login --token` ainda não está disponível. O escopo de autenticação da v1.8 prevê Firefox e entrada oculta de token; Chromium no Windows (App-Bound Encryption) fica de fora. A implementação e validação pertencem ao ticket de autenticação.
- **Sem comandos de moderação no chat.** Exclusão de mensagens, timeouts e banimentos não estão implementados.
- **Emotes apenas em texto.** O chat exibe texto e Unicode; emotes gráficos da Twitch, BTTV e 7TV não são renderizados como imagem no terminal.
- **Apenas transmissões ao vivo.** VODs e clipes não são suportados.
- **Favoritos e recentes locais.** Os canais favoritos ficam salvos localmente em `state.json`, sem sincronização com a lista de canais seguidos da conta na Twitch, que exigiria um Client ID OAuth registrado na API Helix.

## Licença

MIT. Consulte [LICENSE](../LICENSE).
