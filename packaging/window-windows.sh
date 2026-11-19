#!/usr/bin/env bash
# Builds ttv-tui-window.exe in an MSYS2 UCRT64 shell and packages it in a
# ttv-tui-window folder with the official LGPL libmpv-2.dll, SDL3 from MSYS2,
# runtime dependencies, license files, and THIRD-PARTY.txt.
#
# When LGPL_SOURCE_DIR is set, also downloads the corresponding LGPL sources
# (mpv tag, FFmpeg commit, libiconv from GNU + MSYS2 PKGBUILD/patches) into
# that directory, with SHA-256 verified against pinned or PKGBUILD hashes.
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

# Logs how long each phase takes, in a form that is easy to grep:
#   fase <name>: <N> s
phase_name=""
phase_start=0
phase_end() {
  if [[ -n "$phase_name" ]]; then
    echo "fase ${phase_name}: $((SECONDS - phase_start)) s ($(date -u +%H:%M:%S) UTC)"
    phase_name=""
  fi
}
phase() {
  phase_end
  phase_name="$1"
  phase_start=$SECONDS
  echo "fase ${phase_name}: início ($(date -u +%H:%M:%S) UTC)"
}
SECONDS=0

MPV_VERSION="v0.42.0"
MPV_SEMVER="${MPV_VERSION#v}"
MPV_ASSET="libmpv-${MPV_VERSION}-x86_64-w64-mingw32-lgpl.zip"
MPV_SHA256="8e5f77430efc53ef175804ba6618527e3af0448a0dcabf04d3cc72659d1a6fce"
MPV_URL="https://github.com/mpv-player/mpv/releases/download/${MPV_VERSION}/${MPV_ASSET}"
# GitHub source archive of the same tag used for the binary.
MPV_SOURCE_ASSET="mpv-${MPV_VERSION}.tar.gz"
MPV_SOURCE_URL="https://github.com/mpv-player/mpv/archive/refs/tags/${MPV_VERSION}.tar.gz"
MPV_SOURCE_SHA256="bcfebbfa2b65a6d88dc97ea18117831072373e38b5785a2afb874ed4df5070dd"

FFMPEG_COMMIT="baa68e68b34e2de912eb7e94342cebd2545f05ed"
FFMPEG_TAG="git-2026-11-04-baa68e6"
FFMPEG_SOURCE_ASSET="FFmpeg-${FFMPEG_COMMIT}.tar.gz"
FFMPEG_SOURCE_TARBALL_URL="https://github.com/FFmpeg/FFmpeg/archive/${FFMPEG_COMMIT}.tar.gz"
FFMPEG_SOURCE_SHA256="a533cd8e097f2c65e8ebf523ab567817a79ecc9399c5bc5405be0aaf5b79a1f1"

MSYS2_LIBICONV_PKGBUILD_URL="https://raw.githubusercontent.com/msys2/MINGW-packages/master/mingw-w64-libiconv"
MSYS2_LIBICONV_FILES=(
  PKGBUILD
  0002-fix-cr-for-awk-in-configure.all.patch
  0003-add-cp65001-as-utf8-alias.patch
  iconv.pc
)

cache_dir="${MPV_CACHE_DIR:-${XDG_CACHE_HOME:-$HOME/.cache}/ttv-tui}"
mkdir -p "$cache_dir"
cached_zip="$cache_dir/$MPV_ASSET"

# download_verify <url> <dest> <sha256>
# Writes dest only after the digest matches. Retries briefly on stall.
download_verify() {
  local url="$1" dest="$2" want="$3"
  local tmp="${dest}.tmp"
  if [[ -f "$dest" ]] && echo "${want} *${dest}" | sha256sum -c - >/dev/null 2>&1; then
    return 0
  fi
  echo "Downloading $url..."
  curl -sSL -f --connect-timeout 15 --max-time 120 --retry 2 --retry-delay 2 --retry-all-errors \
    -o "$tmp" "$url"
  echo "${want} *${tmp}" | sha256sum -c -
  mv "$tmp" "$dest"
}

# libiconv_upstream_version prints the upstream version of the installed
# mingw-w64-ucrt-x86_64-libiconv package (e.g. 1.19.1 from 1.19.1-1).
libiconv_upstream_version() {
  local full
  full="$(pacman -Q mingw-w64-ucrt-x86_64-libiconv | awk '{ print $2 }')"
  [[ -n "$full" ]] || {
    echo "error: pacman -Q mingw-w64-ucrt-x86_64-libiconv returned empty" >&2
    exit 1
  }
  # Strip the pkgrel suffix after the last hyphen when it is all digits.
  if [[ "$full" =~ ^(.+)-([0-9]+)$ ]]; then
    printf '%s\n' "${BASH_REMATCH[1]}"
  else
    printf '%s\n' "$full"
  fi
}

# fetch_lgpl_sources <outdir> writes the LGPL source attachments and SHA256SUMS.
# Versions come from the same pins/DLL mark/pacman query used for the zip.
fetch_lgpl_sources() {
  local outdir="$1"
  local src_cache="$cache_dir/lgpl-sources"
  mkdir -p "$outdir" "$src_cache"

  local mpv_src="$src_cache/$MPV_SOURCE_ASSET"
  download_verify "$MPV_SOURCE_URL" "$mpv_src" "$MPV_SOURCE_SHA256"
  cp -f "$mpv_src" "$outdir/$MPV_SOURCE_ASSET"

  local ffmpeg_src="$src_cache/$FFMPEG_SOURCE_ASSET"
  download_verify "$FFMPEG_SOURCE_TARBALL_URL" "$ffmpeg_src" "$FFMPEG_SOURCE_SHA256"
  cp -f "$ffmpeg_src" "$outdir/$FFMPEG_SOURCE_ASSET"

  local iconv_ver packaging_dir packaging_tar gnu_url gnu_tar expected_hash pkgbuild_ver
  iconv_ver="$(libiconv_upstream_version)"
  echo "libiconv from MSYS2: mingw-w64-ucrt-x86_64-libiconv -> upstream $iconv_ver"
  packaging_dir="$work/libiconv-msys2-packaging"
  rm -rf "$packaging_dir"
  mkdir -p "$packaging_dir"
  local f
  for f in "${MSYS2_LIBICONV_FILES[@]}"; do
    curl -sSL -f --connect-timeout 15 --max-time 60 --retry 2 --retry-delay 2 --retry-all-errors \
      -o "$packaging_dir/$f" "${MSYS2_LIBICONV_PKGBUILD_URL}/$f"
  done
  pkgbuild_ver="$(awk -F= '/^pkgver=/{ gsub(/["'\'']/,"",$2); print $2; exit }' "$packaging_dir/PKGBUILD")"
  if [[ "$pkgbuild_ver" != "$iconv_ver" ]]; then
    echo "error: MSYS2 PKGBUILD on master has pkgver=$pkgbuild_ver but installed libiconv is $iconv_ver." >&2
    echo "Update the packaging files URL to a MINGW-packages commit that matches the installed package." >&2
    exit 1
  fi
  # First sha256sums entry is the GNU tarball (second is .sig with SKIP).
  expected_hash="$(awk -F"'" '/^sha256sums=/{ print $2; exit }' "$packaging_dir/PKGBUILD")"
  [[ -n "$expected_hash" && "$expected_hash" != SKIP ]] || {
    echo "error: failed to parse libiconv tarball sha256 from PKGBUILD" >&2
    exit 1
  }
  gnu_url="https://ftp.gnu.org/pub/gnu/libiconv/libiconv-${iconv_ver}.tar.gz"
  gnu_tar="$src_cache/libiconv-${iconv_ver}.tar.gz"
  download_verify "$gnu_url" "$gnu_tar" "$expected_hash"
  cp -f "$gnu_tar" "$outdir/libiconv-${iconv_ver}.tar.gz"

  # Verify non-SKIP patch hashes from PKGBUILD against the downloaded files.
  # Order matches source=(): tarball, .sig, then the local files.
  local -a sums=()
  local line
  while IFS= read -r line; do
    [[ "$line" == *\'* ]] || continue
    sums+=("$(printf '%s\n' "$line" | sed -n "s/.*'\\([0-9a-fA-F]\\{64\\}\\|SKIP\\)'.*/\\1/p")")
  done < <(awk '/^sha256sums=/,/\)/' "$packaging_dir/PKGBUILD")
  local -a local_files=(
    0003-add-cp65001-as-utf8-alias.patch
    0002-fix-cr-for-awk-in-configure.all.patch
    iconv.pc
  )
  # sums[0]=tarball, sums[1]=sig SKIP, sums[2..]=local_files
  local i sum
  for i in "${!local_files[@]}"; do
    sum="${sums[$((i + 2))]:-}"
    [[ -n "$sum" && "$sum" != SKIP ]] || {
      echo "error: missing sha256 for ${local_files[$i]} in PKGBUILD" >&2
      exit 1
    }
    (cd "$packaging_dir" && echo "${sum} *${local_files[$i]}" | sha256sum -c -)
  done

  packaging_tar="libiconv-${iconv_ver}-msys2-packaging.tar.gz"
  tar -C "$packaging_dir" -czf "$outdir/$packaging_tar" "${MSYS2_LIBICONV_FILES[@]}"

  {
    echo "# SHA-256 of LGPL sources attached next to the Windows window zip."
    echo "# Generated by packaging/window-windows.sh from the versions actually packaged."
    (cd "$outdir" && sha256sum \
      "$MPV_SOURCE_ASSET" \
      "$FFMPEG_SOURCE_ASSET" \
      "libiconv-${iconv_ver}.tar.gz" \
      "$packaging_tar")
  } > "$outdir/SHA256SUMS"

  {
    echo "mpv ${MPV_VERSION}"
    echo "FFmpeg ${FFMPEG_COMMIT} (${FFMPEG_TAG})"
    echo "libiconv ${iconv_ver} (from $(pacman -Q mingw-w64-ucrt-x86_64-libiconv))"
    echo "SDL3 is zlib-licensed and is not included here."
  } > "$outdir/MANIFEST.txt"

  echo "LGPL sources written to $outdir:"
  ls -lh "$outdir"
}

phase download
if [[ ! -f "$cached_zip" ]] || ! (cd "$cache_dir" && echo "$MPV_SHA256 *$MPV_ASSET" | sha256sum -c - >/dev/null 2>&1); then
  echo "Downloading $MPV_URL..."
  # Bounded so a stalled connection fails (or retries) instead of hanging the job:
  # 3 attempts of at most 90 s each.
  curl -sSL -f --connect-timeout 15 --max-time 90 --retry 2 --retry-delay 2 --retry-all-errors \
    -o "$cached_zip.tmp" "$MPV_URL"
  (cd "$cache_dir" && echo "$MPV_SHA256 *$MPV_ASSET.tmp" | sha256sum -c -)
  mv "$cached_zip.tmp" "$cached_zip"
fi

echo "Verifying $MPV_ASSET..."
(cd "$cache_dir" && echo "$MPV_SHA256 *$MPV_ASSET" | sha256sum -c -)

phase extração
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
echo "mpv pkg-config: version $(pkg-config --modversion mpv)"
echo "mpv pkg-config: $(pkg-config --cflags --libs mpv)"

actual_pc_dir="$(cygpath -u "$(pkg-config --variable=pcfiledir mpv)" 2>/dev/null || pkg-config --variable=pcfiledir mpv)"
expected_pc_dir="$(cygpath -u "$mpv_work/pkgconfig" 2>/dev/null || echo "$mpv_work/pkgconfig")"
if [[ "$actual_pc_dir" != "$expected_pc_dir" ]]; then
  echo "error: pkg-config resolved mpv.pc from '$actual_pc_dir' instead of '$expected_pc_dir'" >&2
  exit 1
fi

phase build
# Build ttv-tui-window.exe against the official libmpv headers and import library.
(cd "$root/window" && PKG_CONFIG_PATH="$mpv_work/pkgconfig:${PKG_CONFIG_PATH:-}" CGO_ENABLED=1 go build -buildvcs=false -trimpath -ldflags "-s -w" -o "$stage/ttv-tui-window.exe" .)

phase licenças
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
iconv_ver="$(libiconv_upstream_version)"
iconv_pacman_q="$(pacman -Q mingw-w64-ucrt-x86_64-libiconv)"

phase dlls
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

phase verificação-e-third-party
# Whitelist of allowed DLLs in the package.
# LGPL among these: libmpv-2.dll (and FFmpeg statically linked inside it) and
# libiconv-2.dll. SDL3.dll is zlib.
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
  echo "The FFmpeg build statically embedded within libmpv-2.dll was built with --enable-version3 (without --enable-gpl),"
  echo "and is licensed under the GNU Lesser General Public License version 3.0 or later (LGPL-3.0+)."
  echo
  echo "Corresponding source for these LGPL libraries is published as attachments of the same"
  echo "GitHub release that publishes this zip (filenames and SHA-256):"
  echo "  ${MPV_SOURCE_ASSET}  (mpv ${MPV_VERSION}; SHA-256 ${MPV_SOURCE_SHA256})"
  echo "  ${FFMPEG_SOURCE_ASSET}  (FFmpeg ${FFMPEG_TAG} / ${FFMPEG_COMMIT}; SHA-256 ${FFMPEG_SOURCE_SHA256})"
  echo "  libiconv-${iconv_ver}.tar.gz  (GNU libiconv matching ${iconv_pacman_q}; SHA-256 from the MSYS2 PKGBUILD)"
  echo "  libiconv-${iconv_ver}-msys2-packaging.tar.gz  (PKGBUILD and patches used by MSYS2 for that version)"
  echo "  SHA256SUMS  (digests of the files above, produced by the packaging job)"
  echo
  echo "Limitation note: The official mpv Windows build uses the container image"
  echo "ghcr.io/btbn/ffmpeg-builds/win64-lgpl:latest without a pinned image tag or digest;"
  echo "consequently, other static libraries embedded within the DLL (such as libass, libplacebo,"
  echo "and dependencies) do not have individually identified exact commit versions."
  echo
  echo "== Other libraries statically linked into libmpv-2.dll =="
  echo "The mpv ${MPV_VERSION} mingw LGPL build (ci/build-mingw64-full.sh on top of the BtbN"
  echo "win64-lgpl image) embeds additional libraries. Exact commits for the BtbN image contents"
  echo "are not published with the binary. Libraries identifiable from that build and from"
  echo "strings in libmpv-2.dll include, among others:"
  echo "  libass (ISC), libplacebo (LGPL-2.1+), FreeType (FTL or GPL), HarfBuzz (MIT),"
  echo "  FriBidi (LGPL-2.1), dav1d (BSD-2-Clause), libarchive (BSD-2-Clause), mujs (ISC),"
  echo "  uchardet (MPL-1.1/GPL/LGPL), zlib (zlib), libpng (libpng/zlib), lcms2 (MIT),"
  echo "  LuaJIT (MIT), libsixel (MIT). See each project's license for the full text."
  echo
  echo "== Library Replacement (LGPL-2.1 §6) =="
  echo "ttv-tui-window.exe loads libmpv-2.dll as a shared library: you may replace it with a"
  echo "compatible modified version of the library."
  echo
  echo "== MSYS2 UCRT64 Packages =="
  echo "The other DLLs in this folder (SDL3.dll and libiconv-2.dll) come unchanged from"
  echo "MSYS2 UCRT64 packages, under their respective open-source licenses."
  echo "SDL3 is zlib-licensed. libiconv (${iconv_ver}, package ${iconv_pacman_q}) is LGPL;"
  echo "its GNU source tarball and the MSYS2 PKGBUILD/patches are release attachments named above."
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

phase fontes-lgpl
if [[ -n "${LGPL_SOURCE_DIR:-}" ]]; then
  mkdir -p "$LGPL_SOURCE_DIR"
  # Resolve to an absolute path so the trap-cleaned $work tree is not required.
  lgpl_out="$(cd "$LGPL_SOURCE_DIR" && pwd)"
  fetch_lgpl_sources "$lgpl_out"
  # Record the libiconv digest actually written (from SHA256SUMS) into a sidecar
  # the release job can echo; THIRD-PARTY already points at the PKGBUILD hash rule.
else
  echo "LGPL_SOURCE_DIR unset: skipping source attachment download (CI packaging only)."
fi

phase compressão
rm -f "$out"
# Level 1 instead of the default 6: on a tree of about 140 MB (libmpv-2.dll is 120 MB)
# it takes about a third of the time for a zip about 8% larger.
(cd "$work" && zip -qr -1 "$out" ttv-tui-window)
phase_end
echo "fase total: $SECONDS s"
echo "$out: $(ls "$stage" | wc -l) files, $(du -sh "$stage" | cut -f1) unpacked"
echo "Zip archive contents:"
unzip -l "$out"
