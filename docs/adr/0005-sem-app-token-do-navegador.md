# Sem app registrado na Twitch; token vem do navegador

Substitui o ADR 0004 (device code grant com app próprio). Registrar um app cria um
vínculo público entre a conta de dev do operador e um cliente cujo propósito é assistir
sem anúncio — superfície de risco alta para um v1 de um usuário. Verificamos que o
essencial dispensa credencial: o token de vídeo é negociado pelo `streamlink`, fora do
nosso binário, e o IRC do Twitch aceita leitura anônima (`NICK justinfan…`) com tags
completas, testado em 2026-11-03 nos canais `xqc` e `gaules`. Enviar mensagem é o único
caso que pede token, e ele pode ser colhido do cookie `auth-token` do próprio navegador
(mesmo mecanismo já usado para o `twitch-tui`) e validado antes de gravar. Consequência
aceita: sem refresh próprio, o token vale enquanto a sessão web durar e o usuário
repete `login --from-browser` quando expira. Registrar app só volta à mesa se o projeto
for atrás de "canais que eu sigo" via Helix.
