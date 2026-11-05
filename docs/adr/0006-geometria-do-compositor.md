# Geometria da janela do mpv fica com o compositor

Posicionar e redimensionar a janela do `mpv` é trabalho do sistema operacional /
compositor, não do app. O acoplamento via `hyprctl` (`window_control=hyprland`)
trazia pouco ganho e bugs de escala; foi removido na v1.6. O app continua
controlando só o layout dos painéis da TUI (`Ctrl+W` / `Ctrl+H` / `Ctrl+L`).
Voltar a controlar área de vídeo só faria sentido se o vídeo passasse a ser
embutido na TUI — o que o ADR 0002 rejeita para o desenho atual.
