#!/usr/bin/env bash
#
# install.sh - Instala o ttv-tui, o atalho .desktop e o ícone SVG a partir de uma release do GitHub.
#
# Uso:
#   packaging/install.sh [--prefix <dir>] [--dry-run] [versao|latest]
#
# Exemplos:
#   packaging/install.sh
#   packaging/install.sh 0.1.7
#   packaging/install.sh --dry-run 0.1.7
#   packaging/install.sh --prefix "$HOME/.local" latest
#

set -euo pipefail

REPO="pedrosatin/ttv-tui"
GITHUB_URL="https://github.com/${REPO}"
RAW_BASE_URL="https://raw.githubusercontent.com/${REPO}"

prefix="${HOME}/.local"
dry_run=false
requested_version=""

usage() {
  cat <<USAGE
Uso: $(basename "${0:-install.sh}") [opcoes] [versao|latest]

Instala o binário ttv-tui, o atalho de desktop e o ícone SVG a partir de uma release do GitHub.

Argumentos:
  versao               Versão da release (ex.: 0.1.7, v0.1.7 ou latest; padrão: latest)

Opções:
  --prefix <dir>       Diretório base de instalação (padrão: ~/.local)
  -n, --dry-run        Mostra o plano de instalação sem baixar arquivos nem alterar o sistema
  -h, --help           Mostra esta ajuda
USAGE
}

die() {
  printf 'Erro: %s\n' "$*" >&2
  exit 1
}

info() {
  printf '==> %s\n' "$*"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --prefix)
      [[ $# -ge 2 ]] || die "a opção --prefix requer um diretório como argumento"
      prefix="$2"
      shift 2
      ;;
    --prefix=*)
      prefix="${1#--prefix=}"
      [[ -n "$prefix" ]] || die "a opção --prefix requer um diretório não vazio"
      shift
      ;;
    -n|--dry-run)
      dry_run=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    -*)
      die "opção desconhecida: $1 (use --help para ver as opções)"
      ;;
    *)
      if [[ -n "$requested_version" ]]; then
        die "argumento extra inesperado: $1"
      fi
      requested_version="$1"
      shift
      ;;
  esac
done

if [[ -z "$requested_version" ]]; then
  requested_version="latest"
fi

os_name="$(uname -s)"
case "$os_name" in
  Linux) os="linux" ;;
  Darwin) os="darwin" ;;
  *) die "sistema operacional não suportado: ${os_name} (suportados: Linux, Darwin)" ;;
esac

uname_arch="$(uname -m)"
case "$uname_arch" in
  x86_64|amd64)
    arch="amd64"
    ;;
  aarch64|arm64)
    arch="arm64"
    ;;
  *)
    die "arquitetura não suportada: ${uname_arch} (suportadas: x86_64/amd64, aarch64/arm64)"
    ;;
esac

resolve_latest_version() {
  local effective_url=""
  local resolved=""

  if command -v curl >/dev/null 2>&1; then
    effective_url="$(curl -fsSLI --connect-timeout 5 -o /dev/null -w '%{url_effective}' "${GITHUB_URL}/releases/latest" 2>/dev/null || true)"
    if [[ "$effective_url" == *"/releases/tag/"* ]]; then
      resolved="${effective_url##*/releases/tag/}"
      resolved="${resolved#v}"
    fi

    if [[ -z "$resolved" ]]; then
      resolved="$(curl -fsSL --connect-timeout 5 "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null \
        | sed -n 's/.*"tag_name":[[:space:]]*"v\{0,1\}\([^"]*\)".*/\1/p' \
        | head -n 1 || true)"
    fi
  fi

  printf '%s' "$resolved"
}

if [[ "$requested_version" == "latest" ]]; then
  version="$(resolve_latest_version)"
  if [[ -z "$version" ]]; then
    if [[ "$dry_run" == true ]]; then
      version="latest"
    else
      die "não foi possível resolver a versão 'latest' no GitHub. Informe a versão explicitamente (ex.: 0.1.7)."
    fi
  fi
else
  version="${requested_version#v}"
fi

if [[ -z "$version" || ! "$version" =~ ^[0-9A-Za-z._-]+$ ]]; then
  die "versão inválida: '${requested_version}'"
fi

tag="v${version}"
tarball="ttv-tui_${version}_${os}_${arch}.tar.gz"
release_base_url="${GITHUB_URL}/releases/download/${tag}"
tarball_url="${release_base_url}/${tarball}"
checksums_url="${release_base_url}/checksums.txt"

bin_dir="${prefix}/bin"
app_dir="${prefix}/share/applications"
icon_dir="${prefix}/share/icons/hicolor/scalable/apps"

bin_target="${bin_dir}/ttv-tui"
desktop_target="${app_dir}/ttv-tui.desktop"
icon_target="${icon_dir}/ttv-tui.svg"

script_dir=""
if [[ -n "${BASH_SOURCE[0]:-}" && -f "${BASH_SOURCE[0]}" ]]; then
  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
fi
local_desktop="${script_dir:+${script_dir}/ttv-tui.desktop}"
local_icon="${script_dir:+$(cd "${script_dir}/.." && pwd)/assets/ttv-tui.svg}"

if command -v sha256sum >/dev/null 2>&1; then
  checksum_cmd=(sha256sum)
elif command -v shasum >/dev/null 2>&1; then
  checksum_cmd=(shasum -a 256)
else
  checksum_cmd=()
fi

warn_path() {
  case ":${PATH}:" in
    *":${bin_dir}:"*) ;;
    *) printf 'Aviso: %s não está no seu PATH atual. Adicione ao seu shell profile:\n' "$bin_dir"
       printf '  export PATH="%s:$PATH"\n' "$bin_dir" ;;
  esac
}

if [[ "$dry_run" == true ]]; then
  info "Plano de instalação (dry-run):"
  printf '  Repositório:   %s\n' "$REPO"
  printf '  Versão:        %s (tag %s)\n' "$version" "$tag"
  printf '  Arquitetura:   %s/%s (%s)\n' "$os" "$arch" "$uname_arch"
  printf '  Prefixo:       %s\n' "$prefix"
  printf '\n'
  info "Etapas planejadas:"
  printf '  1. Baixar tarball:   curl -fsSLO "%s"\n' "$tarball_url"
  printf '  2. Baixar checksums: curl -fsSLO "%s"\n' "$checksums_url"
  printf '  3. Verificar SHA-256 de %s com %s -c\n' "$tarball" "${checksum_cmd[*]:-sha256sum ou shasum -a 256 (não encontrado)}"
  printf '  4. Extrair binário e instalar em: %s (modo 0755)\n' "$bin_target"
  if [[ "$os" == "linux" ]]; then
  if [[ -n "$local_desktop" && -f "$local_desktop" ]]; then
    printf '  5. Instalar atalho .desktop (do tarball ou %s) em: %s (modo 0644)\n' "$local_desktop" "$desktop_target"
  else
    printf '  5. Instalar atalho .desktop (do tarball ou %s/main/packaging/ttv-tui.desktop) em: %s (modo 0644)\n' "$RAW_BASE_URL" "$desktop_target"
  fi
  if [[ -n "$local_icon" && -f "$local_icon" ]]; then
    printf '  6. Instalar ícone SVG (do tarball ou %s) em: %s (modo 0644)\n' "$local_icon" "$icon_target"
  else
    printf '  6. Instalar ícone SVG (do tarball ou %s/main/assets/ttv-tui.svg) em: %s (modo 0644)\n' "$RAW_BASE_URL" "$icon_target"
  fi
  if command -v update-desktop-database >/dev/null 2>&1; then
    printf '  7. Atualizar banco de atalhos: update-desktop-database "%s"\n' "$app_dir"
  else
    printf '  7. update-desktop-database não encontrado no PATH (etapa será ignorada)\n'
  fi
  fi
  warn_path
  exit 0
fi

for cmd in curl tar; do
  command -v "$cmd" >/dev/null 2>&1 || die "comando obrigatório não encontrado no PATH: ${cmd}"
done
[[ ${#checksum_cmd[@]} -gt 0 ]] || die "sha256sum ou shasum é obrigatório"

tmp_dir="$(mktemp -d)"
cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

info "Baixando ttv-tui ${tag} para ${os}/${arch}..."
curl -fsSL -o "${tmp_dir}/${tarball}" "$tarball_url"
curl -fsSL -o "${tmp_dir}/checksums.txt" "$checksums_url"

info "Verificando checksum SHA-256 de ${tarball}..."
expected_line="$(awk -v file="$tarball" '$2 == file || $2 == "*" file { print; exit }' "${tmp_dir}/checksums.txt")"
if [[ -z "$expected_line" ]]; then
  die "entrada para ${tarball} não encontrada em checksums.txt"
fi
(
  cd "$tmp_dir"
  printf '%s\n' "$expected_line" | "${checksum_cmd[@]}" -c -
)

info "Extraindo arquivo ${tarball}..."
tar -xzf "${tmp_dir}/${tarball}" -C "$tmp_dir"

[[ -f "${tmp_dir}/ttv-tui" ]] || die "binário ttv-tui não encontrado dentro do arquivo ${tarball}"

mkdir -p "$bin_dir"

info "Instalando binário em ${bin_target}..."
install -m 0755 "${tmp_dir}/ttv-tui" "$bin_target"

# A janela (SDL3 + libmpv) só existe para Linux amd64; sem ela o app fica no terminal.
if [[ "$os" == "linux" && "$arch" == "amd64" ]]; then
  window_tarball="ttv-tui-window_${version}_${os}_${arch}.tar.gz"
  window_line="$(awk -v file="$window_tarball" '$2 == file || $2 == "*" file { print; exit }' "${tmp_dir}/checksums.txt")"
  if [[ -n "$window_line" ]] && curl -fsSL -o "${tmp_dir}/${window_tarball}" "${release_base_url}/${window_tarball}"; then
    (
      cd "$tmp_dir"
      printf '%s\n' "$window_line" | "${checksum_cmd[@]}" -c -
    )
    tar -xzf "${tmp_dir}/${window_tarball}" -C "$tmp_dir" ttv-tui-window
    info "Instalando a janela em ${bin_dir}/ttv-tui-window..."
    install -m 0755 "${tmp_dir}/ttv-tui-window" "${bin_dir}/ttv-tui-window"
  else
    info "Janela indisponível nesta versão; o ttv-tui segue no terminal."
  fi
fi

resolve_asset() {
  local tarball_rel="$1"
  local local_path="$2"
  local repo_rel="$3"
  local dest_file="$4"

  if [[ -f "${tmp_dir}/${tarball_rel}" ]]; then
    cp "${tmp_dir}/${tarball_rel}" "$dest_file"
  elif [[ -f "${tmp_dir}/$(basename "$tarball_rel")" ]]; then
    cp "${tmp_dir}/$(basename "$tarball_rel")" "$dest_file"
  elif [[ -n "$local_path" && -f "$local_path" ]]; then
    cp "$local_path" "$dest_file"
  else
    if ! curl -fsSL -o "$dest_file" "${RAW_BASE_URL}/${tag}/${repo_rel}" 2>/dev/null; then
      curl -fsSL -o "$dest_file" "${RAW_BASE_URL}/main/${repo_rel}"
    fi
  fi
}

if [[ "$os" == "linux" ]]; then
mkdir -p "$app_dir" "$icon_dir"
desktop_tmp="${tmp_dir}/ttv-tui.desktop.out"
icon_tmp="${tmp_dir}/ttv-tui.svg.out"

resolve_asset "packaging/ttv-tui.desktop" "$local_desktop" "packaging/ttv-tui.desktop" "$desktop_tmp"
resolve_asset "assets/ttv-tui.svg" "$local_icon" "assets/ttv-tui.svg" "$icon_tmp"

info "Instalando atalho de desktop em ${desktop_target}..."
install -m 0644 "$desktop_tmp" "$desktop_target"

info "Instalando ícone em ${icon_target}..."
install -m 0644 "$icon_tmp" "$icon_target"

if command -v update-desktop-database >/dev/null 2>&1; then
  info "Atualizando cache de aplicativos em ${app_dir}..."
  update-desktop-database "$app_dir" || true
fi

fi
info "ttv-tui ${tag} instalado com sucesso!"
warn_path
