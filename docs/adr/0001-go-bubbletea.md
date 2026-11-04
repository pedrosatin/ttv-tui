# Go com bubbletea como stack da TUI

O portfólio do operador usa Rust (flea) e Python, então Go é a escolha inesperada.
Go foi escolhido porque entrega binário único estático, RAM baixa (critério número um
do operador), compilação rápida e um ecossistema de TUI maduro e pequeno
(bubbletea/bubbles/lipgloss), sem o custo de build e a verbosidade de Rust para uma
v1 que precisa sair rápido. Alternativas descartadas: Rust/ratatui (menor RAM, mais
tempo de código) e Python/Textual (mais rápido de escrever, ~50-70 MB de RAM).
