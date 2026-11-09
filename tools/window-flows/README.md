# Bateria da janela única

Valida a janela única ([ADR 0011](../../docs/adr/0011-janela-unica.md)) de ponta a ponta: compila o
checkout, abre `ttv-tui-window` com a TUI real em um monitor virtual, assiste a uma live de verdade
e imprime `PASS` ou `FAIL` com a evidência de cada verificação. Roda sem token, com leitura
anônima.

```sh
python3 tools/window-flows/flows.py
```

Leva cerca de cinco minutos depois de compilar. Não entra no CI, porque depende de uma sessão do
Hyprland e de uma live no ar.

## O que exige

- Linux com uma sessão do Hyprland aberta (configuração em Lua, `hyprctl eval`) e GPU com VA-API.
- `grim`, ImageMagick (`magick`), `foot`, `streamlink` e `pgrep`.
- Go, `gcc`, `pkg-config` e as bibliotecas do SDL3 e do libmpv, para compilar `window/`.
- Rede até a Twitch e ao menos uma live na primeira categoria do catálogo.

Sem um desses itens a bateria encerra antes de testar, dizendo o que falta.

## Códigos de saída

| Código | Significado |
|---|---|
| 0 | Todas as verificações passaram |
| 1 | Falha de produto: algum `FAIL`, ou a janela caiu no meio |
| 2 | Falta pré-requisito no ambiente; nada foi testado |
| 3 | Rodada inválida: janela fora do monitor de teste, tempo esgotado ou limpeza incompleta |
| 4 | O checkout não compilou |

## Carga da máquina

As medições de fps, de latência de tecla e do tempo do catálogo caem com a máquina ocupada. Antes
de medir, a bateria espera o load de 1 minuto ficar abaixo de 6, por até 15 minutos, e registra o
valor no resumo. `--max-load` e `--load-wait` mudam os limites, e `--load-wait 0` não espera. Se só
as medições de desempenho falharem com o load alto, repita com a máquina livre.

## O que ela cria e desfaz

- Um diretório por execução em `~/.cache/scratch/ttv-tui-window-flows/` (`--out` ou
  `TTV_FLOWS_DIR` mudam o lugar), com os binários, `XDG_CONFIG_HOME`, `XDG_STATE_HOME` e
  `XDG_CACHE_HOME` isolados, as capturas, `flows.log` e `window.out`. No fim os binários e o cache
  são apagados e o resto fica como evidência. As execuções antigas não são removidas.
- O monitor virtual `TTVFLOWS` e duas regras de janela para as classes `ttvflows` e
  `ttvflows-foot`, que mandam as janelas de teste para esse monitor sem tomar o foco.
- A janela de teste e seus filhos (TUI, streamlink e o `mpv` da janela), além de um `foot` que
  vive oito segundos para forçar o tile lado a lado.

No fim, mesmo com falha ou `Ctrl+C`, a bateria encerra os processos que carregam a variável
`TTV_FLOWS_RUN` da execução, desativa as regras e remove o monitor. A linha `LIMPEZA` do relatório
diz o que sobrou. O Hyprland não apaga regra de janela em tempo de execução: as duas ficam
desativadas até a próxima recarga da configuração.

Processos, janelas do mpv e diretórios `/tmp/ttv-shell-*` que já existiam no início entram em uma
lista de base e são ignorados, então dá para rodar com o `ttv-tui` do dia a dia aberto. Abrir outro
`ttv-tui` ou `mpv` durante a execução pode gerar `FAIL` falso em "nenhuma janela do mpv" e em "sem
temporário restante".

## Limites

- Os critérios foram calibrados em uma grade de 159 colunas: 1920x1080, escala 5/3 e a fonte
  padrão de 9 pt. O monitor virtual herda a escala da configuração do Hyprland, então a bateria
  ajusta `TTV_WINDOW_PT` para a célula ter os mesmos pixels. Com outra resolução ela avisa no
  início.
- "Decode por hardware" espera `vaapi`.
- A janela abre com `--lang en`, porque as verificações leem os textos em inglês.
