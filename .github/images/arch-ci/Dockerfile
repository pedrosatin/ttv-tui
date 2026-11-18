# Image for the CI and Release jobs that build the window on Arch: the window
# job, govulncheck and the Linux window binary of the release. It carries the
# packages those jobs used to install with pacman on every run. Go is not here:
# the jobs keep installing it with actions/setup-go from go.mod.
#
# Built and published by .github/workflows/arch-ci-image.yml, never on a pull
# request. The jobs pin the published image by digest; README.md next to this
# file explains how to renew it.
FROM docker.io/library/archlinux:base-20261108.0.623074@sha256:d4594bb2300cba11f41c2f27923eb56f63d9988909c402f9c95517d1e362c899

LABEL org.opencontainers.image.source="https://github.com/pedrosatin/ttv-tui" \
      org.opencontainers.image.description="Arch build environment for the ttv-tui window jobs" \
      org.opencontainers.image.licenses="MIT"

# git: actions/checkout and go modules. gcc, pkgconf, sdl3, mpv, libglvnd: cgo
# build of the window. mesa: llvmpipe for the offscreen UX scenarios.
# fontconfig, ttf-dejavu, noto-fonts-emoji: fc-match and the fonts the window
# draws with. tar, gzip: actions/setup-go and the release tarball.
RUN pacman -Syu --noconfirm --needed \
        git gcc pkgconf sdl3 mpv libglvnd mesa \
        fontconfig ttf-dejavu noto-fonts-emoji \
        tar gzip \
    && rm -rf /var/cache/pacman/pkg/*

# Fails the build if a library the window links against, or a font it looks
# up, is missing.
RUN pkg-config --exists sdl3 mpv gl \
    && fc-match -f '%{file}\n' monospace \
    && test -f /usr/share/fonts/noto/NotoColorEmoji.ttf
