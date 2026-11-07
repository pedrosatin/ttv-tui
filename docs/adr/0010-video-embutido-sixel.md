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
modo com sessão GPU mostra no painel a oferta de `r`.

Com sessão sixel, esconder o painel manda `vid=no` depois de 200 ms de estabilidade:
sair do modo, abrir a ajuda ou encolher a janela. O mpv para de decodificar e de
codificar vídeo, e o áudio continua. A barra avisa enquanto a sessão estiver só no
áudio, porque essa sessão não tem janela. O painel de volta reativa com `vid=auto`: o
mpv 0.41 recusa `vid=yes`. Redimensionar com o painel oculto só grava o tamanho, para
não religar o vídeo.

Sessão GPU ignora esses comandos. `vid=no` apagaria a imagem da janela.

## Célula sem tamanho em pixels

O tamanho vem de `TIOCGWINSZ`. Sem os campos em pixels, a saída assume célula 8x16 e
abre mão de 1 linha e 1 coluna do painel, no cálculo do frame e no descarte de frame
grande demais.

A folga cobre diferença pequena. Com célula real 6x12 e painel de 18 linhas, os 480x270
cabem nas 17 linhas assumidas e ocupam 22,5 linhas reais, 4,5 além do painel. O
terminal que informa os pixels não passa por essa estimativa. O caminho para o caso
restante é consultar a célula com `CSI 16 t`.
