# Filtro de anúncio da Kick por plugin local do streamlink

Este ADR registra como o `ttv-tui` descarta o anúncio que o Amazon IVS costura no vídeo da Kick e o que acontece quando o `streamlink` muda por dentro. Ele revê o item 7 do [ADR 0016](0016-kick-sem-conta.md).

## Contexto

O vídeo da Kick é entregue pelo Amazon IVS, que desde abril de 2026 sabe costurar anúncio no vídeo (SSAI). Hoje a Kick mostra anúncio no player do site, fora do vídeo, e nenhuma das 363 playlists amostradas no estudo de viabilidade trouxe anúncio. Se a Kick ligar o SSAI, o plugin `kick` do `streamlink` 8.x entrega o anúncio junto com a live: ele não tem `should_filter_segment`.

O guia do IVS ("SSAI Playlist Reference") documenta a marca que vem na playlist da live, antes dos segmentos do anúncio:

```
#EXT-X-DATERANGE:ID="stitched-ad-1765566299-20000000000",CLASS="live-video-net-stitched-ad",START-DATE="2025-12-12T19:04:59.079Z",DURATION=20.000,X-NET-LIVE-VIDEO-AD-AD-BREAK-ID="test"
```

O `CLASS` é sempre `live-video-net-stitched-ad`, e cada segmento traz `EXT-X-PROGRAM-DATE-TIME`.

O plugin `twitch` do `streamlink` já resolve o mesmo problema na Twitch: o parser marca como anúncio o segmento cuja data cai num `EXT-X-DATERANGE` de anúncio, e o `TwitchHLSStreamWriter.should_filter_segment` devolve essa marca. Quem descarta e registra o intervalo é o writer HLS comum a todos os plugins (`stream/hls/hls.py`), com as duas linhas que o detector do app já lê: `Filtering out segments and pausing stream output` no primeiro segmento descartado e `Resuming stream output` no primeiro conteúdo depois dele.

## Decisão

1. **Um plugin `kick.py` próprio, embutido no binário.** O arquivo fica em `internal/kick/plugin/kick.py` e entra no binário por `go:embed`. Ele executa o `kick.py` do `streamlink` instalado numa cópia privada do módulo, cria subclasses das classes HLS dela (segmento com o campo `ad`, worker que marca os segmentos, writer cujo `should_filter_segment` devolve a marca) e troca o `KickHLSStream` só nessa cópia. O plugin exportado é a própria classe `Kick` do `streamlink`, sem método copiado: busca na API, desafio do navegador, gravação e clipe continuam os do `streamlink`.

2. **O anúncio é o intervalo marcado.** Um segmento é anúncio quando o `EXT-X-PROGRAM-DATE-TIME` dele cai num `EXT-X-DATERANGE` com `CLASS="live-video-net-stitched-ad"` ou `ID` começando com `stitched-ad-`, as duas formas do guia do IVS. A marca é feita com a playlist inteira lida, então vale também um `EXT-X-DATERANGE` que aparece depois dos segmentos que cobre. O título do segmento (`live` no conteúdo, um UUID no anúncio) não entra na regra: o guia não promete o `live` em canal sem SSAI.

3. **O provider entrega, o player grava.** `catalog.Stream` ganha `Plugins`, e só o `StreamTarget` da Kick o preenche. O player grava cada plugin em `streamlink-plugins/` dentro do diretório da sessão, que já é privado e de nome imprevisível (0700, também no fallback em `/tmp`), cria cada arquivo do zero (0600, `O_EXCL`) e passa o diretório com `--plugin-dir`. O diretório some com a sessão. Twitch e URL genérica não recebem nada. O `--plugin-dir` existe desde o `streamlink` 6.7.0, anterior ao plugin `kick` (6.8.0); o `--plugin-dirs` está obsoleto e imprime aviso.

4. **Plugin que não serve cede ao original.** O módulo confere antes de montar qualquer subclasse se as classes e os métodos que estende existem. Se falta algo, ou se o `kick.py` do `streamlink` não carrega, o módulo não exporta `__plugin__`: o `streamlink` ignora o arquivo e toca pelo plugin `kick` dele, sem filtro. O motivo sai numa linha `[plugins.kick][warning] ttv-tui plugin fallback: ...`, que o player manda para o log mostrado ao sair do app e não classifica como falha: ela cita o nome de uma exceção Python, que a classificação do [ADR 0014](0014-componentes-gerenciados.md) tomaria como `streamlink` quebrado. Nenhuma exceção sai do import do plugin, porque o `streamlink` só trata `ImportError` ao carregar plugin e qualquer outra derrubaria a CLI. Se o player não consegue gravar o plugin, a sessão toca sem `--plugin-dir` e anota o motivo no mesmo log.

5. **O detector de intervalo não muda.** As duas linhas vêm do writer HLS do `streamlink`, como na Twitch, e a regra 5 continua valendo: o intervalo da Kick só existe com início e fim que o `streamlink` registrou.

## Como foi provado

- `go test ./internal/kick -run TestAdFilterPluginInStreamlink` roda o plugin dentro do `streamlink` instalado (8.6.2 no desenvolvimento) contra uma live sintética servida em `127.0.0.1`, com as três marcas que o IVS põe antes do anúncio. Com o plugin, saem só os quatro segmentos de conteúdo, e o stderr no formato da CLI traz as duas linhas do intervalo. Sem ele, os dois segmentos de anúncio passam e nenhuma das linhas aparece.
- O mesmo teste prova o fallback em dois casos: um `kick.py` do `streamlink` em que `KickHLSStreamWriter` foi renomeado e um `streamlink` sem `kick.py`. Nos dois, o plugin não substitui o `kick`, a URL da Kick casa com o plugin original do `streamlink`, a linha de fallback aparece e nenhum traceback sai.
- `TestStreamlinkCLILoadsAdFilterPlugin` confere que a CLI carrega o arquivo pelo `--plugin-dir` (`Plugin kick is being overridden by ...`). `TestAdFilterSourceParsesOnPython310` confere que a sintaxe vale no Python 3.10.
- Os testes pulam, com o motivo, quando não há Python com o `streamlink` importável (o AppImage gerenciado não expõe o Python dele) ou quando o `streamlink` é anterior ao 8.0.

## Consequências

- Nenhum anúncio costurado real da Kick foi visto. O plugin segue o guia do IVS e foi testado com uma playlist montada a partir dele.
- O plugin depende de nomes internos do plugin `kick` do `streamlink` (`KickHLSStream`, `KickHLSStreamReader`, `KickHLSStreamWorker`, `KickHLSStreamWriter`, `KickM3U8Parser`, `Kick._get_streams_live`). Uma release que os mude desliga o filtro até uma atualização do app; o vídeo continua tocando, com anúncio.
- Um `SyntaxError` no arquivo derrubaria a CLI antes de qualquer fallback. Por isso a sintaxe fica no Python 3.10, e o teste confere.
- Um `kick.py` que o usuário tenha posto no diretório de plugins do `streamlink` deixa de valer nas reproduções do app: o `--plugin-dir` é carregado depois e o substitui.
- Gravação e clipe também passam pelo plugin, sem efeito: só a live usa o `KickHLSStream`.
