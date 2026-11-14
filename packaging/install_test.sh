#!/usr/bin/env bash
# Teste offline; aceita outro install.sh para provar a regressão na base.
set -euo pipefail
installer="${1:-$(dirname "$0")/install.sh}"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
export HOME="$work/home" XDG_CONFIG_HOME="$work/config" XDG_STATE_HOME="$work/state"
export XDG_DATA_HOME="$work/data" XDG_RUNTIME_DIR="$work/runtime" TMPDIR="$work/tmp"
export FIXTURES="$work/releases" CURL_LOG="$work/curl.log"
mkdir -p "$XDG_RUNTIME_DIR" "$HOME" "$TMPDIR" "$work/bin" "$work/shared" "$FIXTURES"
chmod 700 "$XDG_RUNTIME_DIR"
chmod 1777 "$work/shared"
cp "$installer" "$work/shared/install.sh"
printf 'ATALHO PLANTADO\n' > "$work/shared/ttv-tui.desktop"
mkdir -p "$work/assets"
printf 'ÍCONE PLANTADO\n' > "$work/assets/ttv-tui.svg"
cat > "$work/bin/curl" <<'CURL'
#!/usr/bin/env bash
set -euo pipefail
out="" url=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
printf '%s\n' "$url" >> "$CURL_LOG"
case "$url" in
  https://github.com/pedrosatin/ttv-tui/releases/download/*)
    rel="${url#*/download/}"
    cp "$FIXTURES/$rel" "$out" ;;
  *) printf 'URL inesperada: %s\n' "$url" >&2; exit 22 ;;
esac
CURL
chmod +x "$work/bin/curl"
export PATH="$work/bin:$PATH"

# fail mostra o log do instalador antes de sair: o trap apaga o diretório de trabalho.
fail() {
  echo "FALHOU: $1" >&2
  cat "$2" >&2
  exit 1
}
# Finge Linux x86_64 para o install.sh pedir o tarball linux_amd64 das fixtures em qualquer máquina.
cat > "$work/bin/uname" <<'UNAME'
#!/usr/bin/env bash
case "$1" in -s) echo Linux ;; -m) echo x86_64 ;; *) exit 1 ;; esac
UNAME
chmod +x "$work/bin/uname"
for version in 0.1.7 0.2.0 0.2.1 0.2.2; do
  stage="$work/stage-$version"
  release="$FIXTURES/v$version"
  mkdir -p "$stage" "$release"
  printf '#!/bin/sh\nexit 0\n' > "$stage/ttv-tui"
  if [[ "$version" != 0.1.7 ]]; then
    mkdir -p "$stage/packaging" "$stage/assets"
    printf 'ATALHO DO TARBALL\n' > "$stage/packaging/ttv-tui.desktop"
    printf 'ÍCONE DO TARBALL\n' > "$stage/assets/ttv-tui.svg"
    if [[ "$version" == 0.2.1 ]]; then
      mv "$stage/packaging/ttv-tui.desktop" "$stage/"
      mv "$stage/assets/ttv-tui.svg" "$stage/"
    elif [[ "$version" == 0.2.2 ]]; then
      rm "$stage/assets/ttv-tui.svg"
    fi
  fi
  archive="ttv-tui_${version}_linux_amd64.tar.gz"
  tar -czf "$release/$archive" -C "$stage" .
  (cd "$release" && sha256sum "$archive" > checksums.txt)
  prefix="$work/prefix-$version"
  if ! bash "$work/shared/install.sh" --prefix "$prefix" "$version" > "$work/$version.log" 2>&1; then
    echo "FALHOU: install.sh saiu com erro ($version)" >&2
    cat "$work/$version.log" >&2
    exit 1
  fi
  if [[ "$version" == 0.1.7 || "$version" == 0.2.2 ]]; then
    if [[ -e "$prefix/share/applications/ttv-tui.desktop" || -e "$prefix/share/icons/hicolor/scalable/apps/ttv-tui.svg" ]]; then
      echo "FALHOU: asset instalado com tarball incompleto ($version)" >&2
      exit 1
    fi
    grep -q 'Aviso: tarball sem atalho .desktop ou ícone SVG' "$work/$version.log" ||
      fail "sem aviso de asset ausente ($version)" "$work/$version.log"
  else
    cmp -s "$stage/$( [[ "$version" == 0.2.0 ]] && echo packaging/ )ttv-tui.desktop" "$prefix/share/applications/ttv-tui.desktop" ||
      fail "atalho instalado difere do tarball ($version)" "$work/$version.log"
    cmp -s "$stage/$( [[ "$version" == 0.2.0 ]] && echo assets/ )ttv-tui.svg" "$prefix/share/icons/hicolor/scalable/apps/ttv-tui.svg" ||
      fail "ícone instalado difere do tarball ($version)" "$work/$version.log"
  fi
  bash "$work/shared/install.sh" --dry-run --prefix "$work/dry-$version" "$version" > "$work/dry-$version.log"
  grep -q 'somente do tarball verificado' "$work/dry-$version.log" ||
    fail "dry-run sem origem do atalho ($version)" "$work/dry-$version.log"
  grep -q 'pular ambos com aviso' "$work/dry-$version.log" ||
    fail "dry-run sem o caso de asset ausente ($version)" "$work/dry-$version.log"
  [[ ! -e "$work/dry-$version" ]] || fail "dry-run criou o prefixo ($version)" "$work/dry-$version.log"
done
if grep -q 'raw.githubusercontent.com' "$CURL_LOG"; then
  echo 'FALHOU: download de asset fora do tarball' >&2
  exit 1
fi
echo 'OK: tarballs antigos, atuais, layout plano, asset ausente e dry-run.'
