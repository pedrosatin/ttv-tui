# Portabilidade do player e dos caminhos

A v1.8 tem Linux, macOS arm64 e Windows x64 como alvos suportados. Darwin Intel
(`darwin/amd64`) e Windows ARM (`windows/arm64`) compilam como experimentais.
Compilação cruzada verifica os contratos de compilação; reprodução, áudio,
terminal, clipboard e login exigem validação em cada sistema real.

O protocolo JSON do mpv permanece comum. O transporte segue o SO: socket Unix no
Linux/macOS e named pipe no Windows, acessado por go-winio. O nome do pipe é
separado do diretório de sessão para não passar barras invertidas por
`--player-args`, interpretado pelo shlex do streamlink. O diretório continua
armazenando o arquivo de teclas; não se infere sua localização pelo endpoint.

No Unix, grupos de processos permitem encerrar streamlink e seus filhos. No
Windows, o processo fica em um Job Object com `KILL_ON_JOB_CLOSE`: fechar o Job
encerra a árvore mesmo quando a TUI morre. Isso evita depender de `taskkill` ou
supor que matar só o pai encerra o mpv. O registro no disco serve para recuperação
ou limpeza; sua presença não prova que um player está vivo. O macOS consulta
identidade de processos sem depender do `/proc` do Linux.

Os resolvedores dão prioridade a XDG em todos os sistemas para permitir ambientes
isolados. Os padrões seguem as convenções locais: `.config` e `.local/state` no
Linux, `Library/Application Support` no macOS, APPDATA para configuração e
LOCALAPPDATA para estado/runtime no Windows. Runtime usa XDG_RUNTIME_DIR quando
presente, TMPDIR no macOS, LOCALAPPDATA no Windows e o temporário do SO como
fallback. Sessões usam diretórios privados; sockets Unix respeitam o limite de
comprimento do caminho. O token fica em texto puro, com `0600` no Unix e proteção
pela ACL herdada do perfil no Windows, sem equivalência de permissões POSIX.

`ttv-tui doctor` inspeciona ferramentas, caminhos, presença do token e registros
pendentes sem iniciar a interface, acessar a Twitch ou alterar processos/arquivos.
Registros removidos no encerramento não oferecem histórico do IPC: “limpo” só
significa que nenhum registro está pendente.

Ficam de fora notarização paga da Apple, testes ConPTY e suporte a Darwin Intel.
Downloads pelo navegador podem exigir liberação no Gatekeeper; instruções e
checksums devem acompanhar a distribuição. O login da v1.8 fora do Linux prevê
Firefox e entrada oculta de token, sem contornar App-Bound Encryption do Chrome
no Windows; a implementação de autenticação pertence ao seu próprio ticket.
Homebrew e Scoop só ficam disponíveis ao usuário após atualização dos artefatos,
hashes e publicação dos respectivos canais.
