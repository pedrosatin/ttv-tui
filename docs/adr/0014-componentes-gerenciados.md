# Streamlink gerenciado no Windows e no Linux

Este ADR registra a introdução do suporte a componentes gerenciados no `ttv-tui`, permitindo o download e a verificação sob demanda do `streamlink` no Linux e no Windows.

## Contexto e motivações

O `ttv-tui` utiliza o `streamlink` para contornar anúncios e obter os fluxos de vídeo da Twitch para o player `mpv`. Embora usuários avançados no Linux instalem o `streamlink` via gerenciador de pacotes, no Windows e em sistemas Linux mínimos a instalação do streamlink pode ser complexa (dependência de Python, pipx ou empacotamento desatualizado).

Para oferecer uma experiência imediata de reprodução sem quebrar instalações personalizadas ou ambientes empacotados, o aplicativo precisa resolver o executável em ordem clara de precedência e, caso ausente, provisionar automaticamente o binário oficial correspondente ao sistema operacional e arquitetura.

## Decisão

1. **Ordem de resolução**:
   - Caminho explícito configurado pelo usuário em `config.toml` (`streamlink_path`);
   - Instalação gerenciada prévia sob o diretório de dados da aplicação (`$XDG_DATA_HOME/ttv-tui/components/streamlink/<versão>/` no Linux, `%LOCALAPPDATA%\ttv-tui\components\streamlink\<versão>\` no Windows);
   - Executável presente no `PATH` do sistema.

2. **Download sob demanda**:
   - Caso nenhuma das opções anteriores encontre o executável, na primeira tentativa de reprodução o aplicativo baixa a versão mais recente oficial do repositório da organização Streamlink no GitHub:
     - Linux (x86_64 e aarch64): AppImage oficial com prefixo `streamlink-` (evitando o pacote `streamlink+ffmpeg-`, maior e com ffmpeg GPL embutido);
     - Windows (x86_64): arquivo zip portátil de `streamlink/windows-builds`.
   - O progresso do download é comunicado à interface TUI por mensagens assíncronas, mantendo a camada de visualização desacoplada de operações de I/O. Enquanto aguarda o lock entre processos mantido por outro download ativo, a interface exibe estado explícito de espera ("Aguardando outro download...").

3. **Verificação de integridade e segurança**:
   - O asset é consultado pela API oficial do GitHub (`releases/latest`), obtendo o campo `digest` (`sha256:...`) informado para o arquivo.
   - O download ocorre exclusivamente por conexões HTTPS contra domínios autorizados (`api.github.com`, `github.com`, `release-assets.githubusercontent.com` e `objects.githubusercontent.com`).
   - O asset precisa de tamanho positivo informado pela API (`size`). Sem ele o download é recusado antes do primeiro byte, e com ele a leitura para no tamanho informado.
   - A tag da release vira o nome do diretório da versão, então só aceita `[A-Za-z0-9._+-]`, até 64 caracteres, sem ponto no começo ou no fim, sem `..` e sem nome reservado do Windows com ou sem extensão (`CON`, `NUL`, `COM1`, `nul.txt`).
   - O arquivo só é gravado e tornado executável se o hash SHA-256 computado bater exatamente com o digest esperado. Em caso de divergência, o arquivo é imediatamente removido e a falha é exibida com orientações para instalação manual.

4. **Instalação atômica e compatibilidade de execução**:
   - Todo download e montagem de componentes ocorre em diretórios temporários sob o diretório base (`$XDG_DATA_HOME/ttv-tui/components/streamlink/`), protegidos por lock de arquivo entre processos com expiração de ~30 s (`staleAge`) renovada periodicamente a cada ~5 s via batimento (heartbeat). Isso impede que processos abortados travem a reprodução subsequente por 10 minutos.
   - Sob o lock, se o diretório de versão de destino já existir completo com binário válido, o staging é descartado; se existir vazio ou parcial de uma execução interrompida, é removido antes da promoção atômica via renomeação (`os.Rename`, com retentativa curta no Windows para absorver travas temporárias de antivírus ou indexador). O caminho final retornado é sempre validado pela rotina de descoberta de componentes instalados (`findManagedStreamlink`).
   - No Linux, após validação do SHA-256, a AppImage é extraída uma única vez durante a instalação com `--appimage-extract` (resolvendo caminhos absolutos para compatibilidade com `XDG_DATA_HOME` relativo), executando diretamente o binário `squashfs-root/AppRun`. Isso elimina qualquer necessidade de FUSE (`/dev/fuse` ou `fusermount`), garantindo compatibilidade imediata em contêineres e sistemas restritos.
   - No Windows, o zip portátil é extraído com validação estrita de segmentos de caminho (rejeitando symlinks e travessia `..`, mas permitindo nomes legítimos com pontos duplos como `a..b`) e limite de tamanho descompactado, localizando o executável em `bin/streamlink.exe`.

5. **Inspeção**:
   - O subcomando `ttv-tui doctor` relata a versão e a origem do `streamlink` em uso (`config`, `gerenciado` ou `PATH`). Para o gerenciado, mostra também a versão anterior guardada e a data da última checagem de atualização. Só lê arquivos locais: sem rede e sem escrita.

6. **Atualização automática**:
   - Só quando o `streamlink` em uso é o gerenciado. O caminho do `config.toml` e o executável do `PATH` são do usuário e nunca são tocados.
   - No máximo uma checagem a cada 24 horas, em segundo plano, a partir da abertura do app (a reprodução e a inicialização não esperam). A data da última checagem fica em `components/streamlink/state.json`, no diretório de dados. Uma checagem que não consegue o lock de componentes (outro download ou outra atualização em curso) é adiada e continua pendente; uma que consultou a release conta como feita mesmo que o download falhe, para não repetir o download a cada abertura.
   - A versão nova passa pelo mesmo caminho da primeira instalação: `releases/latest`, digest SHA-256 publicado pela API, domínios fixos, staging e renomeação atômica. Digest errado descarta o arquivo e não troca nada. A versão nova só entra em uso depois de completa no disco e é usada a partir da próxima abertura do app, nunca no meio de uma reprodução.
   - Ficam no disco no máximo a versão atual e a anterior. As mais velhas são apagadas sob o lock, e uma versão que outra instância ainda usa não é apagada: cada processo mantém um marcador de uso (`.use-<pid>-<versão>.lock`, renovado por batimento e expirado sozinho se o processo morre) enquanto usa uma versão gerenciada.

7. **Volta para a versão anterior**:
   - Uma versão instalada por atualização fica "em teste" até a primeira reprodução que chega a tocar. O teste é gravado em `state.json` antes do rename que torna a versão atual. Uma interrupção (SIGKILL, falta de energia) depois da gravação e antes do rename deixa a anterior atual e um teste de uma versão ausente, que nenhuma reprodução confirma nem desfaz; a próxima checagem instala a versão de novo. Um erro ou um cancelamento nesse intervalo não promove nada e devolve o teste anterior. Só uma versão em teste pode ser desfeita: uma versão que já tocou e quebra semanas depois (por exemplo, a Twitch mudou o protocolo) não manda o app para uma versão ainda mais velha.
   - O que conta como falha da versão é só `player.FailureStreamlinkBroken`: erro de plugin ou do runtime Python no log (`Unable to validate`, `PluginError`, `ModuleNotFoundError`, `error while loading shared libraries` e semelhantes), binário que não executa, ou saída com erro antes de entregar o stream sem nenhuma linha que explique por canal ou rede. Canal offline, rede, tempo esgotado, URL sem plugin (`No plugin can handle URL`), ferramenta ausente e erro sem causa reconhecida não contam, e na classificação uma linha de plugin que cita rede ou canal offline fica com essa causa.
   - A classificação ignora texto derivado da URL. O streamlink repete a URL alvo e o nome do canal (`[cli][info] Found matching plugin twitch for URL ...`), e quem registra um canal `keyerror` controla esse texto:
     - as linhas `[..][info]` e `[..][debug]` não classificam, e a URL alvo e a identidade da sessão viram um marcador antes da classificação, inclusive na da última linha;
     - nomes de exceção Python só contam no formato que o Python imprime (`KeyError:`, `streamlink.exceptions.PluginError:`) ou sozinhos logo depois de um `Traceback`, e uma causa fatal verificável posterior (offline, sem plugin, ferramenta ausente) substitui uma classe anterior mais fraca;
     - um erro síncrono do início da reprodução só culpa a versão quando o sistema recusa o executável (`exec format error`), porque o texto de um erro de validação repete a URL;
     - o intervalo comercial só começa com a linha inteira `[stream.hls][info] Filtering out segments and pausing stream output`.
   - Com a falha, a versão é marcada como ruim em `state.json` (o resolvedor a ignora, a atualização não a baixa de novo e seus arquivos são apagados) e o app passa a usar a anterior na próxima reprodução. Sem versão anterior instalada, nada muda. O app avisa no terminal ao sair qual versão foi desfeita.
   - Limite conhecido: se o plugin do site que o usuário abriu está quebrado também na versão anterior, a versão nova é marcada como ruim sem ser a culpada. O custo é baixo: a próxima release corrigida é instalada normalmente.

## O que fica fora

- **macOS**: O suporte a componentes gerenciados no macOS é tratado separadamente, mantendo o comportamento existente baseado em `PATH` e Homebrew.
- **Player gerenciado (mpv / libmpv)**: O mpv permanece resolvido pelo sistema ou pela janela empacotada da plataforma.
