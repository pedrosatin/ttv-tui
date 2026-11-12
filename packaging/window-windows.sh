#!/usr/bin/env bash
# Builds ttv-tui-window.exe in an MSYS2 UCRT64 shell and packages it in a
# ttv-tui-window folder with the official LGPL libmpv-2.dll, SDL3 from MSYS2,
# and THIRD-PARTY.txt.
#
# Usage: packaging/window-windows.sh <output.zip>
# Needs: mingw-w64-ucrt-x86_64-{gcc,pkgconf,sdl3,ntldd}, curl, unzip, zip, and go on PATH.
set -euo pipefail

[[ $# -eq 1 ]] || { echo "usage: $0 <output.zip>" >&2; exit 2; }
out="$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
root="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
stage="$work/ttv-tui-window"
mkdir -p "$stage"

MPV_VERSION="v0.42.0"
MPV_ASSET="libmpv-v0.42.0-x86_64-w64-mingw32-lgpl.zip"
MPV_SHA256="8e5f77430efc53ef175804ba6618527e3af0448a0dcabf04d3cc72659d1a6fce"
MPV_URL="https://github.com/mpv-player/mpv/releases/download/${MPV_VERSION}/${MPV_ASSET}"

cache_dir="${MPV_CACHE_DIR:-${XDG_CACHE_HOME:-$HOME/.cache}/ttv-tui}"
mkdir -p "$cache_dir"
cached_zip="$cache_dir/$MPV_ASSET"

if [[ ! -f "$cached_zip" ]] || ! (cd "$cache_dir" && echo "$MPV_SHA256 *$MPV_ASSET" | sha256sum -c - >/dev/null 2>&1); then
  echo "Downloading $MPV_URL..."
  curl -sSL -f -o "$cached_zip.tmp" "$MPV_URL"
  (cd "$cache_dir" && echo "$MPV_SHA256 *$MPV_ASSET.tmp" | sha256sum -c -)
  mv "$cached_zip.tmp" "$cached_zip"
fi

echo "Verifying $MPV_ASSET..."
(cd "$cache_dir" && echo "$MPV_SHA256 *$MPV_ASSET" | sha256sum -c -)

mpv_work="$work/mpv"
mkdir -p "$mpv_work"
unzip -q "$cached_zip" -d "$mpv_work"

# Set up pkg-config for the official libmpv headers and import library.
mpv_prefix="$(cygpath -m "$mpv_work" 2>/dev/null || echo "$mpv_work")"
mkdir -p "$mpv_work/pkgconfig"
cat <<EOF > "$mpv_work/pkgconfig/mpv.pc"
prefix=$mpv_prefix
exec_prefix=\${prefix}
libdir=\${prefix}
includedir=\${prefix}/include

Name: mpv
Description: mpv media player client library
Version: 0.42.0
Libs: -L\${libdir} -lmpv
Cflags: -I\${includedir}
EOF

# Build ttv-tui-window.exe against the official libmpv headers and import library.
(cd "$root/window" && PKG_CONFIG_PATH="$mpv_work/pkgconfig:${PKG_CONFIG_PATH:-}" CGO_ENABLED=1 go build -buildvcs=false -trimpath -ldflags "-s -w" -o "$stage/ttv-tui-window.exe" .)

# Put the official LGPL libmpv-2.dll and LICENSE.LGPL in place.
cp "$mpv_work/libmpv-2.dll" "$stage/"
cp "$mpv_work/LICENSE.LGPL" "$stage/"

# ntldd -R walks the import tables; system DLLs live outside /ucrt64/bin.
# With libmpv-2.dll already in $stage, ntldd resolves it locally and sees only
# system DLL imports. Only SDL3 and its runtime support come from /ucrt64/bin.
(cd "$stage" && ntldd -R ttv-tui-window.exe) | tr -d '\r' | awk '$2 == "=>" { print $3 }' |
  grep -i '[\\/]ucrt64[\\/]bin[\\/]' | grep -v -i 'libmpv' | sort -fu > "$work/dlls.txt"

while IFS= read -r dll; do
  [[ -n "$dll" ]] || continue
  cp "$(cygpath -u "$dll")" "$stage/"
done < "$work/dlls.txt"

for need in SDL3.dll libmpv-2.dll; do
  [[ -f "$stage/$need" ]] || { echo "missing $need next to ttv-tui-window.exe" >&2; exit 1; }
done

# Ensure no MSYS2 FFmpeg DLLs are packaged.
for forbidden in avcodec avformat avutil swscale swresample postproc avfilter avdevice; do
  if compgen -G "$stage/${forbidden}*.dll" > /dev/null; then
    echo "error: forbidden MSYS2 FFmpeg DLL found in package: $stage/${forbidden}*.dll" >&2
    exit 1
  fi
done

{
  echo "ttv-tui-window.exe links SDL3 and libmpv."
  echo
  echo "== libmpv =="
  echo "libmpv-2.dll is an official binary release from the mpv project,"
  echo "licensed under the GNU Lesser General Public License version 2.1 or later (LGPL-2.1+)."
  echo "Source code for mpv (${MPV_VERSION}): https://github.com/mpv-player/mpv/tree/${MPV_VERSION}"
  echo "Source code for FFmpeg (used in the official mpv static mingw build):"
  echo "  https://github.com/FFmpeg/FFmpeg.git (built via https://github.com/BtbN/FFmpeg-Builds)"
  echo
  echo "== MSYS2 UCRT64 Packages =="
  echo "The other DLLs in this folder (such as SDL3) come unchanged from MSYS2 UCRT64"
  echo "packages, under their own licenses; source and license files: https://packages.msys2.org"
  echo
  if [[ -s "$work/dlls.txt" ]]; then
    while IFS= read -r dll; do
      pacman -Qqo "$(cygpath -u "$dll")" 2>/dev/null || true
    done < "$work/dlls.txt" | sort -u | xargs -r pacman -Q
  fi
  echo
  echo "== GNU Lesser General Public License (LGPL) Version 2.1 =="
  echo
  cat "$mpv_work/LICENSE.LGPL"
} > "$stage/THIRD-PARTY.txt"

rm -f "$out"
(cd "$work" && zip -qr "$out" ttv-tui-window)
echo "$out: $(ls "$stage" | wc -l) files, $(du -sh "$stage" | cut -f1) unpacked"
