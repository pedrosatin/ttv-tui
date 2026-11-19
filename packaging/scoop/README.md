# Scoop bucket

O GoReleaser gera o manifesto e atualiza o bucket `pedrosatin/scoop-bucket` a
cada tag `v*`, com deploy key (`SCOOP_BUCKET_DEPLOY_KEY`). O ensaio
`workflow_dispatch` gera o manifesto em `dist/scoop/ttv-tui.json` e não escreve
no bucket. O manifesto declara `extras/mpv` e `extras/streamlink` em `depends`.

Instalação:

```powershell
scoop bucket add pedrosatin https://github.com/pedrosatin/scoop-bucket.git
scoop install pedrosatin/ttv-tui
```

O arquivo `ttv-tui.json` neste diretório é só referência local; a fonte
publicada é o bucket.
