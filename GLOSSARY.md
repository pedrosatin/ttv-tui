# ttv-tui

Twitch no terminal: navegar, assistir e conversar sem navegador.

## Language

**Categoria**:
Agrupamento de transmissões por jogo ou assunto (o "game" da API da Twitch).
_Avoid_: jogo, game, seção

**Canal**:
Uma transmissão ao vivo identificada pelo login do streamer (ex.: `gaules`).
_Avoid_: stream, live, transmissão

**Source**:
Interface que entrega categorias, canais e resultados de busca para a UI.
Implementada hoje pelo GraphQL do site; o adapter Helix entra atrás da mesma interface.
_Avoid_: client, api, provider

**Player**:
O processo externo que decodifica e exibe o vídeo: `streamlink` puxando o HLS e
`mpv` renderizando, controlados por IPC.
_Avoid_: mpv (quando se fala do conjunto), viewer, engine

**Sessão de chat**:
Conexão IRC autenticada em um canal, com leitura e envio de mensagens.
_Avoid_: sala, room, conexão

**Recente**:
Canal aberto nas últimas execuções, guardado localmente em ordem de uso.
_Avoid_: histórico

**Favorito**:
Canal marcado pelo usuário para ficar no topo da lista, guardado localmente.
_Avoid_: bookmark, star

**Token**:
Credencial OAuth da conta do usuário, gravada em `~/.config/ttv-tui/config.toml`.
_Avoid_: senha, credencial, oauth
