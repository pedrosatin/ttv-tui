# Prévia ao vivo como segunda sessão do player

Com a lista de canais em foco, o espaço abre a prévia do canal destacado: a live de verdade, muda e
na menor qualidade, sem chat e sem entrar nos recentes. A regra 4 do `AGENTS.md` ("um canal por vez")
ganha uma exceção: além da reprodução principal, pode existir uma prévia, e nunca mais de uma.

## Contexto

O operador quer saber o que está passando antes de abrir um canal. Título, categoria e espectadores
não mostram isso. Três formas foram consideradas:

- **Miniatura estática** da plataforma: aparece na hora e não passa pelo streamlink, mas a da Twitch
  é refeita a cada ~5 min e mostra um quadro velho.
- **Miniatura e depois a live**: a melhor experiência, com o dobro de estados para testar.
- **Live muda e leve**: mostra movimento real, ao custo de 2 a 5 s até o primeiro quadro e de cair,
  às vezes, num intervalo de anúncio filtrado.

Ficou a terceira. A prévia passa pelo mesmo `streamlink` e pelo mesmo `mpv` da reprodução normal
(regra 1). O que muda é o perfil: menor qualidade disponível, áudio desligado, sem `input.conf` e
sem chat.

## Decisão

- A prévia é uma **segunda sessão**, de um segundo gerenciador de player, com gerações próprias. Ela
  herda a contenção de processos da sessão principal: grupo de processos, Job Object no Windows e
  encerramento quando a TUI sai. A sessão principal continua com a mesma API e o mesmo ciclo de vida.
- **O espaço com a lista em foco é sempre prévia**, mesmo com vídeo tocando. Pausar fica com o espaço
  quando o foco está no chat. É uma troca consciente de hábito: hoje o espaço pausa em qualquer foco.
  A fatia 01 entrega a prévia só sem vídeo principal; a fatia 02 completa o gesto com vídeo ativo.
- Com vídeo principal tocando, abrir a prévia **silencia o principal** (não pausa: pausar uma live
  acumula atraso) e fechar a prévia devolve o mudo e o volume que ele tinha antes. A ponte já expõe
  `MuteMainForPreview` / `RestoreMainMute` para essa fatia.
- Espaço de novo ou `Esc` fecham a prévia. `Enter` encerra a prévia e abre o canal pelo caminho
  normal. Mover a seleção troca a prévia para o novo canal depois que a seleção fica parada por um
  instante, para que andar pela lista não abra uma live a cada tecla.
- Fora da janela única, a prévia abre numa janela própria do `mpv`, pequena por dica de tamanho do
  próprio `mpv`. A posição continua com o compositor ([ADR 0006](0006-geometria-do-compositor.md)).
  Na janela única ([ADR 0011](0011-janela-unica.md)), a prévia ocupa a coluna de vídeo quando não há
  reprodução principal e um quadro menor sobre ela quando há.
- A regra 5 vale para a prévia: o intervalo só aparece com início e fim verificáveis, pelo mesmo
  classificador da sessão principal.

## Consequências

- Duas lives podem estar abertas ao mesmo tempo por alguns segundos. A prévia nunca tem áudio, nunca
  conecta chat e é encerrada em toda saída da TUI, como a sessão principal.
- A janela única precisa de um segundo `libmpv` com área de desenho própria, e o protocolo entre o
  shim e a janela passa a dizer a qual das duas sessões cada stream pertence.
- O texto da regra 4 no `AGENTS.md` muda junto com a implementação.
