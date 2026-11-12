# Interface Provider e tipos neutros de catálogo

Este ADR registra a introdução da interface `Provider`, do pacote neutro de catálogo com capacidades granulares, do isolamento de dependências em relação ao player e da unificação das regras de identidade de canal no `ttv-tui`.

## Contexto e motivações

O suporte a múltiplas plataformas (Twitch, Kick, YouTube e URLs avulsas) requer eliminar premissas de que todo canal e catálogo pertencem à Twitch. Antes desta mudança:
- Tipos de domínio como `Channel`, `Category` e `Source` pertenciam exclusivamente a `internal/twitch`.
- A lógica de identificação estável e equivalência de canais estava duplicada entre `internal/store` e `internal/ui/list.go`.
- O player e o módulo `playbackBridge` montavam URLs hardcoded para a Twitch (`https://www.twitch.tv/<login>`) via `twitchTarget`.
- O acoplamento entre os tipos de catálogo e o player forçava a UI e o store a carregar `internal/player` e `internal/config` em sua árvore de dependências transitivas.

## Decisão

1. **Tipos neutros e capacidades granulares em `internal/catalog`**:
   - `Channel`, `Category`, `Availability`, `Stream`, `PageRequest` e `Page[T]` residem em pacote neutro, sem acoplamento a APIs ou bibliotecas de reprodução/rede.
   - O catálogo é decomposto em capacidades opcionais:
     - `Browser`: navegação de categorias, listagem de canais por categoria e busca de categorias (`SearchCategories`).
     - `Searcher`: busca de canais ao vivo (`Search`).
     - `Resolver`: resolução de status de transmissão para canais salvos (`ChannelsByIDs`). Renomeado de `ChannelsByLogins` para identificador neutro na migração da lista, já que em plataformas como YouTube o identificador não é um login. Na Twitch, `ChannelsByIDs` delega para a resolução existente e `ChannelsByLogins` é mantido para compatibilidade.
     - `catalog.Source`: preservado como a composição das três capacidades (`Browser`, `Searcher`, `Resolver`) para compatibilidade com a Twitch e a UI existente.
   - Em `internal/twitch/source.go`, os tipos correspondentes são expostos como apelidos de tipo (`type Channel = catalog.Channel`, etc.). Esses apelidos são provisórios e sustentam a compatibilidade até a migração da UI na migração da lista e do chat na migração do chat.

2. **Lógica unificada de identidade**:
   - `catalog.SameChannel` e `catalog.StableID` centralizam a checagem de equivalência de canais e extração de identificadores estáveis.
   - `catalog.StableID` remove o prefixo canônico `plataforma:` tanto de `Login` quanto de `ID`, normalizando identificadores qualificados de plataformas externas (como `kick:xqc`). Antes, o store e a lista removiam o prefixo só do `ID` e usavam o `Login` cru; agora um canal da Kick salvo com `Login` `xqc` e outro com `kick:xqc` são o mesmo canal. Os dados gravados pelo app não têm `Login` prefixado, então nenhum favorito ou recente muda de identidade.
   - A comparação é sensível a maiúsculas e minúsculas no YouTube e insensível à caixa na Twitch e demais plataformas.
   - Cópias locais em `internal/store` e `internal/ui` foram removidas em favor dos métodos do catálogo.

3. **Interface `Provider` e Registro (`internal/provider`)**:
   - Definida em `internal/provider/provider.go` com os métodos:
     - `ID() string`: identificador canônico da plataforma (`twitch`, `kick`, `youtube`, `url`).
     - `Browser() (catalog.Browser, bool)`: capacidade opcional de navegação de categorias e canais.
     - `Searcher() (catalog.Searcher, bool)`: capacidade opcional de busca de canais.
     - `Resolver() (catalog.Resolver, bool)`: capacidade opcional de resolução de status de canais salvos.
     - `StreamTarget(ch catalog.Channel) (catalog.Stream, error)`: montagem do alvo neutro de reprodução (`URL`, `ExtraArgs` do streamlink e rótulo qualificado `Label`).
     - `Chat() (catalog.Chat, bool)`: interface mínima para acomodar o chat de cada plataforma (a ser expandida na migração do chat).
   - **Isolamento de dependências**: `internal/provider` e `internal/twitch` dependem apenas de `catalog`, `platform` e `streamurl` (além da biblioteca padrão), sem importar `internal/player`. `internal/twitch` implementa `Provider` implicitamente sem importar `internal/provider`.
   - `provider.Registry` gerencia providers registrados com `Register`, `Get(id)` e `IDs()` (em ordem alfabética estável e determinística).
   - Em `Registry.ProviderFor(ch)`, caso a plataforma de um canal não esteja registrada, um erro explícito é retornado em vez de cair silenciosamente no provider genérico de URL, exceto quando o canal possui uma URL de stream que o provider de URL sabe manipular.

4. **Implementações e integração no App (`internal/app`)**:
   - `twitch.Provider` implementa o provider da Twitch, encapsulando a fonte GraphQL e a montagem de alvos para `https://www.twitch.tv/<login>`. Retorna erro se o canal for vazio.
   - `provider.URLProvider` atua como provider genérico de URLs sobre `internal/streamurl`, sem capacidades de catálogo e sem chat.
   - No `internal/app/app.go`, a montagem é simplificada: `Options.Source` permanece como a única costura para testes injetarem a fonte falsa da Twitch. Os providers são registrados pelo próprio `ID()` no `Registry`, removendo intermediários desnecessários.
   - A variável local do catálogo de i18n em `app.go` foi renomeada para `appCat` para evitar colisão com o pacote `catalog`.
   - `playbackBridge` em `internal/app/playback.go` serve como a ponte exclusiva entre o domínio de provedores e o subsistema de reprodução, convertendo `catalog.Stream` para `player.Target`.
