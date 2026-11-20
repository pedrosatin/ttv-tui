# Imagem Arch do CI

`ghcr.io/pedrosatin/ttv-tui-arch-ci` traz o Arch com os pacotes que os jobs da janela instalavam com `pacman -Syu` a cada execução: SDL3, libmpv, GCC, pkgconf, mesa, as fontes e o git. O Go fica de fora; os jobs continuam instalando a versão do `go.mod` com o `actions/setup-go`.

Quem usa a imagem: os jobs `Govulncheck (root and window)` e `Window (Arch)` do `ci.yml` e `Window binary` do `release.yml`. Eles a referenciam pelo digest (`@sha256:…`), nunca por tag, então uma imagem nova só chega ao CI num commit que troca o digest.

## Como a imagem é publicada

O fluxo [`arch-ci-image.yml`](../../workflows/arch-ci-image.yml) constrói este `Dockerfile` com Buildx (`driver: docker-container`) e publica as camadas em zstd (`compression=zstd`, `force-compression=true`, `oci-mediatypes=true`) com a tag `AAAAMMDD-<run>`. A compressão zstd encurta o passo `Initialize containers` nos jobs que puxam a imagem. Roda no dia 3 de cada mês e sob demanda (Actions → Arch CI image → Run workflow); nunca em pull request. É o único fluxo do repositório com `packages: write`.

O resumo da execução mostra o digest e as versões de `sdl3`, `mpv`, `gcc` e `pkgconf`, também presentes no log. O fluxo falha se alguma camada sair sem `mediaType` `+zstd`.

O pacote precisa ser **público**: os jobs baixam a imagem sem credencial, inclusive em PR vindo de fork. No GHCR o pacote nasce privado. Depois da primeira publicação, abra Packages → `ttv-tui-arch-ci` → Package settings → Change visibility → Public. A troca não tem volta, e a imagem só contém pacotes públicos do Arch.

## Renovar a imagem

1. Rode o fluxo (ou espere a execução mensal) e copie o digest do resumo.
2. Troque o digest nas três linhas `container:` (`ci.yml`, jobs `govulncheck` e `window`; `release.yml`, job `window`).
3. Abra a PR. O passo que lista os pacotes nativos mostra no log as versões que a PR vai usar; compare com as anteriores.

O Go cacheia a compilação pelo código Go, não pelas bibliotecas C; uma imagem nova muda as bibliotecas que o cgo usa, e os jobs da janela no Arch rodam sem cache do Go.

## Trocar a base

O `FROM` fixa `archlinux:base-<data>` pelo digest. O Dependabot propõe a base nova uma vez por mês; o `pacman -Syu` do build já atualiza os pacotes, então a base só precisa acompanhar quando o Arch mudar algo que o `-Syu` não resolve (keyring, formato do banco do pacman). Para construir localmente:

```bash
docker build -t ttv-tui-arch-ci:local .github/images/arch-ci
docker run --rm ttv-tui-arch-ci:local pacman -Q sdl3 mpv gcc pkgconf
```
