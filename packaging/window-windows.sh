#!/usr/bin/env bash
# Builds ttv-tui-window.exe in an MSYS2 UCRT64 shell and zips it in a
# ttv-tui-window folder with the DLLs it loads from /ucrt64/bin and the list of
# MSYS2 packages they come from (source at https://packages.msys2.org).
#
# Usage: packaging/window-windows.sh <output.zip>
# Needs: mingw-w64-ucrt-x86_64-{gcc,pkgconf,sdl3,mpv,ntldd}, zip, and go on PATH.
set -euo pipefail

[[ $# -eq 1 ]] || { echo "usage: $0 <output.zip>" >&2; exit 2; }
out="$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
root="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
stage="$work/ttv-tui-window"
mkdir -p "$stage"

(cd "$root/window" && CGO_ENABLED=1 go build -buildvcs=false -trimpath -ldflags "-s -w" -o "$stage/ttv-tui-window.exe" .)

# ntldd -R walks the import tables; system DLLs live outside /ucrt64/bin.
ntldd -R "$stage/ttv-tui-window.exe" | tr -d '\r' | awk '$2 == "=>" { print $3 }' |
  grep -i '[\\/]ucrt64[\\/]bin[\\/]' | sort -fu > "$work/dlls.txt"
while IFS= read -r dll; do
  cp "$(cygpath -u "$dll")" "$stage/"
done < "$work/dlls.txt"
for need in SDL3.dll libmpv-2.dll; do
  [[ -f "$stage/$need" ]] || { echo "missing $need next to ttv-tui-window.exe" >&2; exit 1; }
done

{
  echo "ttv-tui-window.exe links SDL3 and libmpv. The DLLs in this folder come"
  echo "unchanged from these MSYS2 UCRT64 packages, under their own licenses;"
  echo "source and license files: https://packages.msys2.org"
  echo
  while IFS= read -r dll; do pacman -Qqo "$(cygpath -u "$dll")"; done < "$work/dlls.txt" | sort -u | xargs pacman -Q
} > "$stage/THIRD-PARTY.txt"

rm -f "$out"
(cd "$work" && zip -qr "$out" ttv-tui-window)
echo "$out: $(ls "$stage" | wc -l) files, $(du -sh "$stage" | cut -f1) unpacked"
