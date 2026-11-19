# Scoop bucket

O GoReleaser só gera o manifesto em `dist/scoop/ttv-tui.json` (`skip_upload:
true`). Depois da attestation e da promoção do rascunho, o workflow publica no
bucket `pedrosatin/scoop-bucket` com a deploy key `SCOOP_BUCKET_DEPLOY_KEY`. O
ensaio `workflow_dispatch` gera e confere o manifesto e não escreve no bucket.
O manifesto declara `extras/mpv` e `extras/streamlink` em `depends`.

Instalação:

```powershell
scoop bucket add pedrosatin https://github.com/pedrosatin/scoop-bucket.git
scoop install pedrosatin/ttv-tui
```

O arquivo `ttv-tui.json` neste diretório é só referência local; a fonte
publicada é o bucket.
