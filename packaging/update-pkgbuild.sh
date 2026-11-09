#!/usr/bin/env bash
#
# update-pkgbuild.sh - Atualiza deterministicamente pkgver e sha256sums no PKGBUILD
#
# Uso:
#   packaging/update-pkgbuild.sh <versao> <caminho/para/checksums.txt> [caminho/para/PKGBUILD]
#
# Argumentos:
#   <versao>: Versão da release (com ou sem prefixo 'v', ex.: 0.1.0 ou v0.1.0)
#   <caminho/para/checksums.txt>: Caminho para o arquivo checksums.txt gerado pelo GoReleaser
#   [caminho/para/PKGBUILD]: (Opcional) Caminho do PKGBUILD. Padrão: PKGBUILD no mesmo diretório do script
#
# Exemplo:
#   ./packaging/update-pkgbuild.sh 0.1.0 dist/checksums.txt
#   ./packaging/update-pkgbuild.sh v0.1.0 dist/checksums.txt packaging/PKGBUILD
#

set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "Uso: $0 <versao> <caminho/para/checksums.txt> [caminho/para/PKGBUILD]" >&2
  exit 1
fi

raw_version="$1"
version="${raw_version#v}"
checksums_file="$2"
script_dir="$(dirname "$0")"
pkgbuild_file="${3:-"${script_dir}/PKGBUILD"}"

if [[ -z "$version" ]]; then
  echo "Erro: versão inválida ou vazia: '$raw_version'" >&2
  exit 1
fi

if [[ ! -f "$checksums_file" ]]; then
  echo "Erro: arquivo checksums.txt não encontrado: '$checksums_file'" >&2
  exit 1
fi

if [[ ! -f "$pkgbuild_file" ]]; then
  echo "Erro: arquivo PKGBUILD não encontrado: '$pkgbuild_file'" >&2
  exit 1
fi

# Extrai o hash SHA-256 pelo nome exato: o checksums.txt também lista ttv-tui-window_*.
sha_amd64=$(awk -v name="ttv-tui_${version}_linux_amd64.tar.gz" '$2 == name {print $1; exit}' "$checksums_file")
sha_arm64=$(awk -v name="ttv-tui_${version}_linux_arm64.tar.gz" '$2 == name {print $1; exit}' "$checksums_file")

if [[ -z "$sha_amd64" || ! "$sha_amd64" =~ ^[a-fA-F0-9]{64}$ ]]; then
  echo "Erro: não foi possível encontrar checksum SHA-256 válido para linux_amd64 em '$checksums_file'" >&2
  exit 1
fi

if [[ -z "$sha_arm64" || ! "$sha_arm64" =~ ^[a-fA-F0-9]{64}$ ]]; then
  echo "Erro: não foi possível encontrar checksum SHA-256 válido para linux_arm64 em '$checksums_file'" >&2
  exit 1
fi

# Reescreve pkgver, pkgrel e os sha256sums de forma determinística
awk -v ver="$version" -v sha_x86="$sha_amd64" -v sha_arm="$sha_arm64" '
  /^pkgver=/ { print "pkgver=" ver; next }
  /^pkgrel=/ { print "pkgrel=1"; next }
  /^sha256sums_x86_64=/ { print "sha256sums_x86_64=(\x27" sha_x86 "\x27)"; next }
  /^sha256sums_aarch64=/ { print "sha256sums_aarch64=(\x27" sha_arm "\x27)"; next }
  { print }
' "$pkgbuild_file" > "${pkgbuild_file}.tmp"

mv "${pkgbuild_file}.tmp" "$pkgbuild_file"

echo "PKGBUILD atualizado com sucesso para versão $version"
