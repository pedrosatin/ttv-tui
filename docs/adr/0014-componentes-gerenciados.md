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
     - Linux (x86_64 e aarch64): AppImage manylinux de `streamlink/streamlink-appimage`;
     - Windows (x86_64): arquivo zip portátil de `streamlink/windows-builds`.
   - O progresso do download é comunicado à interface TUI por mensagens assíncronas, mantendo a camada de visualização desacoplada de operações de I/O.

3. **Verificação de integridade e segurança**:
   - O asset é consultado pela API oficial do GitHub (`releases/latest`), obtendo o campo `digest` (`sha256:...`) informado para o arquivo.
   - O download ocorre exclusivamente por conexões HTTPS contra domínios autorizados (`api.github.com`, `github.com`, `release-assets.githubusercontent.com` e `objects.githubusercontent.com`).
   - O arquivo só é gravado e tornado executável se o hash SHA-256 computado bater exatamente com o digest esperado. Em caso de divergência, o arquivo é imediatamente removido e a falha é exibida com orientações para instalação manual.

4. **Instalação atômica e compatibilidade de execução**:
   - Todo download e montagem de componentes ocorre em diretórios temporários sob o diretório base (`$XDG_DATA_HOME/ttv-tui/components/streamlink/`), protegidos por lock de arquivo entre processos e promovidos atomicamente via renomeação (`os.Rename`), sem sobrescrever nem remover instalações existentes.
   - No Linux, após validação do SHA-256, a AppImage é extraída uma única vez durante a instalação com `--appimage-extract`, executando diretamente o binário `squashfs-root/AppRun`. Isso elimina qualquer necessidade de FUSE (`/dev/fuse` ou `fusermount`), garantindo compatibilidade imediata em contêineres e sistemas restritos.
   - No Windows, o zip portátil é extraído com validação estrita de tipos de entrada (rejeitando symlinks e travessia de caminho) e limite de tamanho descompactado, localizando o executável em `bin/streamlink.exe`.

5. **Inspeção**:
   - O subcomando `ttv-tui doctor` relata a versão e a origem do `streamlink` em uso (`config`, `gerenciado` ou `PATH`).

## O que fica fora

- **Atualização automática**: Não há checagem contínua ou substituição automática de versão a cada execução; isso será abordado na issue de atualização de componentes (ticket 17).
- **macOS**: O suporte a componentes gerenciados no macOS é tratado separadamente (ticket 19), mantendo o comportamento existente baseado em `PATH` e Homebrew.
- **Player gerenciado (mpv / libmpv)**: O mpv permanece resolvido pelo sistema ou pela janela empacotada da plataforma.
