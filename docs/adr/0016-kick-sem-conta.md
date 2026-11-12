# Kick sem conta: JSON do site e chat pelo Pusher anônimo

Este ADR registra como o `ttv-tui` lê o estado de um canal e o chat da Kick sem conta e sem app registrado, e o que isso custa.

## Contexto

O operador decidiu não registrar app na Kick por enquanto. A API pública (`api.kick.com`) exige token em todas as rotas de leitura e não tem chat de leitura: mensagens só chegam por webhook, que pede uma URL pública. Para um cliente de terminal sobram os dois caminhos que o próprio site usa, medidos em 2026-11-12:

- `GET https://kick.com/api/v2/channels/<slug>/livestream` responde sem credencial com título, espectadores e categoria; canal fora do ar responde `{"data":null}` e canal inexistente, 404. `GET https://kick.com/api/v2/channels/<slug>` traz o `chatroom.id`.
- O chat é uma assinatura anônima no Pusher (`wss://ws-us2.pusher.com/app/<chave>`, canal `chatrooms.<id>.v2`, `auth` vazio). A chave da aplicação não é publicada pela Kick; a que o app traz veio de clientes abertos que leem o mesmo chat.

Nenhum dos dois tem contrato. O site fica atrás do Cloudflare, que já bloqueou clientes que não são navegador, e a Kick pode trocar a chave.

## Decisão

1. **`internal/kick` é o provider da Kick.** Ele implementa `Provider` como a Twitch: `Resolver` sobre a rota `livestream` (uma requisição por canal, quatro por vez), `Chat` só leitura e `StreamTarget` com a URL do canal. Não tem `Browser` nem `Searcher`: o canal chega por URL colada, pelos favoritos ou pelos recentes. O vídeo continua com o `streamlink` (regra 1).

2. **Toda resposta fora do esperado vira erro com tipo.** O 403 é `catalog.Bloqueio`, um tipo novo: a requisição foi recusada antes de chegar ao site, repetir na hora dá no mesmo e nada na resposta é dado da plataforma. O 404 embrulha `kick.ErrChannelNotFound`. No `Resolver`, canal inexistente ou ilegível fica sem evidência (não vira offline), e bloqueio, limite de taxa, erro de servidor ou de rede falham a consulta inteira.

3. **Cada plataforma resolve os canais dela.** A UI guarda um `Resolver` por plataforma e agrupa os identificadores salvos antes de perguntar. Um `xqc` da Twitch e um da Kick são canais diferentes, e a falha de uma plataforma não tira o estado das outras.

4. **O chat separa falha que passa de falha que não passa.** Rede, queda da conexão e os códigos 4100 a 4299 do Pusher reconectam com espera crescente, na mesma geração. Bloqueio na busca da sala, canal inexistente, gravação ou clipe (que não têm sala própria) e os códigos 4000 a 4099 encerram a sessão em erro. Esses casos levam um `catalog.ChatErrorReason` (`blocked`, `no_room`, `key_refused`), e a UI explica cada um no idioma do usuário em vez de mostrar o texto do erro. `Ctrl+T` tenta de novo.

5. **A chave do Pusher tem padrão e é configurável.** `kick_pusher_key` no `config.toml` substitui a embutida. Quando o Pusher recusa a chave (código 4001), o chat para e diz que a chave foi recusada; o vídeo segue. O app não tenta descobrir a chave nova sozinho.

6. **O WebSocket é do próprio pacote.** O cliente cobre o que o chat usa da RFC 6455: handshake, texto, fragmentação, ping, pong e close, com limite de 1 MiB por mensagem. O projeto não tinha dependência de WebSocket e o chat da Kick é o único uso. A conexão é direta: o cliente não passa por `HTTPS_PROXY`, e o README registra o limite. Uma conexão confirmada só zera a espera crescente depois de 30 s, para um servidor que aceita e derruba em seguida não virar reconexão a cada 500 ms.

7. **Sem estado de intervalo.** A Kick nunca produz "anúncio" na barra (regra 5): não há sinal verificável de início e fim no que o app lê hoje.

## Consequências

- Envio de mensagem, moderação visível (mensagem apagada, banimento) e catálogo da Kick ficam fora. Os eventos de moderação não apareceram na amostra real usada nos testes, então não foram implementados de memória.
- Se a Kick fechar o JSON do site ou trocar a chave, a Kick para de funcionar no app até uma atualização ou até o usuário pôr a chave nova. A Twitch não é afetada.
- `go test ./internal/kick -run TestLive_Contract` com `TTV_KICK_LIVE_SLUG=<canal ao vivo>` confere os dois contratos contra a Kick real. O teste fica fora da suíte normal.
