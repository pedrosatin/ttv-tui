#!/usr/bin/env bash
# Builds ttv-tui-window.exe in an MSYS2 UCRT64 shell and packages it in a
# ttv-tui-window folder with the official LGPL libmpv-2.dll, SDL3 from MSYS2,
# runtime dependencies, license files, and THIRD-PARTY.txt.
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
MPV_SEMVER="${MPV_VERSION#v}"
MPV_ASSET="libmpv-${MPV_VERSION}-x86_64-w64-mingw32-lgpl.zip"
MPV_SHA256="8e5f77430efc53ef175804ba6618527e3af0448a0dcabf04d3cc72659d1a6fce"
MPV_URL="https://github.com/mpv-player/mpv/releases/download/${MPV_VERSION}/${MPV_ASSET}"

FFMPEG_COMMIT="baa68e68b34e2de912eb7e94342cebd2545f05ed"
FFMPEG_TAG="git-2026-11-04-baa68e6"
FFMPEG_SOURCE_URL="https://github.com/FFmpeg/FFmpeg/commit/${FFMPEG_COMMIT}"

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

# Verify that libmpv-2.dll contains the expected FFmpeg version mark.
if ! grep -q -a -F "$FFMPEG_TAG" "$mpv_work/libmpv-2.dll"; then
  echo "error: libmpv-2.dll does not contain expected FFmpeg version mark ($FFMPEG_TAG)." >&2
  echo "When updating MPV_VERSION, verify the FFmpeg mark in libmpv-2.dll and update FFMPEG_TAG and FFMPEG_COMMIT." >&2
  exit 1
fi

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
Version: ${MPV_SEMVER}
Libs: -L\${libdir} -lmpv
Cflags: -I\${includedir}
EOF

# Ensure pkg-config uses the generated mpv.pc from the official archive.
export PKG_CONFIG_PATH="$mpv_work/pkgconfig:${PKG_CONFIG_PATH:-}"
echo "mpv pkg-config:"
pkg-config --modversion --cflags --libs mpv

actual_pc_dir="$(cygpath -u "$(pkg-config --variable=pcfiledir mpv)" 2>/dev/null || pkg-config --variable=pcfiledir mpv)"
expected_pc_dir="$(cygpath -u "$mpv_work/pkgconfig" 2>/dev/null || echo "$mpv_work/pkgconfig")"
if [[ "$actual_pc_dir" != "$expected_pc_dir" ]]; then
  echo "error: pkg-config resolved mpv.pc from '$actual_pc_dir' instead of '$expected_pc_dir'" >&2
  exit 1
fi

# Build ttv-tui-window.exe against the official libmpv headers and import library.
(cd "$root/window" && PKG_CONFIG_PATH="$mpv_work/pkgconfig:${PKG_CONFIG_PATH:-}" CGO_ENABLED=1 go build -buildvcs=false -trimpath -ldflags "-s -w" -o "$stage/ttv-tui-window.exe" .)

# Put the official LGPL libmpv-2.dll and LICENSE.LGPL in place.
cp "$mpv_work/libmpv-2.dll" "$stage/"
cp "$mpv_work/LICENSE.LGPL" "$stage/"

# Put versioned FFmpeg LGPLv3 and GPLv3 license texts in place.
cp "$root/packaging/licenses/COPYING.LGPLv3" "$stage/"
cp "$root/packaging/licenses/COPYING.GPLv3" "$stage/"

# Locate and put the libiconv license from MSYS2 in place.
iconv_lic="$(pacman -Ql mingw-w64-ucrt-x86_64-libiconv | awk '$2 ~ /\/share\/licenses\/libiconv\/COPYING\.LIB$/ { print $2 }' | head -n 1)"
if [[ -z "$iconv_lic" || ! -f "$iconv_lic" ]]; then
  iconv_lic="$(pacman -Ql mingw-w64-ucrt-x86_64-libiconv | awk '$2 ~ /\/share\/licenses\/.*COPYING/ { print $2 }' | head -n 1)"
fi
if [[ -z "$iconv_lic" || ! -f "$iconv_lic" ]]; then
  echo "error: failed to locate libiconv license via pacman -Ql mingw-w64-ucrt-x86_64-libiconv" >&2
  exit 1
fi
cp "$iconv_lic" "$stage/"
iconv_lic_name="$(basename "$iconv_lic")"

# ntldd -R walks the import tables; system DLLs live outside /ucrt64/bin.
# With libmpv-2.dll already in $stage, ntldd resolves it locally and sees only
# system DLL imports. Only SDL3 and its runtime support come from /ucrt64/bin.
(cd "$stage" && ntldd -R ttv-tui-window.exe) | tr -d '\r' | awk '$2 == "=>" { print $3 }' > "$work/imports.txt"
{ grep -i '[\\/]ucrt64[\\/]bin[\\/]' "$work/imports.txt" || test $? -le 1; } |
  { grep -v -i 'libmpv' || test $? -le 1; } | sort -fu > "$work/dlls.txt"

while IFS= read -r dll; do
  [[ -n "$dll" ]] || continue
  cp "$(cygpath -u "$dll")" "$stage/"
done < "$work/dlls.txt"

# Whitelist of allowed DLLs in the package.
allowed_dlls=("libmpv-2.dll" "SDL3.dll" "libiconv-2.dll")
for dll_path in "$stage"/*.dll; do
  [[ -f "$dll_path" ]] || continue
  dll_name="$(basename "$dll_path")"
  found=false
  for allowed in "${allowed_dlls[@]}"; do
    if [[ "$dll_name" == "$allowed" ]]; then
      found=true
      break
    fi
  done
  if [[ "$found" == false ]]; then
    echo "error: unexpected DLL found in package: $dll_name (allowed: ${allowed_dlls[*]})" >&2
    exit 1
  fi
done

for need in "${allowed_dlls[@]}"; do
  [[ -f "$stage/$need" ]] || { echo "missing $need next to ttv-tui-window.exe" >&2; exit 1; }
done

{
  echo "ttv-tui-window.exe links SDL3 and libmpv."
  echo
  echo "== libmpv and embedded FFmpeg =="
  echo "libmpv-2.dll is an official binary release from the mpv project (${MPV_VERSION})."
  echo "The mpv library code is licensed under the GNU Lesser General Public License version 2.1 or later (LGPL-2.1+)."
  echo "Source code for mpv (${MPV_VERSION}): https://github.com/mpv-player/mpv/tree/${MPV_VERSION}"
  echo
  echo "The FFmpeg build statically embedded within libmpv-2.dll was built with --enable-version3 (without --enable-gpl),"
  echo "and is licensed under the GNU Lesser General Public License version 3.0 or later (LGPL-3.0+)."
  echo "Source code for FFmpeg (${FFMPEG_TAG}):"
  echo "  ${FFMPEG_SOURCE_URL}"
  echo
  echo "Limitation note: The official mpv Windows build uses the container image"
  echo "ghcr.io/btbn/ffmpeg-builds/win64-lgpl:latest without a pinned image tag or digest;"
  echo "consequently, other static libraries embedded within the DLL (such as libass, libplacebo,"
  echo "and dependencies) do not have individually identified exact commit versions."
  echo
  echo "== Written Offer & Library Replacement (LGPL-2.1 §6) =="
  echo "In accordance with Section 6 of the GNU Lesser General Public License (LGPL-2.1), you may"
  echo "replace libmpv-2.dll with a compatible modified version of the library."
  echo
  echo "Written Offer: We offer to provide the complete machine-readable source code for the libraries"
  echo "covered by the LGPL and distributed in this package, valid for at least three (3) years from the"
  echo "date of distribution, upon request."
  echo
  echo "== MSYS2 UCRT64 Packages =="
  echo "The other DLLs in this folder (SDL3.dll and libiconv-2.dll) come unchanged from"
  echo "MSYS2 UCRT64 packages, under their respective open-source licenses."
  echo "Source packages and PKGBUILD definitions: https://packages.msys2.org"
  echo
  if [[ -s "$work/dlls.txt" ]]; then
    while IFS= read -r dll; do
      pacman -Qqo "$(cygpath -u "$dll")" 2>/dev/null || true
    done < "$work/dlls.txt" | sort -u | xargs -r pacman -Q
  fi
  echo
  echo "== License Texts Included in this Package =="
  echo "- LICENSE.LGPL: GNU Lesser General Public License version 2.1 (mpv)"
  echo "- COPYING.LGPLv3: GNU Lesser General Public License version 3 (embedded FFmpeg)"
  echo "- COPYING.GPLv3: GNU General Public License version 3 (referenced by LGPLv3)"
  echo "- ${iconv_lic_name}: GNU Lesser General Public License (libiconv from MSYS2)"
  echo
  echo "== GNU Lesser General Public License (LGPL) Version 2.1 =="
  echo
  cat "$mpv_work/LICENSE.LGPL"
} > "$stage/THIRD-PARTY.txt"

rm -f "$out"
(cd "$work" && zip -qr "$out" ttv-tui-window)
echo "$out: $(ls "$stage" | wc -l) files, $(du -sh "$stage" | cut -f1) unpacked"
echo "Zip archive contents:"
unzip -l "$out"
