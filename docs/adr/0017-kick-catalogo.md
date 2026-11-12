# Catálogo da Kick pelas rotas do site

Este ADR registra de onde o `ttv-tui` tira as categorias ao vivo, os canais de cada categoria e a busca da Kick, como a lista troca de plataforma e o que acontece quando o site recusa o app. Ele completa o [ADR 0016](0016-kick-sem-conta.md), que deixou o catálogo de fora.

## Contexto

A API oficial (`api.kick.com/public`) tem as rotas de categorias e de lives, mas todas pedem token de app registrado, e o operador decidiu não registrar app na Kick. Sobram as rotas que o próprio site chama. Em 2026-11-12 o bundle JS público de `kick.com/browse` chamava as quatro abaixo, e as quatro responderam `200` a um `curl` sem cookie, sem token e com `User-Agent: ttv-tui`:

| Uso | Rota | Parâmetros | Paginação | Resposta lida |
| --- | --- | --- | --- | --- |
| Categorias ao vivo | `GET https://kick.com/api/v1/subcategories` | `limit` (até 32; acima disso vêm 32), `page` | Por número de página. A rota entrega até a página 20; da 21 em diante repete a 20 com `current_page: 20` | `current_page`, `next_page_url`, `data[].{id,name,viewers}`, em ordem de espectadores |
| Lives de uma categoria | `GET https://web.kick.com/api/v1/livestreams` | `category_id` (o `id` numérico da categoria), `sort=viewer_count_desc`, `limit` (1 a 100), `after` | Cursor opaco em `data.pagination.next_cursor`, enviado em `after`; ausente na última página | `data.livestreams[].{title,viewer_count,show_view_count,language,channel.{slug,username},category.{id,name}}` |
| Busca de canais | `GET https://search.kick.com/api/v1/channels/search` | `query`, `limit` (até 100) | Não tem | `data[].{slug,username,is_live}` |
| Busca de categorias | `GET https://search.kick.com/api/v1/categories/search` | `query`, `limit` (até 100) | Não tem | `data[].{id,name,viewer_count}` |

Os comandos que provaram cada linha:

```
curl -A ttv-tui 'https://kick.com/api/v1/subcategories?limit=32&page=1'
curl -A ttv-tui 'https://kick.com/api/v1/subcategories?limit=32&page=21'      # repete a página 20
curl -A ttv-tui 'https://web.kick.com/api/v1/livestreams?limit=24&sort=viewer_count_desc&category_id=28'
curl -A ttv-tui 'https://web.kick.com/api/v1/livestreams?limit=24&sort=viewer_count_desc&category_id=28&after=eyJvIjoyNH0'
curl -A ttv-tui 'https://search.kick.com/api/v1/channels/search?query=gaules&limit=20'
curl -A ttv-tui 'https://search.kick.com/api/v1/categories/search?query=call%20of%20duty&limit=20'
```

O que as rotas recusam, medido no mesmo dia: a de lives responde `400` sem `sort`, sem `limit`, com `limit` acima de 100 ou com `category_id` que não é número, e `500` com cursor inválido; as de busca respondem `400` sem `query` ou com `limit` acima de 100. Uma categoria que não existe devolve `200` com a lista vazia.

Rotas vistas e deixadas de lado:

- `kick.com/stream/livestreams/<idioma>?subcategory=<slug>&page=` também lista as lives de uma categoria e responde sem credencial, mas o bundle atual não a chama mais. Uma rota que o site abandonou tende a sumir antes das outras.
- `web.kick.com/api/v1/channels/search` é a busca de canais para quem está logado: responde `401` sem sessão.
- `search.kick.com/api/v1/search/enriched` devolve canais, categorias e lives numa resposta só, mas as lives são as que casam com o texto, não as dos canais achados. Ela não dá título e espectadores para os canais da busca.

Nenhuma dessas rotas tem contrato, e os três hosts ficam atrás do Cloudflare, que já bloqueou clientes que não são navegador.

## Decisão

1. **O cliente da Kick implementa `Browser` e `Searcher`.** `kick.Client` ganha `Categories`, `Channels`, `SearchCategories` e `Search` sobre as quatro rotas, e o `Provider` da Kick as entrega como a Twitch entrega as dela. A Kick tem busca de categorias, então `SearchCategories` continua dentro de `Browser`.

2. **A lista mostra uma plataforma por vez.** A UI guarda o catálogo de cada plataforma (`WithPlatformCatalog`) e a tecla `p` passa para a próxima: Twitch, Kick, Twitch. A troca recomeça a lista nas categorias da plataforma nova e não toca no vídeo nem no chat. Favoritos e recentes continuam no topo, de todas as plataformas. O cabeçalho da lista leva o nome da plataforma, e a barra de status o repete quando sobra espaço. Com um catálogo só, a tecla não faz nada e o nome não aparece.

3. **Bloqueio do site não derruba a lista.** O `403` vira `catalog.Bloqueio`, como no ADR 0016. Na lista esse erro é tratado à parte dos outros: favoritos, recentes e o que já estava carregado continuam na tela, navegáveis, e embaixo deles aparece "Catálogo da Kick indisponível agora", com `r` para tentar de novo e `p` para trocar de plataforma. Os demais erros (rede, limite de taxa, servidor, resposta inesperada) continuam substituindo as linhas, agora com o nome da plataforma que falhou.

4. **Só entram categorias com alguém assistindo.** A rota de categorias lista as mais de 18 mil do site. A listagem para na primeira com zero espectadores ou na página 20, o que vier antes. Com 32 por página, o teto é de 640 categorias.

5. **A busca de canais não traz dados da live.** A rota só diz se o canal está ao vivo. O resultado aparece como `[vivo]` ou `[offline]`, sem título e sem contagem. Ao abrir um canal que chegou sem esses dados, a UI consulta o `Resolver` da plataforma, como já fazia para a URL colada, e a barra passa a mostrar título e espectadores. Uma requisição por canal da busca daria os mesmos dados na lista, ao custo de até vinte pedidos por busca num site que bloqueia cliente que não é navegador.

6. **Contagem escondida fica escondida.** A resposta traz `show_view_count: false` nos canais que escondem a contagem no site. A lista mostra esses canais como ao vivo, sem número. O filtro de audiência mínima os esconde, como esconde qualquer canal sem contagem.

7. **Sem cache.** Cada página é pedida de novo. A contagem muda em segundos, e uma lista guardada esconderia o canal que acabou de entrar no ar.

## Consequências

- O catálogo da Kick depende de três hosts e de quatro rotas sem contrato. Se uma mudar, a parte dela falha com "Resposta inesperada da API da Kick" e as outras continuam; a Twitch não é afetada. `TTV_KICK_LIVE_CATALOG=1 go test ./internal/kick -run TestLive_Catalog -v` confere as quatro contra a Kick real e fica fora da suíte normal.
- O `403` real do Cloudflare não apareceu durante o desenvolvimento. O tratamento está coberto por testes com um servidor que responde `403`, não por um bloqueio de verdade. Resolver o desafio do Cloudflare com um navegador fica para outro trabalho.
- A ordem das categorias muda entre uma página e a seguinte, porque a contagem é ao vivo. A UI descarta as repetidas; uma categoria pode faltar até o próximo recarregamento.
- A plataforma da lista não é gravada: o app sempre abre na Twitch.
- Os termos de uso da Kick (seção 1.4) proíbem acesso automatizado ao serviço. O risco é o mesmo que o ADR 0016 já aceitou para o estado do canal e para o chat.
