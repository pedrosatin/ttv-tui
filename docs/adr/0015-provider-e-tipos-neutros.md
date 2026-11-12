# Interface Provider e tipos neutros de catálogo

Este ADR registra a introdução da interface `Provider`, do pacote neutro de catálogo e da unificação das regras de identidade de canal no `ttv-tui`.

## Contexto e motivações

O suporte a múltiplas plataformas (Twitch, Kick, YouTube e URLs avulsas) requer eliminar premissas de que todo canal e catálogo pertencem à Twitch. Antes desta mudança:
- Tipos de domínio como `Channel`, `Category` e `Source` pertenciam exclusivamente a `internal/twitch`.
- A lógica de identificação estável e equivalência de canais estava duplicada entre `internal/store` e `internal/ui/list.go`.
- O player e o módulo `playbackBridge` montavam URLs hardcoded para a Twitch (`https://www.twitch.tv/<login>`) via `twitchTarget`.

## Decisão

1. **Tipos neutros em `internal/catalog`**:
   - `Channel`, `Category`, `Availability`, `Source`, `PageRequest` e `Page[T]` passam a residir em pacote neutro, sem acoplamento a APIs ou protocolos de rede específicos.
   - Em `internal/twitch/source.go`, os tipos correspondentes passam a ser apelidos (`type Channel = catalog.Channel`, etc.), assegurando retrocompatibilidade total com código existente.

2. **Lógica unificada de identidade**:
   - `catalog.SameChannel` e `catalog.StableID` centralizam a checagem de equivalência de canais e extração de identificadores estáveis.
   - A comparação é sensível a maiúsculas e minúsculas no YouTube e insensível à caixa na Twitch e demais plataformas.
   - Cópias locais em `internal/store` e `internal/ui` foram removidas em favor dos métodos do catálogo.

3. **Interface `Provider` e Registro**:
   - Definida em `internal/provider/provider.go` com os métodos:
     - `ID() string`: identificador canônico da plataforma (`twitch`, `kick`, `youtube`, `url`).
     - `Catalog() (catalog.Source, bool)`: fonte opcional de navegação de categorias e canais.
     - `StreamTarget(ch catalog.Channel) (player.Target, error)`: montagem do alvo de reprodução (`URL`, `ExtraArgs` do streamlink e rótulo qualificado `Label`).
     - `Chat() (Chat, bool)`: interface mínima para acomodar o chat de cada plataforma (a ser expandida no ticket 09).
   - O `provider.Registry` gerencia os providers disponíveis e despacha canais para o provider apropriado (ou para o provider genérico de URL como fallback).

4. **Implementações iniciais e integração no App**:
   - `twitch.Provider` implementa o provider da Twitch, encapsulando a fonte GraphQL e a montagem de alvos para `https://www.twitch.tv/<login>`.
   - `provider.URLProvider` atua como provider genérico de URLs sobre `internal/streamurl`, sem catálogo e sem chat.
   - `internal/app` instancia a Twitch pelo Provider e a ponte de reprodução (`playbackBridge`) obtém os alvos de reprodução via `Provider.StreamTarget`, preservando exatamente os comandos e argumentos do streamlink históricos.
