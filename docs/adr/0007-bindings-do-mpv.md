# O app define as teclas da janela do mpv

`--no-config` ignora a configuração do usuário, mas mantém os bindings compilados no
`mpv`. Com a janela do vídeo focada, teclas comuns faziam coisas que a TUI não mostrava:
`Ctrl+-` acumulou `A-V delay: -1800 ms`, `Shift+-` trocou a faixa de vídeo e deixou a
tela preta, `?` abriu uma lista em inglês por cima do vídeo. Por isso o `mpv` roda com
`--no-input-default-bindings` e um `input.conf` gerado por sessão, só com pausa,
volume e mudo, as mesmas teclas da TUI. O OSD do volume mostra só o número, o do
mudo mostra ♪ ✕, e `--volume-max=100` impede a janela de passar do teto que a barra da TUI exibe.

Ficam de fora o OSC (a barra de controles do `mpv`), `q`, `f` e tela cheia. O OSC roda com `--no-osc`: a barra responde a clique fora do nosso controle, mesmo com os bindings restritos, e mostra tooltips em inglês. Sobre `q`, `f` e tela cheia: encerrar é `Ctrl+X` ou fechar a janela, que o `mpv`
atende sem binding, e a geometria é do compositor (ADR 0006). Se o arquivo não puder ser
gravado, a janela fica sem teclas e a TUI avisa; voltar aos bindings embutidos traria
de volta as ações invisíveis.
