# Vídeo embutido em sixel como experimento opt-in

O ADR 0002 rejeitou vídeo no terminal pelo custo de CPU e pela queda de framerate. A
medição da v1.10 confirmou o custo: a janela GPU gastou 23% de CPU a cerca de 30 fps,
e sixel em 480x270 a 15 fps gastou 69% no mpv e 9,5% no foot. A regra mudou porque o
operador quer vídeo e TUI na mesma tela, e o tiling dependeria do window manager. A
janela do mpv continua sendo o padrão quando o modo não está habilitado.

## Limites

O próprio mpv codifica os frames (`--vo=sixel`), e a TUI só os posiciona na tela, então
o vídeo segue sem reimplementação. O modo exige `video = "sixel"` no config ou
`--video=sixel`, além de detecção nos dois
lados: DA1 do terminal e `--vo=help` do mpv. Kitty, Alacritty e Terminal.app não têm
sixel. Windows e tmux ficam fora desta fase. Sixel é o único backend.

A taxa é fixa em 15 fps. Em 30 fps o custo subiu para 118% no mpv e 17% no foot, e essa
taxa nunca foi medida fora do foot. O iTerm2 e o ConPTY têm risco de backpressure.

## Ciclo de vida

Com `video = "sixel"` no config ou `--video=sixel`, e suporte confirmado no
terminal e no mpv, o app abre no layout `video`, com a lista sobre o painel. A
barra mostra `Vídeo embutido ativo: escolha um canal`. O primeiro canal abre
embutido, sem janela do mpv. Quem habilita o modo espera ver o vídeo no terminal.
Sem a chave, sem suporte ou sem TTY, o app abre em split e informa o motivo.

O VO é escolhido no início da reprodução: modo `video` usa sixel, qualquer outro
modo usa a janela GPU. Mudar de layout não reinicia o player, pois isso custa
reconexão e possivelmente um anúncio. Ao entrar no modo com uma sessão GPU, o
painel oferece `r` para reiniciá-la embutida. Reiniciar o mesmo canal mantém o
volume, e só outro canal volta ao baseline de 100. O `Manager` guarda canal e
volume da última sessão, pois um retry pode vir depois de um início que falhou e
não deixou sessão para consultar.

`Esc` ou `l` abre a lista sem sair do modo, no desenho normal de lista e chat. O painel
fica coberto: nenhum frame sai, então nenhuma linha é disputada com a lista. Coberto conta
como oculto para o `vid=no` abaixo. A diferença para a ajuda é que a saída guarda o último
frame e o reemite ao fechar, o que cobre a pausa, quando nenhum frame novo viria, e a espera
pelo `vid=auto`. Escolher um canal ou `Esc` fecha. Aqui `Esc` não sobe um nível na lista:
quem quer as categorias usa `Ctrl+R`.

Em pausa o mpv ainda desenha um frame quando o vídeo é reconstruído, cerca de 0,6 a 1,9 s
depois do `vid=auto`. A saída aceita esse frame: depois de encolher a janela ele é a única
imagem que o painel vai ter.

Com sessão sixel, esconder o painel manda `vid=no` depois de 200 ms de estabilidade:
sair do modo, abrir a ajuda ou encolher a janela. O mpv para de decodificar e de
codificar vídeo, e o áudio continua. A barra avisa enquanto a sessão estiver só no
áudio, porque essa sessão não tem janela. O painel de volta reativa com `vid=auto`: o
mpv 0.41 recusa `vid=yes`. Redimensionar com o painel oculto só grava o tamanho, para
não religar o vídeo.

Sessão GPU ignora esses comandos. `vid=no` apagaria a imagem da janela.

## Tamanho da célula

O tamanho vem de `TIOCGWINSZ`, que acompanha mudança de fonte. Sem os campos em pixels, a
TUI pergunta com `CSI 16 t` antes de o Bubble Tea assumir a entrada, e manda DA1 junto: o
terminal responde em ordem, então a resposta do DA1 encerra a espera, e só um terminal mudo
gasta os 350 ms do timeout. Depois disso ninguém lê essa resposta, por isso ela vale para a
sessão inteira: mudar a fonte nesse terminal passa despercebido.

Sem resposta, a saída assume célula 6x12 e abre mão de 1 linha e 1 coluna, no cálculo do
frame e no descarte de frame grande demais. Estimativa pequena só encolhe o vídeo, e
estimativa grande pinta sobre o chat. Como exemplo, em um painel de 18 linhas o frame
fica em 363x204, que cabe em qualquer célula a partir de 6x12. Com célula real menor que
isso e nenhum dos dois meios de medir, o frame ainda passa do painel, e nada detecta
esse caso.

## Composição da área de vídeo na v1.11

O painel deixa de ter o teto de 18 linhas. A área do modo `video`, descontada a barra
de status, é composta pelo vídeo e por uma faixa opaca de chat em largura total.
`video_chat_rows` define a altura da faixa, com padrão de 8 linhas e valores de 3 a 30,
sempre limitada ao espaço disponível. `video_chat_position` aceita `"bottom"`, o
padrão, ou `"top"`. Valores ausentes ou inválidos usam os padrões.

A tecla `c` oculta ou mostra a faixa enquanto o vídeo está na tela e nenhum campo de
texto está aberto. Ocultar o chat entrega suas linhas ao vídeo. A composição mantém
as linhas de chat separadas da imagem e aproveita a altura do terminal, que o painel
fixo de 18 linhas deixava ociosa. A mudança de área recalcula o tamanho do frame sem
reiniciar o player. O frame mantém a proporção 16:9 e pode deixar margens.

## Tamanho do frame

O frame é o maior 16:9 que cabe no painel, sem teto próprio. O tamanho depende da área
que a composição reserva ao vídeo e do tamanho da célula. Um painel de 18 linhas com
célula 10x20, por exemplo, comporta 640x360. Sobrando espaço, o frame é centralizado na
célula mais próxima, pelo tamanho que o próprio frame declara. Assim um stream que
não é 16:9, ou um frame antigo durante um resize, também fica no centro.

O teto antigo de 480x270 deixava a imagem num canto do painel. Tirar o teto custa CPU: numa
live real, o mpv foi de 105% em 480x270 para 125% em 640x360, e a saída de 2,0 para 3,2 MB/s.
Em fonte sintética, 15 fps se sustentam até 960x540 e caem para cerca de 12 fps em 1280x720.
O pior frame medido, ruído puro em 1280x720, tem 3,3 MiB, dentro do limite de 8 MiB do demux.

Sem terminal, o mpv conta 25 linhas e reserva a última: pedir 360 de altura dava 342. Por
isso a altura vai como linhas de uma banda sixel, mais uma banda para a linha reservada, em
`--vo-sixel-height` e `--vo-sixel-rows`.

## Log do streamlink

No modo sixel o stdout do player é o stdout do streamlink (`-v`), então o log precisa sair
dali. O streamlink resolve o caminho de `--logfile` antes de abrir, e `/dev/stderr` sobre um
pipe vira `/proc/PID/fd/pipe:[N]`, que não abre: o streamlink morre no setup de log. Por
isso o log vai para `streamlink.log` no diretório da sessão. O arquivo é criado pela
sessão antes do processo, lido por polling a cada 100 ms e removido junto com o diretório.
Um FIFO evitaria o polling, mas prenderia o streamlink se o leitor sumisse e não existe no
Windows.

## Espera pelo mpv

A espera de 30 s cobre tudo que o streamlink faz antes de o mpv existir. Numa VPN com RTT de
380 ms isso levou 15,6 s, e em canais reais já mediu 19,3 s, por isso os 15 s antigos davam
tempo esgotado. O custo é que um início travado sem nenhuma linha demora 30 s para aparecer
como erro. Os outros casos não esperam: processo morto encerra na hora, linha fatal falha na
hora e endpoint presente que recusa conexão falha em cerca de 2 s.
