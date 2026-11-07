# Vídeo embutido em sixel como experimento opt-in

O ADR 0002 rejeitou vídeo no terminal pelo custo de CPU e pela queda de framerate. A
medição da v1.10 confirmou o custo: a janela GPU gastou 23% de CPU a cerca de 30 fps,
e sixel em 480x270 a 15 fps gastou 69% no mpv e 9,5% no foot. A regra mudou porque o
operador quer vídeo e TUI na mesma tela, e o tiling dependeria do window manager. A
janela do mpv continua sendo o padrão. Nada neste modo liga sozinho.

## Limites

O próprio mpv codifica os frames (`--vo=sixel`), e a TUI só os posiciona na tela, então
o vídeo segue sem reimplementação. O modo exige `--video=sixel` e detecção nos dois
lados: DA1 do terminal e `--vo=help` do mpv. Kitty, Alacritty e Terminal.app não têm
sixel. Windows e tmux ficam fora desta fase. Sixel é o único backend.

A taxa é fixa em 15 fps. Em 30 fps o custo subiu para 118% no mpv e 17% no foot, e essa
taxa nunca foi medida fora do foot. O iTerm2 e o ConPTY têm risco de backpressure.

## Ciclo de vida

A flag habilita a capacidade e o layout decide o uso. O VO é escolhido no início da
reprodução: modo `video` usa sixel, qualquer outro modo usa a janela GPU. Reiniciar
custa reconexão e possivelmente um anúncio, por isso mudar de layout nunca reinicia o
player. Com uma sessão no ar, só `r` reinicia, com o VO do layout corrente.

A lista fica oculta no modo `video`, então um canal costuma nascer fora dele. Entrar no
modo com sessão GPU mostra no painel a oferta de `r` e a tecla da lista. Reiniciar o mesmo
canal mantém o volume, e só outro canal volta ao baseline de 100. O `Manager` guarda canal
e volume da última sessão, porque um retry pode vir depois de um início que falhou e não
deixou sessão para consultar.

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
estimativa grande pinta sobre o chat. Em painel de 18 linhas o frame fica em 362x204, que
cabe em qualquer célula a partir de 6x12. Com célula real menor que isso e nenhum dos dois
meios de medir, o frame ainda passa do painel, e nada detecta esse caso.
