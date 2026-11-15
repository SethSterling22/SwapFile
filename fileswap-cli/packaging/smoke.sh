#!/bin/sh
# Installs the amd64 packages from dist/ in clean Debian and Arch containers
# and runs a few commands. Used by CI and by hand: packaging/smoke.sh
set -eu
cd "$(dirname "$0")/.."

CHECK='fileswap --version && fileswap --help | grep -q tfr \
  && fileswap init --name smoke --server 127.0.0.1:7341 | grep -q "ws://127.0.0.1:7341/ws" \
  && fileswap id | grep -Eq "[0-9a-f]{8}-[0-9a-f]{4}-" \
  && fileswap id | grep -q "Folder:  $HOME/FileSwap" \
  && test "$(stat -c %a ~/.config/fileswap/identity.json)" = 600 \
  && fileswap listen -d && fileswap status | grep -q PID && test -d ~/FileSwap && fileswap stop | grep -q Offline \
  && { fileswap status; test $? = 3; } \
  && echo "smoke OK on $(. /etc/os-release; echo $PRETTY_NAME)"'

docker run --rm -v "$PWD/dist":/dist:ro debian:stable sh -euc \
  "apt-get install -y -qq /dist/fileswap_*_amd64.deb >/dev/null && $CHECK"

docker run --rm -v "$PWD/dist":/dist:ro archlinux:latest sh -euc \
  "pacman -U --noconfirm /dist/fileswap-*-x86_64.pkg.tar.zst >/dev/null && $CHECK"
