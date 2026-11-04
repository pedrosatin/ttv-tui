# Vídeo vem de streamlink + mpv, não de HLS próprio

A TUI orquestra `streamlink` (token, playlists, filtro automático de anúncio) e
`mpv` (decode e GPU), controlando o player por IPC JSON. Reimplementar HLS dentro do
app daria controle de latência e de anúncio, mas duplicaria um componente maduro e
multiplicaria o código do v1; vídeo embutido no terminal (sixel/tct) queimaria CPU e
cairia de framerate. O acoplamento fica restrito ao pacote de player, que fala apenas
com o socket IPC — trocar o mecanismo depois não toca a UI.
