#!/bin/sh
# End-to-end test between two Linux nodes: the real server image, and the
# fileswap binary in a Debian and an Arch container on their own Docker
# network. They pair, send a file one way and a folder (ZIP) the other way,
# and one reads and takes the other's clipboard; every byte is checked.
# Used by CI and by hand: packaging/e2e.sh   (FILESWAP_BIN=dist/fileswap to
# test a `make build` binary instead of `make dist`'s)
set -eu
cd "$(dirname "$0")/.."

BIN=${FILESWAP_BIN:-dist/fileswap-linux-amd64}
NET=fileswap-e2e
SERVER=fileswap-e2e-server
UUID_RE='[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}'
[ -x "$BIN" ] || { echo "e2e: $BIN not found (run make dist)" >&2; exit 1; }

cleanup() {
	docker rm -f "$SERVER" fileswap-e2e-a fileswap-e2e-b >/dev/null 2>&1 || true
	docker network rm "$NET" >/dev/null 2>&1 || true
}
trap cleanup EXIT
trap 'exit 1' INT TERM
cleanup

step() { printf '\n== %s\n' "$*"; }
fail() { printf 'e2e FAILED: %s\n' "$*" >&2; for n in a b; do echo "--- node $n log" >&2; on "$n" 'cat ~/.local/state/fileswap/listen.log' >&2 || true; done; exit 1; }
# on <a|b> <shell command>: runs it in that node's container
on() { n=$1; shift; docker exec "fileswap-e2e-$n" sh -euc "$*"; }

step "server image and network"
docker build -q -t fileswap-e2e-server -f ../fileswap/server/Dockerfile ../fileswap >/dev/null
docker network create "$NET" >/dev/null
docker run -d --name "$SERVER" --network "$NET" fileswap-e2e-server >/dev/null
for _ in $(seq 30); do
	docker exec "$SERVER" wget -qO- http://localhost:3000/health >/dev/null 2>&1 && break
	sleep 1
done

step "nodes: debian (a) and arch (b)"
docker run -d --name fileswap-e2e-a --network "$NET" -v "$PWD/$BIN:/usr/local/bin/fileswap:ro" debian:stable sleep infinity >/dev/null
docker run -d --name fileswap-e2e-b --network "$NET" -v "$PWD/$BIN:/usr/local/bin/fileswap:ro" archlinux:latest sleep infinity >/dev/null
on a "fileswap init --name debian-a --server $SERVER:3000 >/dev/null"
on b "fileswap init --name arch-b --server $SERVER:3000 --kind laptop >/dev/null"
A=$(on a 'fileswap id' | grep -Eo "$UUID_RE" | head -n1)
B=$(on b 'fileswap id' | grep -Eo "$UUID_RE" | head -n1)
echo "a=$A b=$B"

step "pair: a allows b, b syncs with a"
on a "fileswap allow $B && fileswap start" >/dev/null
on b "fileswap sync $A" || fail "sync"
on b 'fileswap start' >/dev/null
for _ in $(seq 20); do
	on a 'fileswap peers' | grep arch-b | grep -qi online && break
	sleep 1
done
on a 'fileswap peers'
on a 'fileswap peers' | grep arch-b | grep -qi online || fail "a does not see b online"
on b 'fileswap peers' | grep -q debian-a || fail "b is not paired with a"

step "file a → b (20 MB)"
on a 'head -c 20000000 /dev/urandom > /tmp/big.bin && fileswap tfr arch-b /tmp/big.bin' || fail "tfr file"
SENT=$(on a 'sha256sum /tmp/big.bin' | cut -d' ' -f1)
GOT=$(on b 'sha256sum ~/FileSwap/big.bin' | cut -d' ' -f1) || fail "big.bin not in b's swap folder"
[ "$SENT" = "$GOT" ] || fail "big.bin differs: $SENT vs $GOT"
echo "big.bin OK ($GOT)"

step "folder b → a (one ZIP)"
on b 'mkdir -p /tmp/fotos/sub && echo hola > /tmp/fotos/nota.txt && head -c 3000000 /dev/urandom > /tmp/fotos/sub/foto.jpg \
	&& fileswap tfr debian-a /tmp/fotos' || fail "tfr folder"
# debian:stable has no unzip: the server image (busybox) extracts it
on a 'cat ~/FileSwap/fotos.zip' | docker exec -i "$SERVER" sh -euc \
	'cd /tmp && cat > f.zip && mkdir x && cd x && unzip -q ../f.zip' || fail "fotos.zip missing or not a ZIP"
WANT=$(on b 'cd /tmp && find fotos -type f | sort | xargs sha256sum')
HAVE=$(docker exec "$SERVER" sh -euc 'cd /tmp/x && find fotos -type f | sort | xargs sha256sum')
[ "$WANT" = "$HAVE" ] || fail "ZIP contents differ:\n$WANT\n---\n$HAVE"
echo "$HAVE"

step "clipboard of b, from a"
on b 'fileswap clip add -t "only a note" >/dev/null && fileswap clip add -t "the plan" /tmp/fotos/sub/foto.jpg >/dev/null'
LS=$(on a 'fileswap clip ls arch-b') || fail "clip ls"
echo "$LS"
echo "$LS" | grep -q "only a note" || fail "note not listed"
echo "$LS" | grep -q "foto.jpg" || fail "attachment not listed"
# The item's ID is on the line above its attachment
ID=$(echo "$LS" | grep -B1 "foto.jpg" | head -n1 | awk '{print $1}')
on a "fileswap clip get arch-b $ID" | grep -q "the plan" || fail "clip get $ID: text"
WANT=$(on b 'sha256sum /tmp/fotos/sub/foto.jpg' | cut -d' ' -f1)
HAVE=$(on a 'sha256sum ~/FileSwap/foto.jpg' | cut -d' ' -f1) || fail "clip attachment not saved"
[ "$WANT" = "$HAVE" ] || fail "clip attachment differs"
echo "clip get OK"

step "unpair"
on a 'fileswap unpair arch-b' >/dev/null || fail "unpair"
sleep 2
on b 'fileswap peers' | grep -q debian-a && fail "b still lists a after unpair"
on a 'fileswap stop' | grep -q "offline" || fail "stop"

printf '\ne2e OK: Debian ⇄ Arch through %s\n' "$(docker exec "$SERVER" wget -qO- http://localhost:3000/health)"
